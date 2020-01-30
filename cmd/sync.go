package main

import (
	"time"

	"github.com/cloudworkz/grafana-permission-sync/pkg/groups"
	"github.com/rikimaru0345/sdk"
	"golang.org/x/time/rate"
)

// describes an update to a user,
// how to adjust roles in each org
type userUpdate struct {
	Email   string
	Changes []*userRoleChange
}
type userRoleChange struct {
	Organization *grafanaOrganization
	OldRole      Role
	NewRole      Role
	Reason       *Rule
}

var (
	createdPlans         = 0
	lastGoogleGroupFetch time.Time

	grafana   *grafanaState
	groupTree *groups.GroupTree
)

func setupSync() {
	lastGoogleGroupFetch = time.Now().Add(-999 * time.Hour)
	log.Infow("starting sync", "applyInterval", config.Settings.ApplyInterval.String(), "groupRefreshInterval", config.Settings.GroupsFetchInterval.String())

	// 1. grafana state
	grafanaClient := sdk.NewClient(config.Grafana.URL, config.Grafana.User+":"+config.Grafana.Password, sdk.DefaultHTTPClient)
	grafana = &grafanaState{grafanaClient, nil, make(map[uint]*grafanaOrganization), rate.NewLimiter(rate.Every(time.Second/10), 2)}

	// 2. google groups service
	var err error
	groupTree, err = groups.CreateGroupTree(log, config.Google.Domain, config.Google.AdminEmail, config.Google.CredentialsPath, []string{
		"https://www.googleapis.com/auth/admin.directory.group.member.readonly",
		"https://www.googleapis.com/auth/admin.directory.group.readonly",
		//"https://www.googleapis.com/auth/admin.directory.user.readonly",
	}...)
	if err != nil {
		log.Fatalw("unable to create google directory service", "error", err.Error())
	}
}

func startSync() {
	for {
		updatePlan := createUpdatePlan()
		createdPlans++

		printPlan(updatePlan)

		executePlan(updatePlan)

		time.Sleep(config.Settings.ApplyInterval)
	}
}

func createUpdatePlan() []userUpdate {

	// - Grafana: fetch all users and orgs from grafana
	log.Info("createUpdatePlan: fetching grafana state")
	grafana.fetchState()
	// - Rules: from the rules get set of all groups and set of all explicit users; fetch them from google
	log.Info("createUpdatePlan: fetching google groups")
	fetchGoogleGroups()

	log.Info("createUpdatePlan: compute update map")
	updates := make(map[string]*userUpdate) // user email -> update

	// 1. setup initial state: nobody is in any organization!
	for _, grafUser := range grafana.allUsers {
		var initialChangeSet []*userRoleChange
		for _, org := range grafana.organizations {
			orgUser := org.findUser(grafUser.Email)
			var currentRole Role
			if orgUser != nil {
				currentRole = Role(orgUser.Role)
			}
			initialChangeSet = append(initialChangeSet, &userRoleChange{org, currentRole, "", nil})
		}

		updates[grafUser.Email] = &userUpdate{grafUser.Email, initialChangeSet}
	}

	// 2. apply all rules, keep highest permission
	for _, rule := range config.Rules {
		applyRule(updates, rule)
	}

	// 3. filter changes:
	// - remove entries that don't do anything (same new and old role)
	// - remove demotions if we're not allowed to
	// - todo: do not remove from orgID 1
	for _, userUpdate := range updates {
		var realChanges []*userRoleChange
		for _, change := range userUpdate.Changes {

			keepChange := true

			if change.OldRole == change.NewRole {
				keepChange = false // not a change
			}

			if !config.Settings.CanDemote && change.NewRole.isLowerThan(change.OldRole) {
				keepChange = false // prevent demotion / removal
			}

			if change.Organization.ID == 1 && change.NewRole == "" && !config.Settings.RemoveFromMainOrg {
				keepChange = false // don't remove from main org
			}

			if keepChange {
				realChanges = append(realChanges, change)
			}
		}
		userUpdate.Changes = realChanges
	}

	// convert update map to slice, filter entries that don't do anything
	var result []userUpdate
	for _, update := range updates {
		if len(update.Changes) > 0 {
			result = append(result, *update)
		}
	}
	return result
}

func printPlan(plan []userUpdate) {

	totalChanges := 0
	for _, uu := range plan {
		totalChanges += len(uu.Changes)
	}
	log.Infow("printing new update plan to console...", "affectedUsers", len(plan), "totalChanges", totalChanges)

	for _, uu := range plan {
		for _, change := range uu.Changes {
			if change.OldRole == "" {
				// Add to org
				log.Infow("Add user to org", "user", uu.Email, "org", change.Organization.Name, "role", change.NewRole)
			} else if change.NewRole == "" {
				// Remove from org
				log.Infow("Remove user from org", "user", uu.Email, "org", change.Organization.Name)
			} else {
				// Change role in org
				var verb string
				if change.NewRole.isHigherThan(change.OldRole) {
					verb = "Promote"
				} else {
					verb = "Demote"
				}

				log.Infow(verb+" user", "user", uu.Email, "org", change.Organization.Name, "oldRole", change.OldRole, "role", change.NewRole, "reasonIndex", change.Reason.Index, "reasonNote", change.Reason.Note)
			}
		}
	}
}

func executePlan(plan []userUpdate) {
	log.Infow("executing plan...")

	for _, uu := range plan {
		for _, change := range uu.Changes {
			var status sdk.StatusMessage
			var err error = nil
			var user *sdk.OrgUser = nil

			if change.OldRole != "" {
				user = change.Organization.findUser(uu.Email)
				if user == nil {
					log.Warnw("cannot find orgUser", "action", "remove from org", "user", uu.Email)
					continue
				}
			}

			if change.OldRole == "" {
				// Add to org
				grafana.Wait()
				status, err = grafana.AddOrgUser(sdk.UserRole{LoginOrEmail: uu.Email, Role: string(change.NewRole)}, change.Organization.ID)
			} else if change.NewRole == "" {
				// Remove from org
				grafana.Wait()
				status, err = grafana.DeleteOrgUser(change.Organization.ID, user.ID)
			} else {
				// Change role in org
				grafana.Wait()
				status, err = grafana.UpdateOrgUser(sdk.UserRole{LoginOrEmail: uu.Email, Role: string(change.NewRole)}, change.Organization.ID, user.ID)
			}

			if err != nil {
				log.Errorw("error applying change",
					"userEmail", uu.Email,
					"changeOrg", change.Organization.Name,
					"changeOldRole", change.OldRole,
					"changeNewRole", change.NewRole,
					"error", err,
					"message", status.Message,
					"slug", status.Slug,
					"version", status.Version,
					"status", status.Status,
					"UID", status.UID,
					"URL", status.URL)
			}
		}
	}
}

func applyRule(userUpdates map[string]*userUpdate, rule *Rule) {

	// 1. find set of all affected users
	// users = rule.Groups.Select(g=>g.Email).Concat(rule.Users).Distinct();
	var users []string // user emails

	for _, groupEmail := range rule.Groups {
		group, err := groupTree.GetGroup(groupEmail)
		if err != nil {
			log.Error("unable to get group", "email", groupEmail, "error", err)
		}
		for _, user := range group.AllUsers() {
			users = append(users, user.Email)
		}
	}

	for _, userEmail := range rule.Users {
		users = append(users, userEmail)
	}

	users = distinct(users)

	// 2. update the role in the corrosponding org for each user
	for _, u := range users {
		update, exists := userUpdates[u]
		if !exists {
			continue
		}

		for _, change := range update.Changes {
			if rule.matchesOrg(change.Organization.Name) {
				if rule.Role.isHigherThan(change.NewRole) {
					// this rule applies a "higher" role than is already set
					change.NewRole = rule.Role
					change.Reason = rule
				}
			}
		}
	}
}

func fetchGoogleGroups() {

	timeSinceLastGroupFetch := time.Now().Sub(lastGoogleGroupFetch)
	if timeSinceLastGroupFetch < config.Settings.GroupsFetchInterval {
		// skip update for now...
		return
	}

	lastGoogleGroupFetch = time.Now()
	log.Infow("refreshing google groups", "timeSinceLastGroupFetch", timeSinceLastGroupFetch.String())

	groupTree.Clear()

	// prefetch all groups and users
	distinctGroups := config.getAllGroups()
	for _, reqGroup := range distinctGroups {
		log.Infow("fetching google group", "email", reqGroup)
		_, err := groupTree.GetGroup(reqGroup)
		if err != nil {
			log.Errorw("error fetching group", "error", err)
		}
	}

	// We never need to fetch individual users, if a rule gives permissions to a user directly, we don't need any lookup anyway
	// distinctUsers := config.getAllUsers()
	// for _, reqUser := range distinctUsers {
	// 	log.Infow("fetching google user", "email", reqUser)
	// 	groupTree.GetUser(reqUser)
	// }
}

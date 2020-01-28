package main

import (
	"github.com/grafana-tools/sdk"
	"time"
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

type grafanaOrganization struct {
	*sdk.Org
	Users []sdk.OrgUser
}

var (
	grafana *sdk.Client

	// todo: move this into a separate "Grafana" package and don't use package-globals
	allUsers      []sdk.User
	organizations map[uint]*grafanaOrganization // [orgID]Org

	createdPlans         = 0 // must be >0 for the service to be ready
	lastGoogleGroupFetch time.Time
)

func startSync() {
	lastGoogleGroupFetch = time.Now().Add(-999 * time.Hour)
	log.Infow("starting sync", "applyInterval", config.Settings.ApplyInterval.String(), "groupRefreshInterval", config.Settings.GroupsFetchInterval.String())

	grafana = sdk.NewClient(config.Grafana.URL, config.Grafana.User+":"+config.Grafana.Password, sdk.DefaultHTTPClient)

	for {
		updatePlan := createUpdatePlan()
		createdPlans++

		printPlan(updatePlan)

		time.Sleep(config.Settings.ApplyInterval)
		time.Sleep(100 * time.Hour)
		//executeUpdatePlan()
	}
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

				log.Infow(verb+" user", "user", uu.Email, "org", change.Organization.Name, "oldRole", change.OldRole, "role", change.NewRole)
			}
		}
	}
}

func createUpdatePlan() []userUpdate {

	// - Grafana: fetch all users and orgs from grafana
	log.Info("createUpdatePlan: fetching grafana state")
	fetchGrafanaState()
	// - Rules: from the rules get set of all groups and set of all explicit users; fetch them from google
	log.Info("createUpdatePlan: fetching google groups")
	fetchGoogleGroups()

	log.Info("createUpdatePlan: compute update map")
	updates := make(map[string]*userUpdate) // user email -> update

	// 1. setup initial state: nobody is in any organization!
	for _, grafUser := range allUsers {
		var initialChangeSet []*userRoleChange
		for _, org := range organizations {
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
			if contains(rule.Organizations, change.Organization.Org.Name) {
				if rule.Role.isHigherThan(change.NewRole) {
					// this rule applies a "higher" role than is already set
					change.NewRole = rule.Role
					change.Reason = rule
				}
			}
		}
	}
}

func fetchGrafanaState() {

	// get all users (including those that don't belong to any org)
	var err error
	allUsers, err = grafana.GetAllUsers()
	if err != nil {
		log.Errorf("unable to fetch all users from grafana: %v", err.Error())
		return
	}

	// get all orgs...
	organizations = make(map[uint]*grafanaOrganization)
	orgs, err := grafana.GetAllOrgs()
	if err != nil {
		log.Errorf("unable to list all orgs: %v" + err.Error())
		return
	}

	for _, org := range orgs {
		// ...and their users
		users, err := grafana.GetOrgUsers(org.ID)
		if err != nil {
			log.Error("error listing users for org: " + err.Error())
			continue
		}
		orgCopy := org // need to create a local copy of the org...
		organizations[org.ID] = &grafanaOrganization{&orgCopy, users}
	}
}

func (org grafanaOrganization) findUser(userEmail string) *sdk.OrgUser {
	for _, u := range org.Users {
		if u.Email == userEmail {
			return &u
		}
	}
	return nil
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

	// distinctUsers := config.getAllUsers()
	// for _, reqUser := range distinctUsers {
	// 	log.Infow("fetching google user", "email", reqUser)
	// 	groupTree.GetUser(reqUser)
	// }
}

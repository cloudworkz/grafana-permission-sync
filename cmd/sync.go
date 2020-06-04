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

const (
	noUpdatesMessageInterval = 2 * time.Hour // how often to print the "no changes" message
)

var (
	createdPlans = 0

	applyRateLimit *rate.Limiter

	lastGoogleGroupFetch        time.Time
	googleGroupRefreshRateLimit *rate.Limiter

	noUpdatesMessageRateLimit *rate.Limiter

	grafana   *grafanaState
	groupTree *groups.GroupTree
)

func setupSync() {
	setupRateLimits()

	log.Infow("Starting Grafana-Permission-Sync",
		"applyInterval", config.Settings.ApplyInterval.String(),
		"groupRefreshInterval", config.Settings.GroupsFetchInterval.String(),
		"grafana_url", config.Grafana.URL,
		"rules", len(config.Rules))

	// 1. grafana state
	grafanaClient := sdk.NewClient(config.Grafana.URL, config.Grafana.User+":"+config.Grafana.Password, sdk.DefaultHTTPClient)
	grafana = &grafanaState{grafanaClient, nil, make(map[uint]*grafanaOrganization), rate.NewLimiter(rate.Every(time.Second/10), 2)}

	// 2. google groups service
	var err error
	groupTree, err = groups.CreateGroupTree(log, config.Google.Domain, config.Google.AdminEmail, config.Google.CredentialsPath, config.Google.GroupBlacklist, []string{
		"https://www.googleapis.com/auth/admin.directory.group.member.readonly",
		"https://www.googleapis.com/auth/admin.directory.group.readonly",
		//"https://www.googleapis.com/auth/admin.directory.user.readonly",
	}...)
	if err != nil {
		log.Fatalw("unable to create google directory service", "error", err.Error())
	}
}

func setupRateLimits() {
	applyRateLimit = rate.NewLimiter(rate.Every(config.Settings.ApplyInterval), 1)
	googleGroupRefreshRateLimit = rate.NewLimiter(rate.Every(config.Settings.GroupsFetchInterval), 1)
	noUpdatesMessageRateLimit = rate.NewLimiter(rate.Every(noUpdatesMessageInterval), 1)
}

// startSync is the main loop, it does not return.
func startSync() {

	for {
		// 1.
		// Wait for rate limit or config change
		for {
			if applyRateLimit.Allow() {
				break // its time for the next update
			}
			if newConfig != nil {
				break // config has changed
			}
			time.Sleep(time.Millisecond * 500)
		}

		// 2.
		// Load new config (if there is one)
		next := newConfig // todo: most likely nothing will go wrong here, but it would be cleaner to do a real "interlocked compare exchange"
		if next != nil {
			config = next
			newConfig = nil
			setupRateLimits()
			log.Info("A new config has been loaded and applied!")
		}

		// 3.
		// Create and execute update plan
		if dryRunNoPlanNoExec {
			log.Warn("mode: dryRunNoPlanNoExec")
			continue // skip rest
		}

		updatePlan := createUpdatePlan()
		createdPlans++

		if len(updatePlan) > 0 {
			printPlan(updatePlan)

			if !dryRunNoExec {
				executePlan(updatePlan)
			} else {
				log.Warn("mode: dryRunNoExec")
			}
		} else {
			printNoNewUpdates()
		}
	}
}

func createUpdatePlan() []userUpdate {

	// - Grafana: fetch all users and orgs from grafana
	grafana.fetchState()

	// - Rules: from the rules get set of all groups and set of all explicit users; fetch them from google
	fetchGoogleGroups()

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
	// - do not remove anyone from orgID 1
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

	log.Info("")
	log.Infow("New update-plan computed!", "affectedUsers", len(plan), "totalChanges", totalChanges)

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

	log.Info("")
}

func executePlan(plan []userUpdate) {

	log.Infow("Applying updates to Grafana...")

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
				log.Errorw("error applying update",
					"userEmail", uu.Email,
					"org", change.Organization.Name,
					"oldRole", change.OldRole,
					"newRole", change.NewRole,
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
			log.Errorw("unable to get group", "email", groupEmail, "error", err)
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

	r := googleGroupRefreshRateLimit.Reserve()
	if r.OK() == false {
		// should not be possible because we're the only function and go-routine that ever uses this!
		log.Error("google groups refresh: rateLimit.Reserve().OK() returned false")
		return
	}

	readyIn := r.Delay()
	if readyIn > 0 {
		r.Cancel() // dont actually consume a token
		log.Debugw("refresh google groups: not ready yet", "nextRefreshAllowedIn", readyIn.String())
		return
	}

	now := time.Now()
	timeSinceLast := now.Sub(lastGoogleGroupFetch)
	lastGoogleGroupFetch = now

	groupTree.Clear()

	// prefetch all groups and users
	distinctGroups := config.getAllGroups()

	log.Infow("Refreshing google groups...", "timeSinceLastGroupFetch", timeSinceLast.String(), "groupCount", len(distinctGroups))

	for _, reqGroup := range distinctGroups {
		log.Debugw("fetching google group", "email", reqGroup)
		_, err := groupTree.GetGroup(reqGroup)
		if err != nil {
			log.Errorw("error fetching group", "error", err)
		}
	}
}

func printNoNewUpdates() {
	if noUpdatesMessageRateLimit.Allow() == false {
		return
	}

	log.Infow("Computed update plan contains no changes. (This message will be throttled in order to prevent the log from being spammed. "+"Grafana-Permission-Sync will continue to run as usual and check for updates with the same frequency.)",
		"applyInterval", config.Settings.ApplyInterval.String(),
		"noUpdatesMessageInterval", noUpdatesMessageInterval.String())
}

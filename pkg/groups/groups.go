package groups

import (
	"context"
	"fmt"
	"io/ioutil"
	"strings"

	"go.uber.org/zap"
	"golang.org/x/oauth2/google"
	admin "google.golang.org/api/admin/directory/v1"
	"google.golang.org/api/option"
)

// GroupTree is the service that deals with google groups
type GroupTree struct {
	svc    *admin.Service
	logger *zap.SugaredLogger
	domain string

	groups map[string]*Group
	users  map[string]*User
}

// Group is a 'google group', but in a more useful format than the original libarary provides
type Group struct {
	Email string

	Parent *Group
	Groups []*Group
	Users  []*User
}

// User is a more useful version of a google user
type User struct {
	Email  string
	Groups []*Group // Groups a user is in (todo: does this include 'implicit' / 'parent' groups??)
}

// AllUsers constructs a slice containing all users of the group (including users of all nested groups recursively)
func (g *Group) AllUsers() []*User {
	var result []*User

	openSet := []*Group{g}

	addGroup := func(newGroup *Group) {
		for _, gr := range openSet {
			if gr.Email == newGroup.Email {
				return // already present, don't add
			}
		}
		openSet = append(openSet, newGroup)
	}

	addUser := func(newUser *User) {
		for _, u := range result {
			if u.Email == newUser.Email {
				return // already present, don't add
			}
		}
		result = append(result, newUser)
	}

	for i := 0; i < len(openSet); i++ {
		// add all users in that group
		for _, u := range g.Users {
			addUser(u)
		}
		// and also add all sub-groups to the exploration list
		for _, subGroup := range g.Groups {
			addGroup(subGroup)
		}
	}

	return result
}

// CreateGroupTree -
func CreateGroupTree(logger *zap.SugaredLogger, domain string, userEmail string, serviceAccountFilePath string, scopes ...string) (*GroupTree, error) {
	ctx := context.Background()
	log := logger

	log.Infow("loading creds", "path", serviceAccountFilePath)
	jsonCredentials, err := ioutil.ReadFile(serviceAccountFilePath)
	if err != nil {
		return nil, err
	}

	config, err := google.JWTConfigFromJSON(jsonCredentials, scopes...)
	if err != nil {
		return nil, fmt.Errorf("JWTConfigFromJSON: %v", err)
	}
	config.Subject = userEmail

	ts := config.TokenSource(ctx)

	svc, err := admin.NewService(ctx, option.WithTokenSource(ts))
	if err != nil {
		return nil, fmt.Errorf("NewService: %v", err)
	}
	return &GroupTree{svc, logger, domain, map[string]*Group{}, make(map[string]*User)}, nil
}

// Clear removes all groups and users from the cache
func (g *GroupTree) Clear() {
	g.groups = make(map[string]*Group)
	g.users = make(map[string]*User)
}

// ListGroupMembersRaw finds all members in a group
func (g *GroupTree) ListGroupMembersRaw(groupKey string, includeDerived bool) (result []*admin.Member, err error) {
	// g.logger.Infof("listing members for group: %v", groupKey)
	result = []*admin.Member{}

	err = g.svc.Members.List(groupKey).IncludeDerivedMembership(includeDerived).Pages(context.Background(), func(page *admin.Members) error {
		for _, member := range page.Members {
			result = append(result, member)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return result, nil
}

// ListGroupMembers same as ListGroupMembersRaw, but packages them into a better serializable format
func (g *GroupTree) ListGroupMembers(groupKey string, includeDerived bool) (result []map[string]interface{}, err error) {
	result = make([]map[string]interface{}, 0)

	// We handle lookup of nested groups manually, so we pass 'false'
	members, err := g.ListGroupMembersRaw(groupKey, false)
	if err != nil {
		return nil, err
	}

	// Only take the fields we want, correctly nest sub-groups
	for _, member := range members {
		element := map[string]interface{}{
			"id":    member.Id,
			"type":  member.Type,
			"email": member.Email,
		}

		if includeDerived && member.Type == "GROUP" {
			subGroup, err := g.ListGroupMembers(member.Id, includeDerived)
			if err != nil {
				element["error"] = fmt.Sprintf("cannot resolve subgroup '%v' (in group '%v'): %v", member.Email, groupKey, err.Error())
			} else {
				element["items"] = subGroup
			}
		}

		result = append(result, element)
	}
	return result, nil
}

// ListUserGroups finds all groups a user is a member in. userKey can be primaryEmail, any aliasEmail, or the unique userID
func (g *GroupTree) ListUserGroups(userKey string) (groups []map[string]interface{}, err error) {
	groups = make([]map[string]interface{}, 0)
	err = g.svc.Groups.List().Domain(g.domain).UserKey(userKey).Pages(context.Background(), func(page *admin.Groups) error {
		for _, group := range page.Groups {
			groups = append(groups, map[string]interface{}{
				"name":  group.Name,
				"email": group.Email,
			})
		}
		return nil
	})

	return groups, err
}

// ListAllGroups -
func (g *GroupTree) ListAllGroups() {
	var (
		totalCount, totalPages = 0, 0
		groupMap               = make(map[string]*Group)
	)

	err := g.svc.Groups.List().Domain(g.domain).Pages(context.Background(), func(page *admin.Groups) error {
		totalPages++
		for _, group := range page.Groups {
			totalCount++

			g.logger.Infow("group",
				"email", group.Email,
				"aliases", strings.Join(group.Aliases, ", "),
				"nonEditAlias", strings.Join(group.NonEditableAliases, ", "),
				"description", group.Description,
				"name", group.Name)

			entry := &Group{group.Email, nil, nil, nil}
			groupMap[group.Email] = entry
			for _, alias := range append(group.Aliases, group.NonEditableAliases...) {
				groupMap[alias] = entry // add under alias names as well
			}
		}
		return nil
	})

	if err != nil {
		g.logger.Fatalw("unable to list groups", "error", err.Error())
	}

	g.logger.Infow("done", "totalCount", totalCount, "totalPages", totalPages)
}

// GetUser -
func (g *GroupTree) GetUser(email string) *User {
	user, exists := g.users[email]
	if !exists {
		// googleUser :=
		// user := &User{
		// }
		// g.users[email] = user
	}
	return user
}

// GetGroup -
func (g *GroupTree) GetGroup(email string) (*Group, error) {
	grp, exists := g.groups[email]
	if exists {
		return grp, nil // already cached
	}

	members, err := g.ListGroupMembersRaw(email, false)
	if err != nil {
		g.logger.Warnw("error listing group members", "groupEmail", email)
		return nil, err // error
	}

	grp = &Group{Email: email} // create new
	g.groups[email] = grp

	for _, m := range members {
		if m.Type == "GROUP" {
			subGroup, err := g.GetGroup(m.Email) // cache that sub group as well
			if err != nil {
				continue
			}

			grp.Groups = append(grp.Groups, subGroup) // add it as a child
			subGroup.Parent = grp                     // set parent
		} else if m.Type == "USER" {
			// cache user
			u, exists := g.users[m.Email]
			if !exists {
				u = &User{m.Email, []*Group{grp}}
				g.users[m.Email] = u
			} else {
				// update (if neccesary)
				u.Groups = append(u.Groups, grp)
			}

			grp.Users = append(grp.Users, u)
		} else {
			g.logger.Fatalw("unknown member type in google group", "group", email, "memberType", m.Type, "memberId", m.Id, "memberEmail", m.Email)
		}
	}

	return grp, nil
}

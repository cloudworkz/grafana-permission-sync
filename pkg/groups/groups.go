package groups

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"regexp"
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

	groups         map[string]*Group
	users          map[string]*User
	groupBlacklist []string
}

// Group is a 'google group', but in a more useful format than the original libarary provides
type Group struct {
	Email string

	Groups []*Group
	Users  []*User
}

// User is a more useful version of a google user
type User struct {
	Email string
	// Groups []*Group // Groups a user is in directly
	// AllGroups []*Group // Groups + all indirect groups
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
		current := openSet[i]
		if current == nil {
			continue
		}

		// add all users in that group
		for _, u := range current.Users {
			if u != nil {
				addUser(u)
			}
		}
		// and also add all sub-groups to the exploration list
		for _, subGroup := range current.Groups {
			if subGroup != nil {
				addGroup(subGroup)
			}
		}
	}

	return result
}

// CreateGroupTree -
func CreateGroupTree(logger *zap.SugaredLogger, domain string, userEmail string, serviceAccountFilePath string, groupBlacklist []string, scopes ...string) (*GroupTree, error) {
	ctx := context.Background()
	log := logger

	var svc *admin.Service
	if serviceAccountFilePath != "" {
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

		svc, err = admin.NewService(ctx, option.WithTokenSource(ts))
		if err != nil {
			return nil, fmt.Errorf("NewService: %v", err)
		}
	} else {
		var err error
		svc, err = admin.NewService(ctx)
		if err != nil {
			return nil, fmt.Errorf("NewService: %v", err)
		}
	}
	return &GroupTree{svc, logger, domain, map[string]*Group{}, make(map[string]*User), groupBlacklist}, nil
}

// Clear removes all groups and users from the cache
func (g *GroupTree) Clear() {
	g.groups = make(map[string]*Group)
	g.users = make(map[string]*User)
}

// ListGroupMembersRaw finds all members in a group
func (g *GroupTree) ListGroupMembersRaw(groupKey string) (result []*admin.Member, err error) {
	// g.logger.Infof("listing members for group: %v", groupKey)
	result = []*admin.Member{}

	err = g.svc.Members.List(groupKey).IncludeDerivedMembership(false).Pages(context.Background(), func(page *admin.Members) error {
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

// GetGroup -
func (g *GroupTree) GetGroup(email string) (*Group, error) {
	grp, exists := g.groups[email]
	if exists {
		return grp, nil // return existing
	}

	// Check blacklist
	isBlacklisted, reason := g.isGroupInBlacklist(email)
	if isBlacklisted {
		g.logger.Infow("Skipping group because it is blacklisted", "groupEmail", email, "pattern", reason)
		return nil, errors.New("group is blacklisted by: '" + reason + "'")
	}

	members, err := g.ListGroupMembersRaw(email)
	if err != nil {
		g.logger.Warnw("error listing group members", "groupEmail", email, "err", err)
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
		} else if m.Type == "USER" {
			// cache user
			u, exists := g.users[m.Email]
			if !exists {
				u = &User{m.Email}
				g.users[m.Email] = u
			}

			grp.Users = append(grp.Users, u)
		} else {
			g.logger.Fatalw("unknown member type in google group", "group", email, "memberType", m.Type, "memberId", m.Id, "memberEmail", m.Email)
		}
	}

	return grp, nil
}

func (g *GroupTree) isGroupInBlacklist(email string) (isBlacklisted bool, reason string) {
	for _, item := range g.groupBlacklist {
		if strings.HasPrefix(item, "/") && strings.HasSuffix(item, "/") {
			// regex match
			pattern := item[1 : len(item)-1]
			isMatch, err := regexp.MatchString(pattern, email)
			if err == nil && isMatch {
				return true, item
			}
		} else {
			// regular match
			if item == email {
				return true, item
			}
		}
	}

	return false, ""
}

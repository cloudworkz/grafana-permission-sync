package groups

import (
	"context"
	"fmt"

	admin "google.golang.org/api/admin/directory/v1"
)

// ListGroupMembersForDisplay same as ListGroupMembersRaw, but packages them into a better serializable format
func (g *GroupTree) ListGroupMembersForDisplay(groupKey string, includeDerived bool) (result []map[string]interface{}, err error) {
	result = make([]map[string]interface{}, 0)

	// We handle lookup of nested groups manually, so we pass 'false'
	members, err := g.ListGroupMembersRaw(groupKey)
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
			subGroup, err := g.ListGroupMembersForDisplay(member.Id, includeDerived)
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

// ListUserGroupsForDisplay finds all groups a user is a member in. userKey can be primaryEmail, any aliasEmail, or the unique userID
func (g *GroupTree) ListUserGroupsForDisplay(userKey string) (groups []map[string]interface{}, err error) {
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

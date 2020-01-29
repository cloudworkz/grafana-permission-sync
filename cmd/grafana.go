package main

import (
	"github.com/rikimaru0345/sdk"
)

type grafanaState struct {
	*sdk.Client

	// todo: move this into a separate "Grafana" package and don't use package-globals
	allUsers      []sdk.User
	organizations map[uint]*grafanaOrganization // [orgID]Org

}

type grafanaOrganization struct {
	*sdk.Org
	Users []sdk.OrgUser
}

func (g *grafanaState) fetchState() {

	// get all users (including those that don't belong to any org)
	var err error
	g.allUsers, err = g.GetAllUsers()
	if err != nil {
		log.Errorf("unable to fetch all users from grafana: %v", err.Error())
		return
	}

	// get all orgs...
	g.organizations = make(map[uint]*grafanaOrganization)
	orgs, err := g.GetAllOrgs()
	if err != nil {
		log.Errorf("unable to list all orgs: %v" + err.Error())
		return
	}

	for _, org := range orgs {
		// ...and their users
		users, err := g.GetOrgUsers(org.ID)
		if err != nil {
			log.Error("error listing users for org: " + err.Error())
			continue
		}
		orgCopy := org // need to create a local copy of the org...
		g.organizations[org.ID] = &grafanaOrganization{&orgCopy, users}
	}
}

func (g *grafanaOrganization) findUser(userEmail string) *sdk.OrgUser {
	for _, u := range g.Users {
		if u.Email == userEmail {
			return &u
		}
	}
	return nil
}

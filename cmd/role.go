package main

import (
	basicLog "log"
)

// Role is one of the 4 grafana roles: viewer, editor, admin, and <empty> (meaning user is not part of the org)
type Role string

var (
	roleLevels = map[Role]int{
		"":       0,
		"Viewer": 1,
		"Editor": 2,
		"Admin":  3,
	}
)

func (r Role) level() int {
	level, exists := roleLevels[r]
	if !exists {
		basicLog.Fatal("invalid role: " + string(r))
	}

	return level
}

// Compare two roles, returning the higher role (the one that gives more permissions)
func (r Role) isHigherThan(other Role) bool {
	thisLevel := r.level()
	otherLevel := other.level()

	if thisLevel > otherLevel {
		return true
	}
	return false
}
func (r Role) isHigherOrEqThan(other Role) bool {
	thisLevel := r.level()
	otherLevel := other.level()

	if thisLevel >= otherLevel {
		return true
	}
	return false
}
func (r Role) isLowerThan(other Role) bool {
	return !r.isHigherOrEqThan(other)
}

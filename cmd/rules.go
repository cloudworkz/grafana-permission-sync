package main

import "errors"

// Rule is a single mapping rule that specifies
// what google groups/users get what grafana-role in which grafana-org
type Rule struct {
	Groups        FlattenedArray `yaml:"groups"`
	Users         FlattenedArray `yaml:"users"`
	Organizations FlattenedArray `yaml:"orgs"`
	Role          Role           `yaml:"role"`
}

func (r *Rule) verify() error {
	if r.Role != "Viewer" && r.Role != "Editor" && r.Role != "Admin" {
		return errors.New("Invalid role \"%s\". Must be one of [Viewer, Editor, Admin]")
	}

	return nil
}

// FlattenedArray -
type FlattenedArray []string

// UnmarshalYAML -
func (r *FlattenedArray) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var x []interface{}
	if err := unmarshal(&x); err != nil {
		return err
	}

	// Unpack nested arrays; remove duplicates
	*r = distinct(flatten(x))

	return nil
}

func flatten(ar []interface{}) (r []string) {
	for _, item := range ar {
		switch i := item.(type) {
		case string:
			r = append(r, i)
		case []interface{}:
			r = append(r, flatten(i)...)
		}
	}
	return
}

func contains(ar []string, item string) bool {
	for _, e := range ar {
		if e == item {
			return true
		}
	}
	return false
}

func distinct(ar []string) []string {
	seen := make(map[string]bool)
	var new []string

	for _, str := range ar {
		if seen[str] == false {
			new = append(new, str)
			seen[str] = true
		}
	}

	return new
}

package main

import "errors"

import "strings"

import "regexp"

// Rule is a single mapping rule that specifies
// what google groups/users get what grafana-role in which grafana-org
type Rule struct {
	Note  string `yaml:"note"` // will be displayed in the 'reason' field for every change
	Index int    // used as a fallback reason

	Groups        FlattenedArray `yaml:"groups"`
	Users         FlattenedArray `yaml:"users"`
	Organizations FlattenedArray `yaml:"orgs"`
	Role          Role           `yaml:"role"`
}

func (r *Rule) verify() error {
	if r.Role != "Viewer" && r.Role != "Editor" && r.Role != "Admin" {
		return errors.New("Invalid role \"%s\". Must be one of [Viewer, Editor, Admin]")
	}

	for _, o := range r.Organizations {
		if strings.HasPrefix(o, "/") && strings.HasSuffix(o, "/") {
			pattern := o[1 : len(o)-1]
			_, err := regexp.Compile(pattern)
			if err != nil {
				log.Fatalw("your regex pattern can not be compiled", "pattern", pattern, "error", err.Error())
			}
		}
	}

	return nil
}

func (r *Rule) matchesOrg(org string) bool {
	// check if it contains an exact match, or regex match
	for _, item := range r.Organizations {
		if strings.HasPrefix(item, "/") && strings.HasSuffix(item, "/") {
			// is regex match?
			pattern := item[1 : len(item)-1]
			isMatch, err := regexp.MatchString(pattern, org)
			if err == nil && isMatch {
				return true
			}
		} else {
			// is regular match?
			if item == org {
				return true
			}
		}
	}

	return false
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

package parser

import (
	"github.com/mirkobrombin/go-foundation/pkg/tags"
)

// Policy represents the security rules extracted from a struct tag.
type Policy struct {
	Roles          []string
	Permissions    map[string][]string
	UseValueAsRole bool
}

var tagParser = tags.NewParser("guard")

// Parse extracts policy information from a 'guard' struct tag.
func Parse(tag string) *Policy {
	p := &Policy{
		Permissions: make(map[string][]string),
	}

	parsed := tagParser.Parse(tag)

	for key, values := range parsed {
		if key == "role" {
			if len(values) == 1 && values[0] == "*" {
				p.UseValueAsRole = true
			} else {
				p.Roles = append(p.Roles, values...)
			}
			continue
		}

		p.Permissions[key] = values
	}

	return p
}

package configuration

import (
	"fmt"
	"sort"
	"strings"
)

// BindCollection binds every first-level child section of prefix into a T,
// keyed by section name. It is the typed counterpart of loading a directory
// of files with source/dir: each file becomes one entry in the returned map.
//
// Example:
//
//	cfg := configuration.NewBuilder().Add(dir.New("tenants", "*.json")).Build(ctx)
//	tenants, err := configuration.BindCollection[TenantConfig](cfg, "")
//	// tenants["acme"].Quota, tenants["globex"].Quota, ...
//
// With a non-empty prefix the collection is read one level below it:
// BindCollection[T](cfg, "tenants") binds "tenants:<name>:*" into entries
// keyed by <name>.
func BindCollection[T any](c *Configuration, prefix string) (map[string]*T, error) {
	names := c.CollectionKeys(prefix)
	items := make(map[string]*T, len(names))
	for _, name := range names {
		section := c
		if prefix != "" {
			section = c.GetSection(prefix + ":" + name)
		} else {
			section = c.GetSection(name)
		}
		item := new(T)
		if err := section.Bind(item); err != nil {
			return nil, fmt.Errorf("configuration: entry %q: %w", name, err)
		}
		items[name] = item
	}
	return items, nil
}

// CollectionKeys returns the sorted names of the first-level child sections
// under prefix. With an empty prefix it returns the top-level key segments.
//
// Keys are stored lowercased, so returned names are lowercase regardless of
// the original file or key casing.
func (c *Configuration) CollectionKeys(prefix string) []string {
	p := ""
	if prefix != "" {
		p = strings.ToLower(prefix) + ":"
	}

	seen := make(map[string]struct{})
	c.mu.RLock()
	for k := range c.data {
		if !strings.HasPrefix(k, p) {
			continue
		}
		rest := strings.TrimPrefix(k, p)
		seg, _, _ := strings.Cut(rest, ":")
		if seg != "" {
			seen[seg] = struct{}{}
		}
	}
	c.mu.RUnlock()

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ItemValidator reports problems with a single bound entry. The returned
// slice holds human-readable problem descriptions; empty means valid.
type ItemValidator[T any] func(name string, item *T) []string

// CrossValidator reports problems that only exist across entries, such as
// duplicate ports or overlapping domains. The returned slice holds
// human-readable problem descriptions; empty means valid.
type CrossValidator[T any] func(items map[string]*T) []string

// ValidateCollection runs the item validator on every entry (in sorted name
// order) and then every cross validator on the whole collection, aggregating
// all problems. Entry problems are prefixed with the entry name.
//
// Returning every problem at once matters for the "validate my config
// directory" workflow: fixing one file at a time is the slow path.
func ValidateCollection[T any](items map[string]*T, item ItemValidator[T], cross ...CrossValidator[T]) []string {
	var problems []string

	if item != nil {
		names := make([]string, 0, len(items))
		for name := range items {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			for _, p := range item(name, items[name]) {
				problems = append(problems, fmt.Sprintf("%s: %s", name, p))
			}
		}
	}

	for _, cv := range cross {
		if cv != nil {
			problems = append(problems, cv(items)...)
		}
	}
	return problems
}

package configuration

import (
	"strings"
	"testing"
)

type siteConfig struct {
	Domain string `conf:"domain"`
	Port   int    `conf:"port"`
}

func collectionFixture(t *testing.T) *Configuration {
	t.Helper()
	c := New()
	c.data["a.example:domain"] = "a.example"
	c.data["a.example:port"] = "8080"
	c.data["b.example:domain"] = "b.example"
	c.data["b.example:port"] = "9090"
	return c
}

func TestBindCollectionBindsEachSection(t *testing.T) {
	sites, err := BindCollection[siteConfig](collectionFixture(t), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 2 {
		t.Fatalf("got %d entries, want 2", len(sites))
	}
	if sites["a.example"].Domain != "a.example" || sites["a.example"].Port != 8080 {
		t.Errorf("unexpected a.example: %+v", sites["a.example"])
	}
	if sites["b.example"].Port != 9090 {
		t.Errorf("unexpected b.example: %+v", sites["b.example"])
	}
}

func TestBindCollectionWithPrefix(t *testing.T) {
	c := New()
	c.data["sites:a:port"] = "8080"
	c.data["other:b:port"] = "1"

	sites, err := BindCollection[siteConfig](c, "sites")
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 1 || sites["a"].Port != 8080 {
		t.Fatalf("unexpected result: %+v", sites)
	}
}

func TestCollectionKeysSortedAndTopLevelOnly(t *testing.T) {
	c := New()
	c.data["z.example:ssl:enabled"] = "true"
	c.data["a.example:port"] = "1"
	c.data["m.example:port"] = "2"

	keys := c.CollectionKeys("")
	want := []string{"a.example", "m.example", "z.example"}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", keys, want)
	}
}

func TestValidateCollectionAggregatesItemAndCrossProblems(t *testing.T) {
	sites, err := BindCollection[siteConfig](collectionFixture(t), "")
	if err != nil {
		t.Fatal(err)
	}

	item := func(name string, s *siteConfig) []string {
		if s.Domain == "" {
			return []string{"domain is required"}
		}
		return nil
	}
	cross := func(items map[string]*siteConfig) []string {
		seen := map[int]string{}
		var out []string
		for name, s := range items {
			if prev, dup := seen[s.Port]; dup {
				out = append(out, "port conflict between "+prev+" and "+name)
			}
			seen[s.Port] = name
		}
		return out
	}

	if problems := ValidateCollection(sites, item, cross); len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}

	sites["b.example"].Port = sites["a.example"].Port
	sites["b.example"].Domain = ""
	problems := ValidateCollection(sites, item, cross)
	if len(problems) != 2 {
		t.Fatalf("got %v, want 2 problems", problems)
	}
	if !strings.HasPrefix(problems[0], "b.example: ") {
		t.Errorf("item problem not prefixed with entry name: %q", problems[0])
	}
}

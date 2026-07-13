package parser

import "testing"

func TestParseStaticRolesAndPermissions(t *testing.T) {
	policy := Parse("role:admin,editor; read:document; write:document")

	if len(policy.Roles) != 2 || policy.Roles[0] != "admin" || policy.Roles[1] != "editor" {
		t.Fatalf("Roles = %v", policy.Roles)
	}
	if policy.UseValueAsRole {
		t.Fatal("UseValueAsRole = true, want false")
	}
	if got := policy.Permissions["read"]; len(got) != 1 || got[0] != "document" {
		t.Fatalf("Permissions[read] = %v", got)
	}
	if got := policy.Permissions["write"]; len(got) != 1 || got[0] != "document" {
		t.Fatalf("Permissions[write] = %v", got)
	}
}

func TestParseDynamicRole(t *testing.T) {
	policy := Parse("role:*; manage:project")

	if !policy.UseValueAsRole {
		t.Fatal("UseValueAsRole = false, want true")
	}
	if len(policy.Roles) != 0 {
		t.Fatalf("Roles = %v, want none", policy.Roles)
	}
	if got := policy.Permissions["manage"]; len(got) != 1 || got[0] != "project" {
		t.Fatalf("Permissions[manage] = %v", got)
	}
}

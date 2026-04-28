package guard_test

import (
	"testing"

	"github.com/mirkobrombin/go-foundation/pkg/guard"
)

type Document struct {
	Title   string `guard:"role:admin; can:read,write"`
	Content string `guard:"role:editor; can:edit"`
	Public  string `guard:"role:*; can:view"`
	OwnerID string `guard:"role:*; can:delete"`
}

type DynamicDoc struct {
	Meta map[string]string `guard:"role:*; can:manage"`
}

type MockUser struct {
	ID    string
	Roles []string
}

func (u *MockUser) GetID() string      { return u.ID }
func (u *MockUser) GetRoles() []string { return u.Roles }

func TestGuard_Can(t *testing.T) {
	g := guard.NewGuard()

	admin := &MockUser{ID: "1", Roles: []string{"admin"}}
	editor := &MockUser{ID: "2", Roles: []string{"editor"}}
	owner := &MockUser{ID: "3", Roles: []string{"user"}}
	stranger := &MockUser{ID: "4", Roles: []string{"guest"}}

	doc := &Document{
		Title:   "Secret",
		Content: "Stuff",
		Public:  "guest",
		OwnerID: "3",
	}

	tests := []struct {
		name     string
		user     guard.Identity
		resource *Document
		action   string
		wantErr  bool
	}{
		{"Admin Read Title", admin, doc, "read", false},
		{"Admin Write Title", admin, doc, "write", false},
		{"Admin Edit Content Fail", admin, doc, "edit", true},
		{"Editor Read Title Fail", editor, doc, "read", true},
		{"Editor Edit Content", editor, doc, "edit", false},
		{"Guest View Public", stranger, doc, "view", false},
		{"Owner Delete Fail", owner, doc, "delete", true},
		{"Stranger Delete Fail", stranger, doc, "delete", true},
		{"Unknown Action Fail", admin, doc, "unknown", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := g.Can(tt.user, tt.resource, tt.action)
			if (err != nil) != tt.wantErr {
				t.Errorf("Can() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGuard_Can_NilIdentity(t *testing.T) {
	g := guard.NewGuard()
	err := g.Can(nil, &Document{}, "read")
	if err == nil {
		t.Error("expected error for nil identity")
	}
}

func TestGuard_Can_NilResource(t *testing.T) {
	g := guard.NewGuard()
	err := g.Can(&MockUser{ID: "1", Roles: []string{"admin"}}, nil, "read")
	if err == nil {
		t.Error("expected error for nil resource")
	}
}

func TestGuard_Can_NonStructResource(t *testing.T) {
	g := guard.NewGuard()
	err := g.Can(&MockUser{ID: "1", Roles: []string{"admin"}}, "string", "read")
	if err == nil {
		t.Error("expected error for non-struct resource")
	}
}

func TestGuard_GetRoles(t *testing.T) {
	g := guard.NewGuard()
	admin := &MockUser{ID: "1", Roles: []string{"admin", "editor"}}
	doc := &Document{Title: "T", Content: "C", Public: "guest", OwnerID: "3"}

	roles, err := g.GetRoles(admin, doc)
	if err != nil {
		t.Fatalf("GetRoles failed: %v", err)
	}
	if len(roles) == 0 {
		t.Error("expected at least one role, got none")
	}
}

func TestGuard_GetRoles_Nil(t *testing.T) {
	g := guard.NewGuard()
	_, err := g.GetRoles(nil, &Document{})
	if err == nil {
		t.Error("expected error for nil identity")
	}
	_, err = g.GetRoles(&MockUser{ID: "1", Roles: []string{"admin"}}, nil)
	if err == nil {
		t.Error("expected error for nil resource")
	}
}

type PolicyDoc struct {
	Name   string `guard:"can:read"`
	Secret string `guard:"role:admin; can:read"`
}

func TestGuard_NoPolicyDefined(t *testing.T) {
	g := guard.NewGuard()
	admin := &MockUser{ID: "1", Roles: []string{"admin"}}
	doc := &PolicyDoc{Name: "hello", Secret: "secret"}

	err := g.Can(admin, doc, "read")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGuard_GetRoles_DynamicDoc(t *testing.T) {
	g := guard.NewGuard()
	user := &MockUser{ID: "123", Roles: []string{"user"}}
	doc := &DynamicDoc{Meta: map[string]string{"123": "owner", "456": "viewer"}}

	roles, err := g.GetRoles(user, doc)
	if err != nil {
		t.Fatalf("GetRoles failed: %v", err)
	}
	foundOwner := false
	for _, r := range roles {
		if r == "owner" {
			foundOwner = true
		}
	}
	if !foundOwner {
		t.Errorf("expected 'owner' role for user 123, got %v", roles)
	}
}

func TestGuard_Can_DynamicDoc(t *testing.T) {
	g := guard.NewGuard()
	user := &MockUser{ID: "123", Roles: []string{"owner"}}
	doc := &DynamicDoc{Meta: map[string]string{"123": "owner", "456": "viewer"}}

	err := g.Can(user, doc, "manage")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	stranger := &MockUser{ID: "789", Roles: []string{"user"}}
	err = g.Can(stranger, doc, "manage")
	if err == nil {
		t.Error("expected error for stranger")
	}
}

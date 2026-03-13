package pointer_test

import (
	"testing"

	"github.com/mirkobrombin/go-foundation/pkg/pointer"
)

type User struct {
	ID    int    `db:"primary_key; column:user_id"`
	Name  string `db:"column:full_name"`
	Email string
}

func TestRegistry(t *testing.T) {
	reg := pointer.NewRegistry("db")
	reg.Register(User{})

	u := &User{}

	if name := pointer.FieldName(reg, u, &u.Name); name != "Name" {
		t.Errorf("expected Name, got %s", name)
	}

	if col := pointer.TagValue(reg, u, &u.ID, "column"); col != "user_id" {
		t.Errorf("expected user_id, got %s", col)
	}

	if col := pointer.TagValue(reg, u, &u.Name, "column"); col != "full_name" {
		t.Errorf("expected full_name, got %s", col)
	}

	if !pointer.HasTag(reg, u, &u.ID, "primary_key") {
		t.Error("expected primary_key tag to exist")
	}

	if pointer.HasTag(reg, u, &u.Email, "db") {
		t.Error("expected no tags for Email")
	}
}

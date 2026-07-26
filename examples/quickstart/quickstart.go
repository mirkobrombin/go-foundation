// Package quickstart shows the static Foundation v2 workflow.
package quickstart

import (
	"context"
	"fmt"
	"sync"

	"github.com/mirkobrombin/go-foundation/v2/app"
	"github.com/mirkobrombin/go-foundation/v2/core/contracts"
)

// User is the model returned by the example handlers.
type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// UserStore is the application contract used by the example.
type UserStore interface {
	Create(name string) User
	Find(id int) (User, bool)
}

// MemoryUserStore is an in-memory UserStore implementation.
type MemoryUserStore struct {
	contracts.Implements[UserStore]

	mu    sync.RWMutex
	users map[int]User
	next  int
}

// NewMemoryUserStore creates an empty store.
func NewMemoryUserStore() *MemoryUserStore {
	return &MemoryUserStore{
		users: make(map[int]User),
		next:  1,
	}
}

// Create stores a user.
func (s *MemoryUserStore) Create(name string) User {
	s.mu.Lock()
	defer s.mu.Unlock()

	user := User{ID: s.next, Name: name}
	s.users[user.ID] = user
	s.next++
	return user
}

// Find returns a user by ID.
func (s *MemoryUserStore) Find(id int) (User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user, ok := s.users[id]
	return user, ok
}

// GetUser is a generated HTTP registration target.
type GetUser struct {
	_     struct{}  `method:"GET" path:"/users/{id:int}"`
	ID    int       `path:"id"`
	Users UserStore `inject:"users"`
}

// Handle returns the requested user.
func (h *GetUser) Handle(context.Context) (any, error) {
	user, ok := h.Users.Find(h.ID)
	if !ok {
		return nil, fmt.Errorf("user %d not found", h.ID)
	}
	return user, nil
}

// CreateUser is a generated action registration target.
type CreateUser struct {
	_     struct{}  `action:"users.create" keys:"ctrl+n"`
	Name  string    `json:"name"`
	Users UserStore `inject:"users"`
}

// Handle creates a user.
func (a *CreateUser) Handle(context.Context) (any, error) {
	if a.Name == "" {
		return nil, fmt.Errorf("user name is required")
	}
	return a.Users.Create(a.Name), nil
}

// Build creates a ready application using generated registrations.
func Build() (*app.App, error) {
	application := app.New().
		Provide("users", NewMemoryUserStore())
	RegisterFoundation(application)
	if _, err := application.Build(); err != nil {
		return nil, err
	}
	return application, nil
}

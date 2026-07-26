package valid

import (
	"context"

	"github.com/mirkobrombin/go-foundation/v2/app"
	"github.com/mirkobrombin/go-foundation/v2/app/di"
	"github.com/mirkobrombin/go-foundation/v2/core/contracts"
)

type Store interface {
	Get() string
}

type MemoryStore struct {
	contracts.Implements[Store]
}

func (s *MemoryStore) Get() string {
	return "value"
}

type GetItem struct {
	_     struct{} `method:"GET" path:"/items/{id:int}"`
	ID    int      `path:"id"`
	Store Store    `inject:"store"`
}

func (h *GetItem) Handle(context.Context) (any, error) {
	return h.Store.Get(), nil
}

// foundation:ignore handler
type SchemaOnly struct {
	_ struct{} `method:"GET" path:"/schema"`
}

func build() {
	app.New().Schedule("cleanup", "0 3 * * *", func(context.Context) error { return nil })
	b := di.NewBuilder()
	b.Provide("store", &MemoryStore{})
	di.RegisterImpl[Store, *MemoryStore](b)
	di.RegisterAs[Store](b, func() *MemoryStore { return &MemoryStore{} })
	di.RegisterFromFunc[Store](b, func() Store { return &MemoryStore{} })
}

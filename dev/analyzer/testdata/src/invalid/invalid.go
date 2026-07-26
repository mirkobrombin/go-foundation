package invalid

import (
	"context"

	"github.com/mirkobrombin/go-foundation/v2/app"
	"github.com/mirkobrombin/go-foundation/v2/app/di"
	"github.com/mirkobrombin/go-foundation/v2/core/bind"
	"github.com/mirkobrombin/go-foundation/v2/core/contracts"
)

type Store interface {
	Get() string
}

type BrokenStore struct {
	contracts.Implements[Store] // want "BrokenStore.*does not implement"
}

type MissingPath struct {
	_ struct{} `method:"GET"` // want "must declare method and path"
}

func (h *MissingPath) Handle(context.Context) (any, error) {
	return nil, nil
}

type BadRoute struct {
	_     struct{} `method:"FETCH" path:"items/{id}"` // want "unsupported HTTP method" "route path must start"
	Other string   `path:"other"`
}

func (h *BadRoute) Handle(context.Context) (any, error) {
	return nil, nil
}

type MissingField struct {
	_ struct{} `method:"GET" path:"/items/{id}"` // want "route parameter.*has no path field"
}

func (h *MissingField) Handle(context.Context) (any, error) {
	return nil, nil
}

type UnknownConstraint struct {
	_  struct{} `method:"GET" path:"/unknown/{id:innt}"` // want "unknown route constraint"
	ID string   `path:"id"`
}

func (h *UnknownConstraint) Handle(context.Context) (any, error) {
	return nil, nil
}

type InvalidRegex struct {
	_  struct{} `method:"GET" path:"/regex/{id:regex([)}"` // want "invalid regex constraint"
	ID string   `path:"id"`
}

func (h *InvalidRegex) Handle(context.Context) (any, error) {
	return nil, nil
}

type InvalidCatchAll struct {
	_    struct{} `method:"GET" path:"/files/{*path:int}"` // want "catch-all parameters cannot have constraints"
	Path string   `path:"path"`
}

func (h *InvalidCatchAll) Handle(context.Context) (any, error) {
	return nil, nil
}

type ParameterBranch struct {
	_  struct{} `method:"GET" path:"/ambiguous/{id}/details"`
	ID string   `path:"id"`
}

func (h *ParameterBranch) Handle(context.Context) (any, error) {
	return nil, nil
}

type ConflictingCatchAll struct {
	_    struct{} `method:"POST" path:"/ambiguous/{*path}"` // want "conflicts with parameter route"
	Path string   `path:"path"`
}

func (h *ConflictingCatchAll) Handle(context.Context) (any, error) {
	return nil, nil
}

type InvalidParameterName struct {
	_  struct{} `method:"GET" path:"/invalid/{{id}}"` // want "invalid parameter name"
	ID string   `path:"id"`
}

func (h *InvalidParameterName) Handle(context.Context) (any, error) {
	return nil, nil
}

type InvalidHandler struct {
	_ struct{} `action:"invalid.handler"` // want "must implement Handle"
}

type BadInjection struct {
	Value  int `inject:"value"` // want "dependency.*has type string, want int"
	hidden int `inject:"value"` // want "injected field hidden must be exported" "dependency.*has type string, want int"
}

func check() {
	application := app.New()
	application.Schedule("", "* * * * *", nil) // want "scheduled job name cannot be empty" "scheduled job handler cannot be nil"
	application.Schedule("duplicate", "* * * * *", func(context.Context) error { return nil })
	application.Schedule("duplicate", "60 * * * *", func(context.Context) error { return nil }) // want "scheduled job.*already registered" "invalid cron field"
	b := di.NewBuilder()
	b.Provide("value", "wrong")
	di.RegisterImpl[Store, *BrokenStore](b)                     // want "does not implement"
	di.RegisterAs[Store](b, func() *BrokenStore { return nil }) // want "does not implement"
	di.RegisterFromFunc[Store](b, func() int { return 0 })      // want "constructor returns int, want invalid.Store"

	target := struct{}{}
	bind.New().Bind(&target)              // want "binding error is ignored"
	_ = bind.New().BindJSON(&target, nil) // want "binding error is ignored"
}

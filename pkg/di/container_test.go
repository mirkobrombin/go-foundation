package di

import (
	"io"
	"testing"

	"github.com/mirkobrombin/go-foundation/pkg/contracts"
)

type testDB struct {
	Name string
}

type testService struct {
	DB     *testDB `inject:"db"`
	Logger string  `inject:"logger"`
}

func TestContainer_ProvideAndGet(t *testing.T) {
	c := New()
	db := &testDB{Name: "test"}
	c.Provide("db", db)

	got, ok := c.Get("db")
	if !ok {
		t.Fatal("expected to find 'db'")
	}

	gotDB, ok := got.(*testDB)
	if !ok {
		t.Fatal("expected *testDB type")
	}

	if gotDB.Name != "test" {
		t.Errorf("got %q, want %q", gotDB.Name, "test")
	}
}

func TestContainer_Has(t *testing.T) {
	c := New()
	c.Provide("exists", "value")

	if !c.Has("exists") {
		t.Error("Has should return true for 'exists'")
	}

	if c.Has("missing") {
		t.Error("Has should return false for 'missing'")
	}
}

func TestContainer_Inject(t *testing.T) {
	c := New()
	db := &testDB{Name: "injected"}
	c.Provide("db", db)
	c.Provide("logger", "stdout")

	svc := &testService{}
	c.Inject(svc)

	if svc.DB != db {
		t.Errorf("DB not injected correctly")
	}

	if svc.Logger != "stdout" {
		t.Errorf("Logger: got %q, want %q", svc.Logger, "stdout")
	}
}

func TestContainer_Clone(t *testing.T) {
	c := New()
	c.Provide("key", "value")

	clone := c.Clone()
	clone.Provide("new", "added")

	if !clone.Has("key") {
		t.Error("clone should have 'key'")
	}

	if !clone.Has("new") {
		t.Error("clone should have 'new'")
	}

	if c.Has("new") {
		t.Error("original should not have 'new'")
	}
}

func TestContainer_Keys(t *testing.T) {
	c := New()
	c.Provide("a", 1)
	c.Provide("b", 2)

	keys := c.Keys()
	if len(keys) != 2 {
		t.Errorf("got %d keys, want 2", len(keys))
	}
}

func TestContainer_MustGet_Panic(t *testing.T) {
	c := New()

	defer func() {
		if r := recover(); r == nil {
			t.Error("MustGet should panic for missing key")
		}
	}()

	c.MustGet("missing")
}

func TestResolve(t *testing.T) {
	c := New()
	c.Provide("num", 42)

	got, ok := Resolve[int](c, "num")
	if !ok {
		t.Fatal("expected to resolve 'num'")
	}
	if got != 42 {
		t.Errorf("got %d, want 42", got)
	}
}

func TestResolve_TypeMismatch(t *testing.T) {
	c := New()
	c.Provide("num", 42)

	_, ok := Resolve[string](c, "num")
	if ok {
		t.Error("should return false for type mismatch")
	}
}

func TestMustResolve_Panic(t *testing.T) {
	c := New()

	defer func() {
		if r := recover(); r == nil {
			t.Error("MustResolve should panic for missing key")
		}
	}()

	MustResolve[int](c, "missing")
}

func TestBuilder_RegisterAndResolveType(t *testing.T) {
	b := NewBuilder()
	Register(b, func() *testDB { return &testDB{Name: "built"} })

	c := b.MustBuild()
	db := ResolveType[*testDB](c)
	if db.Name != "built" {
		t.Errorf("got %q, want %q", db.Name, "built")
	}
}

func TestBuilder_TransientLifetime(t *testing.T) {
	b := NewBuilder()
	Register(b, func() *testDB { return &testDB{Name: "fresh"} }, Transient)

	c := b.MustBuild()
	a := ResolveType[*testDB](c)
	b2 := ResolveType[*testDB](c)
	if a == b2 {
		t.Error("Transient should return new instances")
	}
}

func TestBuilder_SingletonLifetime(t *testing.T) {
	b := NewBuilder()
	Register(b, func() *testDB { return &testDB{Name: "singleton"} }, Singleton)

	c := b.MustBuild()
	a := ResolveType[*testDB](c)
	b2 := ResolveType[*testDB](c)
	if a != b2 {
		t.Error("Singleton should return same instance")
	}
}

func TestResolveType_PanicOnMissing(t *testing.T) {
	b := NewBuilder()
	c := b.MustBuild()

	defer func() {
		if r := recover(); r == nil {
			t.Error("ResolveType should panic on missing type")
		}
	}()

	ResolveType[*testDB](c)
}

func TestBuilder_ProvideNamed(t *testing.T) {
	b := NewBuilder()
	b.Provide("db", &testDB{Name: "named"})

	c := b.MustBuild()
	got, ok := c.Get("db")
	if !ok {
		t.Fatal("expected named dep 'db'")
	}
	db := got.(*testDB)
	if db.Name != "named" {
		t.Errorf("got %q, want %q", db.Name, "named")
	}
}

type Worker interface {
	Work() string
}

type GoodWorker struct {
	contracts.Implements[Worker]
}

func (g *GoodWorker) Work() string {
	return "working hard"
}

type LazyWorker struct {
	contracts.Implements[Worker]
}

func (l *LazyWorker) Work() string {
	return "working smart"
}

type BrokenWorker struct {
	contracts.Implements[Worker]
}

func TestContainer_ProvideWithContracts(t *testing.T) {
	c := New()

	t.Run("Valid implementation", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Provide should not panic for valid worker: %v", r)
			}
		}()
		c.Provide("good", &GoodWorker{})
	})

	t.Run("Invalid implementation panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("Provide should panic for broken worker")
			}
		}()
		c.Provide("broken", &BrokenWorker{})
	})
}

func TestResolveAll(t *testing.T) {
	c := New()
	c.Provide("good", &GoodWorker{})
	c.Provide("lazy", &LazyWorker{})
	c.Provide("other", "not a worker")

	workers := ResolveAll[Worker](c)
	if len(workers) != 2 {
		t.Fatalf("expected 2 workers, got %d", len(workers))
	}

	foundGood := false
	foundLazy := false
	for _, w := range workers {
		switch w.Work() {
		case "working hard":
			foundGood = true
		case "working smart":
			foundLazy = true
		}
	}

	if !foundGood || !foundLazy {
		t.Error("ResolveAll did not find all expected workers")
	}
}

func TestContainer_Scope(t *testing.T) {
	c := New()
	c.Provide("shared", "value")

	child := c.Scope()
	if !child.Has("shared") {
		t.Error("child should inherit named deps from parent")
	}
}

func TestContainer_ProvideLazy(t *testing.T) {
	c := New()
	called := 0
	c.ProvideLazy("lazy", func() any {
		called++
		return "computed"
	})

	if called != 0 {
		t.Error("lazy factory should not be called on registration")
	}

	v, ok := c.Get("lazy")
	if !ok {
		t.Fatal("expected to find 'lazy'")
	}
	if v != "computed" {
		t.Errorf("got %v, want %q", v, "computed")
	}
	if called != 1 {
		t.Error("lazy factory should be called once on first access")
	}

	v2, _ := c.Get("lazy")
	if v2 != "computed" {
		t.Error("lazy factory should return cached value")
	}
	if called != 1 {
		t.Error("lazy factory should only be called once")
	}
}

// --- M1: RegisterFromFunc tests ---

type Config struct {
	DSN string
}

type UserService struct {
	DB  *testDB
	Cfg *Config
}

func NewUserService(db *testDB, cfg *Config) UserService {
	return UserService{DB: db, Cfg: cfg}
}

func TestRegisterFromFunc(t *testing.T) {
	b := NewBuilder()
	Register(b, func() *testDB { return &testDB{Name: "pg"} })
	Register(b, func() *Config { return &Config{DSN: "host=localhost"} })
	RegisterFromFunc[UserService](b, NewUserService, Scoped)

	c := b.MustBuild()
	svc := ResolveType[UserService](c)
	if svc.DB.Name != "pg" {
		t.Errorf("DB.Name = %q, want %q", svc.DB.Name, "pg")
	}
	if svc.Cfg.DSN != "host=localhost" {
		t.Errorf("Cfg.DSN = %q, want %q", svc.Cfg.DSN, "host=localhost")
	}
}

func TestRegisterFromFunc_MissingDep(t *testing.T) {
	b := NewBuilder()
	Register(b, func() *testDB { return &testDB{Name: "pg"} })
	RegisterFromFunc[UserService](b, NewUserService, Scoped)

	_, err := b.Build()
	if err == nil {
		t.Fatal("expected build error for missing Config dependency")
	}
}

// --- M1: Scoped disposal tests ---

type closableService struct {
	closed bool
}

func (c *closableService) Close() error {
	c.closed = true
	return nil
}

func TestScopedContainer_Close(t *testing.T) {
	b := NewBuilder()
	Register(b, func() *closableService { return &closableService{} }, Scoped)

	c := b.MustBuild()
	scope := c.Scope()
	svc := ResolveType[*closableService](scope)
	if svc.closed {
		t.Error("service should not be closed yet")
	}
	if err := scope.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !svc.closed {
		t.Error("service should be closed after scope.Close()")
	}
}

func TestContainer_Close_Singleton(t *testing.T) {
	b := NewBuilder()
	Register(b, func() *closableService { return &closableService{} }, Singleton)

	c := b.MustBuild()
	svc := ResolveType[*closableService](c)
	if svc.closed {
		t.Error("service should not be closed yet")
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !svc.closed {
		t.Error("singleton implementing io.Closer should be closed on container close")
	}
}

func TestContainer_Close_NonCloser(t *testing.T) {
	b := NewBuilder()
	Register(b, func() *testDB { return &testDB{Name: "ok"} })

	c := b.MustBuild()
	ResolveType[*testDB](c)
	if err := c.Close(); err != nil {
		t.Fatalf("Close on non-closer should not error: %v", err)
	}
}

var _ io.Closer = (*closableService)(nil)
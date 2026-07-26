//foundation:ignore-file

package di

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mirkobrombin/go-foundation/v2/core/contracts"
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
	if err := c.Inject(svc); err != nil {
		t.Fatalf("Inject() error = %v", err)
	}

	if svc.DB != db {
		t.Errorf("DB not injected correctly")
	}

	if svc.Logger != "stdout" {
		t.Errorf("Logger: got %q, want %q", svc.Logger, "stdout")
	}
}

func TestContainer_InjectRejectsMissingDependency(t *testing.T) {
	c := New()
	c.Provide("db", &testDB{})

	err := c.Inject(&testService{})
	if err == nil {
		t.Fatal("Inject() accepted a missing dependency")
	}
}

func TestContainer_InjectRejectsWrongType(t *testing.T) {
	c := New()
	c.Provide("db", &testDB{})
	c.Provide("logger", 42)

	err := c.Inject(&testService{})
	if err == nil {
		t.Fatal("Inject() accepted a dependency with the wrong type")
	}
}

func TestContainer_InjectRejectsUnexportedField(t *testing.T) {
	target := struct {
		value string `inject:"value"`
	}{}
	container := NewBuilder()
	container.Provide("value", "ok")

	if err := container.MustBuild().Inject(&target); err == nil {
		t.Fatal("Inject() accepted an unexported field")
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

func TestBuilder_RegisterAs(t *testing.T) {
	b := NewBuilder()
	RegisterAs[Worker](b, func() *GoodWorker { return &GoodWorker{} })

	c := b.MustBuild()
	if got := ResolveType[Worker](c).Work(); got != "working hard" {
		t.Fatalf("ResolveType[Worker]() = %q", got)
	}
}

func TestBuilder_RegisterImplRejectsInvalidContract(t *testing.T) {
	b := NewBuilder()
	RegisterImpl[Worker, *BrokenWorker](b)

	if _, err := b.Build(); err == nil {
		t.Fatal("Build() accepted an invalid implementation")
	}
}

func TestBuilder_RegisterImplIgnoresContractMarkerForInjection(t *testing.T) {
	b := NewBuilder()
	RegisterImpl[Worker, *GoodWorker](b)

	worker, err := TryResolveType[Worker](b.MustBuild())
	if err != nil {
		t.Fatal(err)
	}
	if worker.Work() != "working hard" {
		t.Fatalf("Work() = %q", worker.Work())
	}
}

type configuredWorker struct {
	Config *Config `inject:"config"`
}

func (w *configuredWorker) Work() string {
	return w.Config.DSN
}

func TestBuilder_RegisterImplValidatesPointerFields(t *testing.T) {
	b := NewBuilder()
	RegisterImpl[Worker, *configuredWorker](b)

	if _, err := b.Build(); err == nil {
		t.Fatal("Build() accepted a pointer implementation with a missing dependency")
	}
}

type hiddenDependencyWorker struct {
	value string `inject:"value"`
}

func (w *hiddenDependencyWorker) Work() string {
	return w.value
}

func TestBuilder_RegisterImplRejectsUnexportedInjectionField(t *testing.T) {
	b := NewBuilder()
	Register(b, func() string { return "configured" })
	RegisterImpl[Worker, *hiddenDependencyWorker](b)

	if _, err := b.Build(); err == nil {
		t.Fatal("Build() accepted an unexported injection field")
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

func TestBuilder_ConcurrentSingletonResolution(t *testing.T) {
	b := NewBuilder()
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	Register(b, func() *testDB {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return &testDB{Name: "singleton"}
	})

	container := b.MustBuild()
	results := make(chan *testDB, 2)
	go func() {
		results <- ResolveType[*testDB](container)
	}()
	<-started
	go func() {
		results <- ResolveType[*testDB](container)
	}()
	close(release)

	first := <-results
	second := <-results
	if first != second {
		t.Fatal("concurrent singleton resolutions returned different instances")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("singleton factory called %d times", got)
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

func TestBuilder_RejectsDuplicateNamedDependency(t *testing.T) {
	b := NewBuilder()
	b.Provide("db", &testDB{})
	b.Provide("db", &testDB{})

	if _, err := b.Build(); err == nil {
		t.Fatal("Build() accepted a duplicate named dependency")
	}
}

func TestBuilder_RejectsNilInstances(t *testing.T) {
	b := NewBuilder()
	var database *testDB
	RegisterInstance(b, database)
	b.Provide("database", database)

	if _, err := b.Build(); err == nil {
		t.Fatal("Build() accepted nil instances")
	}
}

func TestBuilder_RejectsNilFactories(t *testing.T) {
	b := NewBuilder()
	var factory func() *testDB
	Register(b, factory)

	if _, err := b.Build(); err == nil {
		t.Fatal("Build() accepted a nil factory")
	}
}

func TestBuilder_RejectsNilFactoryResults(t *testing.T) {
	t.Run("register", func(t *testing.T) {
		b := NewBuilder()
		Register(b, func() *testDB { return nil })
		if _, err := TryResolveType[*testDB](b.MustBuild()); err == nil {
			t.Fatal("TryResolveType() accepted a nil factory result")
		}
	})

	t.Run("register as", func(t *testing.T) {
		b := NewBuilder()
		RegisterAs[Worker](b, func() *GoodWorker { return nil })
		if _, err := TryResolveType[Worker](b.MustBuild()); err == nil {
			t.Fatal("TryResolveType() accepted a nil implementation")
		}
	})

	t.Run("constructor", func(t *testing.T) {
		b := NewBuilder()
		RegisterFromFunc[*testDB](b, func() *testDB { return nil })
		if _, err := TryResolveType[*testDB](b.MustBuild()); err == nil {
			t.Fatal("TryResolveType() accepted a nil constructor result")
		}
	})
}

type valueError struct{}

func (valueError) Error() string {
	return "value error"
}

func TestRegisterFromFuncRejectsConcreteErrorResult(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("RegisterFromFunc() accepted a concrete error result")
		}
	}()
	RegisterFromFunc[*testDB](NewBuilder(), func() (*testDB, valueError) {
		return &testDB{}, valueError{}
	})
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

func TestContainer_ScopeDoesNotOwnSingleton(t *testing.T) {
	b := NewBuilder()
	Register(b, func() *closableService { return &closableService{} }, Singleton)

	container := b.MustBuild()
	scope := container.Scope()
	service := ResolveType[*closableService](scope)
	if err := scope.Close(); err != nil {
		t.Fatalf("scope.Close(): %v", err)
	}
	if service.closed {
		t.Fatal("scope closed a singleton owned by the root container")
	}
	if err := container.Close(); err != nil {
		t.Fatalf("container.Close(): %v", err)
	}
	if !service.closed {
		t.Fatal("root container did not close its singleton")
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

func TestContainer_ResolveAllAllowsReentrantLazyFactory(t *testing.T) {
	c := New()
	c.ProvideLazy("worker", func() any {
		c.Provide("side-effect", "registered")
		return &GoodWorker{}
	})

	done := make(chan []Worker, 1)
	go func() {
		done <- ResolveAll[Worker](c)
	}()

	select {
	case workers := <-done:
		if len(workers) != 1 {
			t.Fatalf("ResolveAll() returned %d workers", len(workers))
		}
	case <-time.After(time.Second):
		t.Fatal("ResolveAll() deadlocked in a reentrant lazy factory")
	}
}

func TestContainer_ProvideLazyRejectsNilFactory(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("ProvideLazy() accepted a nil factory")
		}
	}()
	New().ProvideLazy("nil", nil)
}

func TestContainer_ProvideLazyRejectsNilResult(t *testing.T) {
	container := New()
	container.ProvideLazy("nil", func() any {
		return (*testDB)(nil)
	})
	if value, ok := container.Get("nil"); ok || value != nil {
		t.Fatalf("Get() = (%v, %v), want (nil, false)", value, ok)
	}
}

func TestContainer_NamedCloserIsClosed(t *testing.T) {
	service := &closableService{}
	builder := NewBuilder()
	builder.Provide("service", service)
	container := builder.MustBuild()

	if err := container.Close(); err != nil {
		t.Fatal(err)
	}
	if !service.closed {
		t.Fatal("Close() did not close a named dependency")
	}
}

func TestContainer_LazyCloserBelongsToRoot(t *testing.T) {
	service := &closableService{}
	container := New()
	container.ProvideLazy("service", func() any { return service })
	scope := container.Scope()

	if _, ok := scope.Get("service"); !ok {
		t.Fatal("scope did not resolve the lazy dependency")
	}
	if err := scope.Close(); err != nil {
		t.Fatal(err)
	}
	if service.closed {
		t.Fatal("scope closed a lazy dependency owned by the root")
	}
	if err := container.Close(); err != nil {
		t.Fatal(err)
	}
	if !service.closed {
		t.Fatal("root did not close its lazy dependency")
	}
}

func TestContainerRejectsUseAfterClose(t *testing.T) {
	container := New()
	container.Provide("value", 1)
	if err := container.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := container.Get("value"); ok {
		t.Fatal("Get() returned a value after Close()")
	}
	if _, err := TryResolveType[*testDB](container); !errors.Is(err, ErrContainerClosed) {
		t.Fatalf("TryResolveType() error = %v, want ErrContainerClosed", err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("Provide() succeeded after Close()")
		}
	}()
	container.Provide("late", 2)
}

func TestResolveAllRejectsUseAfterClose(t *testing.T) {
	container := New()
	var called atomic.Bool
	container.ProvideLazy("worker", func() any {
		called.Store(true)
		return &GoodWorker{}
	})
	if err := container.Close(); err != nil {
		t.Fatal(err)
	}

	if workers := ResolveAll[Worker](container); len(workers) != 0 {
		t.Fatalf("ResolveAll() returned %d workers after Close()", len(workers))
	}
	if called.Load() {
		t.Fatal("ResolveAll() realized a lazy dependency after Close()")
	}
}

func TestContainerProvideLinearizesWithClose(t *testing.T) {
	for iteration := 0; iteration < 100; iteration++ {
		container := New()
		service := &closableService{}
		start := make(chan struct{})
		var wg sync.WaitGroup
		var provided atomic.Bool
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_ = container.Close()
		}()
		go func() {
			defer wg.Done()
			<-start
			defer func() {
				_ = recover()
			}()
			container.Provide("service", service)
			provided.Store(true)
		}()
		close(start)
		wg.Wait()

		if container.Has("service") {
			t.Fatal("closed container reported a registered service")
		}
		if provided.Load() && !service.closed {
			t.Fatal("service admitted before Close() was not closed")
		}
	}
}

func TestContainerClosesLazyResourceRacingClose(t *testing.T) {
	container := New()
	started := make(chan struct{})
	release := make(chan struct{})
	service := &closableService{}
	container.ProvideLazy("service", func() any {
		close(started)
		<-release
		return service
	})
	resolved := make(chan bool, 1)
	go func() {
		_, ok := container.Get("service")
		resolved <- ok
	}()
	<-started
	if err := container.Close(); err != nil {
		t.Fatal(err)
	}
	close(release)
	if <-resolved {
		t.Fatal("Get() succeeded after concurrent Close()")
	}
	if !service.closed {
		t.Fatal("late lazy resource was not closed")
	}
}

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

func NewFailingUserService(db *testDB, cfg *Config) (UserService, error) {
	return UserService{}, errors.New("constructor failed")
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

func TestRegisterFromFunc_ReturnsConstructorError(t *testing.T) {
	b := NewBuilder()
	Register(b, func() *testDB { return &testDB{Name: "pg"} })
	Register(b, func() *Config { return &Config{DSN: "host=localhost"} })
	RegisterFromFunc[UserService](b, NewFailingUserService)

	c := b.MustBuild()
	if _, err := TryResolveType[UserService](c); err == nil {
		t.Fatal("TryResolveType() ignored the constructor error")
	}
}

func TestRegisterFromFunc_ZeroArgumentConstructor(t *testing.T) {
	builder := NewBuilder()
	RegisterFromFunc[*testDB](builder, func() *testDB {
		return &testDB{Name: "zero"}
	})
	if got := ResolveType[*testDB](builder.MustBuild()).Name; got != "zero" {
		t.Fatalf("resolved name = %q, want zero", got)
	}
}

type failingInitializer interface {
	Ready()
}

type initFailure struct{}

func (*initFailure) Ready() {}

func (*initFailure) Init() error {
	return errors.New("init failed")
}

func TestRegisterImpl_PropagatesInitError(t *testing.T) {
	builder := NewBuilder()
	RegisterImpl[failingInitializer, *initFailure](builder)
	container := builder.MustBuild()
	if _, err := TryResolveType[failingInitializer](container); err == nil {
		t.Fatal("TryResolveType() ignored Init error")
	}
}

type cyclicA struct{}
type cyclicB struct{}

func newCyclicA(*cyclicB) *cyclicA {
	return &cyclicA{}
}

func newCyclicB(*cyclicA) *cyclicB {
	return &cyclicB{}
}

func TestRegisterFromFunc_RejectsCircularDependency(t *testing.T) {
	b := NewBuilder()
	RegisterFromFunc[*cyclicA](b, newCyclicA)
	RegisterFromFunc[*cyclicB](b, newCyclicB)

	if _, err := b.Build(); err == nil {
		t.Fatal("Build() accepted a circular constructor dependency")
	}
}

type serviceWithDB struct {
	DB *testDB
}

func TestRegisterFromFunc_UsesRegisteredSingleton(t *testing.T) {
	b := NewBuilder()
	created := 0
	Register(b, func() *testDB {
		created++
		return &testDB{Name: "singleton"}
	})
	RegisterFromFunc[*serviceWithDB](b, func(db *testDB) *serviceWithDB {
		return &serviceWithDB{DB: db}
	})

	container := b.MustBuild()
	db := ResolveType[*testDB](container)
	service := ResolveType[*serviceWithDB](container)
	if service.DB != db {
		t.Fatal("constructor dependency did not use the registered singleton")
	}
	if created != 1 {
		t.Fatalf("singleton factory called %d times", created)
	}
}

type closableService struct {
	closed bool
	order  *[]string
	name   string
}

func (c *closableService) Close() error {
	c.closed = true
	if c.order != nil {
		*c.order = append(*c.order, c.name)
	}
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

func TestContainer_CloseRegisteredInstance(t *testing.T) {
	service := &closableService{}
	b := NewBuilder()
	RegisterInstance(b, service)
	container := b.MustBuild()

	if err := container.Close(); err != nil {
		t.Fatal(err)
	}
	if !service.closed {
		t.Fatal("Close() did not close a registered instance")
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

type dependentCloser struct {
	service *closableService
	order   *[]string
}

func (c *dependentCloser) Close() error {
	*c.order = append(*c.order, "dependent")
	return nil
}

func TestContainer_CloseUsesReverseCreationOrder(t *testing.T) {
	var order []string
	b := NewBuilder()
	Register(b, func() *closableService {
		return &closableService{name: "dependency", order: &order}
	})
	RegisterFromFunc[*dependentCloser](b, func(service *closableService) *dependentCloser {
		return &dependentCloser{service: service, order: &order}
	})

	container := b.MustBuild()
	ResolveType[*dependentCloser](container)
	if err := container.Close(); err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "dependent" || order[1] != "dependency" {
		t.Fatalf("close order = %v", order)
	}
}

var _ io.Closer = (*closableService)(nil)

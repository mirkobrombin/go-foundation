package mcp

import (
	"fmt"
	"sort"
	"strings"
)

// Rule is one declaration topic: the exact grammar Foundation understands, the
// mistakes that grammar invites, and code that compiles.
type Rule struct {
	Topic       string   `json:"topic"`
	Summary     string   `json:"summary"`
	Grammar     []string `json:"grammar"`
	Constraints []string `json:"constraints"`
	Mistakes    []string `json:"common_mistakes"`
	Example     string   `json:"example"`
	SeeAlso     []string `json:"see_also,omitempty"`
}

// rules is the declaration reference. Every statement here is taken from the
// runtime and analyzer sources, not from prose, because prose drifts.
var rules = []Rule{
	{
		Topic:   "http_handler",
		Summary: "An HTTP handler is a struct that declares its route in tags and implements Handle. Foundation builds a fresh instance per request, binds the request into its fields, then calls Handle.",
		Grammar: []string{
			`_ struct{} ` + "`" + `method:"GET" path:"/users/{id:int}"` + "`" + ` declares the route on a blank field.`,
			`method accepts any HTTP method, uppercase: GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS.`,
			`path segments are literal, parameter {name}, constrained parameter {name:int}, or catch-all {*rest}.`,
			`Constraints: int, alpha, regex(PATTERN). Several can be comma separated, regex(...) must stand alone.`,
			`A catch-all must be the last segment and cannot carry a constraint.`,
			`Field binding tags: path:"id", query:"page", header:"X-Request-Id", form:"name", body:"json", default:"10".`,
			`Dependency tag: inject:"name", on an exported field.`,
			`Handle must be a method on the pointer receiver: func (h *GetUser) Handle(ctx context.Context) (any, error).`,
		},
		Constraints: []string{
			"Every route parameter needs a field bound with path:\"name\", and every path field needs a parameter with that name. The analyzer reports both directions.",
			"Injected fields must be exported. An unexported field cannot be set and the analyzer reports it.",
			"Only fields with inject:\"name\" are injected. An untagged field is never filled, which is a deliberate change from v1.",
			"JSON binding rejects unknown fields and bodies larger than 1 MiB, and answers with a client error status instead of 500.",
			"Two handlers on the same method and path fail at registration, they do not silently override.",
		},
		Mistakes: []string{
			"Writing the route on a named field instead of the blank _ struct{} field.",
			"Declaring {id:int} and then binding the field with query:\"id\" instead of path:\"id\".",
			"Expecting an untagged dependency field to be injected because v1 did that.",
			"Ignoring the error returned by registration, which the analyzer reports as an ignored binding error.",
		},
		Example: `type GetUser struct {
	_     struct{}  ` + "`" + `method:"GET" path:"/users/{id:int}"` + "`" + `
	ID    int       ` + "`" + `path:"id"` + "`" + `
	Page  int       ` + "`" + `query:"page" default:"1"` + "`" + `
	Users UserStore ` + "`" + `inject:"users"` + "`" + `
}

func (h *GetUser) Handle(ctx context.Context) (any, error) {
	user, ok := h.Users.Find(h.ID)
	if !ok {
		return nil, errutil.NotFound("user not found")
	}
	return user, nil
}`,
		SeeAlso: []string{"dependency_injection", "binding", "errors"},
	},
	{
		Topic:   "action",
		Summary: "An action is a named command with an optional key binding, dispatched by name or through a typed handle.",
		Grammar: []string{
			`_ struct{} ` + "`" + `action:"users.create" keys:"ctrl+n"` + "`" + ` declares the action.`,
			`action is the dispatch name and cannot be empty.`,
			`keys is optional and holds the key binding shown to a user interface.`,
			`Payload fields are bound from the dispatch payload and reject unknown fields.`,
			`Handle has the same shape as an HTTP handler: func (a *CreateUser) Handle(ctx context.Context) (any, error).`,
		},
		Constraints: []string{
			"An action name must be unique in the package. A duplicate is reported by the analyzer and rejected at registration.",
			"Dispatching a name that no package declares is reported by the analyzer when the name is a literal.",
			"Typed actions carry the payload and result types at the call site, which removes the name from Go code entirely.",
		},
		Mistakes: []string{
			"Registering the same action name twice in different files of the same package.",
			"Dispatching a name built by string concatenation, which the analyzer cannot verify. Use a typed action instead.",
		},
		Example: `type CreateUser struct {
	_    struct{} ` + "`" + `action:"users.create" keys:"ctrl+n"` + "`" + `
	Name string   ` + "`" + `json:"name"` + "`" + `
}

func (a *CreateUser) Handle(ctx context.Context) (any, error) {
	return User{Name: a.Name}, nil
}

// Typed dispatch, no string at the call site.
create := actions.NewTyped[CreateUser, User]("users.create")
result, err := actions.DispatchTyped(ctx, router, create, CreateUser{Name: "ada"})`,
		SeeAlso: []string{"typed_apis", "generation"},
	},
	{
		Topic:   "dependency_injection",
		Summary: "Dependencies are provided by name and injected into exported fields tagged with inject. Everything about the graph is decided when the application is built, not when a request arrives.",
		Grammar: []string{
			`application.Provide("users", store) registers a value under a name.`,
			`Users UserStore ` + "`" + `inject:"users"` + "`" + ` asks for it.`,
			`di.NewKey[UserStore]("users") creates a typed key, used with di.ProvideKey and di.ResolveKey.`,
			`di.RegisterFromFunc registers a constructor that returns a value and optionally an error.`,
		},
		Constraints: []string{
			"Container.Inject returns an error. Handle it: the analyzer reports it when it is dropped.",
			"A missing name, a wrong type, a duplicate provider, or a failing constructor stops App.Build. Nothing is deferred to the first request.",
			"A constructor must return one value and optionally an error, in that order.",
			"Injection is by name, and only tagged exported fields take part.",
		},
		Mistakes: []string{
			"Providing under one name and injecting another, usually a typo. The analyzer catches it inside a package.",
			"Assuming cross package wiring is verified statically. It is not: App.Build is the boundary for that.",
			"Calling Inject and ignoring the returned error.",
		},
		Example: `application := app.New().Provide("users", NewMemoryUserStore())
if _, err := application.Build(); err != nil {
	return err
}

// Typed alternative, when the relationship can stay in Go.
var usersKey = di.NewKey[UserStore]("users")
di.ProvideKey(builder, usersKey, store)
users, ok := di.ResolveKey(container, usersKey)`,
		SeeAlso: []string{"contracts", "typed_apis"},
	},
	{
		Topic:   "contracts",
		Summary: "A contract records that a type implements an interface, in a form the compiler can check once the registry is generated.",
		Grammar: []string{
			`contracts.Implements[UserStore] embedded in a struct declares the relationship.`,
			`contracts.Assert[UserStore]((*MemoryUserStore)(nil)) states it explicitly, without embedding.`,
			`foundation generate turns both into var _ UserStore = (*MemoryUserStore)(nil).`,
		},
		Constraints: []string{
			"The type argument must be an interface. The analyzer reports anything else.",
			"The compiler is the authority once the assertion is generated. The analyzer is the earlier, weaker signal.",
			"Use Assert for framework internals and manually wired types, Implements for application types that should also appear in the generated registry.",
			"contracts.Verify remains for dynamic plugin boundaries. Do not make it the primary check.",
		},
		Mistakes: []string{
			"Embedding contracts.Implements with a struct type as the argument.",
			"Believing the marker verifies anything by itself before generation. Run foundation generate, or write the assertion.",
		},
		Example: `type MemoryUserStore struct {
	contracts.Implements[UserStore]

	mu    sync.RWMutex
	users map[int]User
}

// Equivalent explicit form.
var _ = contracts.Assert[UserStore]((*MemoryUserStore)(nil))`,
		SeeAlso: []string{"generation", "http_handler"},
	},
	{
		Topic:   "generation",
		Summary: "foundation generate writes one zz_foundation.gen.go per package that declares something, and removes it when the declarations are gone.",
		Grammar: []string{
			`foundation generate ./... writes the registries.`,
			`foundation generate -check ./... fails when a committed file is stale or orphaned.`,
			`The file exposes FoundationHTTPHandlers, FoundationActions, and RegisterFoundation(application *app.App).`,
		},
		Constraints: []string{
			"Commit the generated files. They are what makes a broken contract a compile error on a machine without the CLI.",
			"Generation is optional: RegisterHTTP and RegisterActionHandler still register by reflection, and the analyzer reports the same problems either way.",
			"Output is deterministic, sorted by package path and declaration order, so it does not churn diffs.",
			"Do not edit the generated file. Regenerate it.",
		},
		Mistakes: []string{
			"Adding a handler and forgetting to regenerate, which -check turns into a failed build instead of a missing route at runtime.",
			"Editing zz_foundation.gen.go by hand.",
			"Adding the generated file to .gitignore, which defeats its purpose.",
		},
		Example: `foundation generate ./...
foundation check ./...
go test ./...

# in an application
application := app.New().Provide("users", store)
RegisterFoundation(application)
if _, err := application.Build(); err != nil {
	return err
}`,
		SeeAlso: []string{"contracts", "workflow"},
	},
	{
		Topic:   "binding",
		Summary: "Request and payload binding fills handler fields from the transport, with limits that make a malformed request the client's problem.",
		Grammar: []string{
			`path:"id" binds a route parameter.`,
			`query:"page" binds a query string value.`,
			`header:"X-Request-Id" binds a header.`,
			`form:"name" binds a form value.`,
			`body:"json" binds the JSON request body into the field.`,
			`default:"10" supplies a value when the source is absent.`,
		},
		Constraints: []string{
			"JSON binding rejects unknown fields and malformed trailing content.",
			"Request bodies larger than 1 MiB are rejected.",
			"A binding failure produces a client error status, not 500.",
			"Action payload binding also rejects unknown fields.",
		},
		Mistakes: []string{
			"Sending extra JSON fields and expecting them to be ignored.",
			"Treating a binding error as an internal error in handler code.",
		},
		Example: `type CreateUser struct {
	_    struct{} ` + "`" + `method:"POST" path:"/users"` + "`" + `
	Body struct {
		Name string ` + "`" + `json:"name"` + "`" + `
	} ` + "`" + `body:"json"` + "`" + `
	Trace string ` + "`" + `header:"X-Request-Id"` + "`" + `
}`,
		SeeAlso: []string{"http_handler", "errors"},
	},
	{
		Topic:   "errors",
		Summary: "Registration and wiring return errors, and handler errors carry their HTTP meaning through errutil.",
		Grammar: []string{
			`errutil.NotFound, errutil.BadRequest and friends produce errors that map to a status.`,
			`RegisterDefinition and RegisterDefinitions return an error.`,
			`Container.Inject returns an error.`,
			`App.Build returns the application and an error.`,
		},
		Constraints: []string{
			"Do not panic in application code to signal an HTTP outcome. Return the error.",
			"Do not discard registration errors. The analyzer reports discarded binding errors.",
		},
		Mistakes: []string{
			"Returning a bare fmt.Errorf from a handler and expecting a 404.",
			"Ignoring the error from Build because it 'never fails' during development.",
		},
		Example: `if err := server.RegisterDefinition(definition, container); err != nil {
	return err
}

func (h *GetUser) Handle(ctx context.Context) (any, error) {
	user, ok := h.Users.Find(h.ID)
	if !ok {
		return nil, errutil.NotFound("user not found")
	}
	return user, nil
}`,
		SeeAlso: []string{"http_handler", "dependency_injection"},
	},
	{
		Topic:   "typed_apis",
		Summary: "Where a relationship can live in the type system, Foundation offers a typed form so the compiler carries it instead of a string.",
		Grammar: []string{
			`di.NewKey[T](name), di.ProvideKey, di.ProvideLazyKey, di.ResolveKey, di.MustResolveKey.`,
			`actions.NewTyped[In, Out](name), actions.HandleTyped, actions.DispatchTyped.`,
			`web.DefinitionFromHandler builds a static definition from a handler value.`,
		},
		Constraints: []string{
			"Keep strings where the name is external metadata: a route path, an action exposed to a user interface, a configuration key.",
			"Use the typed form inside Go code, where a rename should be a compile error.",
		},
		Mistakes: []string{
			"Wrapping everything in typed keys, including names that exist only because an external system uses them.",
		},
		Example: `var usersKey = di.NewKey[UserStore]("users")

builder := di.NewBuilder()
di.ProvideKey(builder, usersKey, store)
container, err := builder.Build()
users, ok := di.ResolveKey(container, usersKey)`,
		SeeAlso: []string{"dependency_injection", "action"},
	},
	{
		Topic:   "layering",
		Summary: "Foundation is organised in layers and the analyzer enforces the direction, in your code as much as in its own.",
		Grammar: []string{
			`core holds runtime building blocks with no application dependency.`,
			`app holds composition and boundaries: di, web, actions, dispatcher, hosting, testing.`,
			`dev is a separate module: analyzer, generator, CLI, MCP server.`,
		},
		Constraints: []string{
			"core cannot import app.",
			"Runtime packages cannot import dev.",
			"An application never imports dev either: the tools are a development dependency, not a runtime one.",
		},
		Mistakes: []string{
			"Importing the analyzer or generator from application code to 'reuse' its types.",
			"Reaching for an app package inside a core package to avoid passing a parameter.",
		},
		Example: `// allowed
import "github.com/mirkobrombin/go-foundation/v2/core/caching"

// rejected by foundation check inside a core package
import "github.com/mirkobrombin/go-foundation/v2/app/di"`,
		SeeAlso: []string{"workflow"},
	},
	{
		Topic:   "scheduler",
		Summary: "Scheduled work is registered with a five field cron expression and a handler.",
		Grammar: []string{
			`Five fields: minute hour day-of-month month day-of-week.`,
			`Registration takes a name, the expression, and a non nil handler.`,
		},
		Constraints: []string{
			"Literal expressions are validated by the analyzer: field count, field syntax, empty or duplicate names, nil handlers.",
			"An expression built at runtime is not verified statically.",
		},
		Mistakes: []string{
			"Using a six field expression copied from a system that includes seconds.",
			"Registering two jobs under the same name.",
		},
		Example: `scheduler.Register("nightly-report", "0 2 * * *", func(ctx context.Context) error {
	return report(ctx)
})`,
		SeeAlso: []string{"workflow"},
	},
	{
		Topic:   "workflow",
		Summary: "The order that keeps a Foundation change honest, from an empty directory to a verified result.",
		Grammar: []string{
			`1. Install the CLI: go install github.com/mirkobrombin/go-foundation/dev/v2/cmd/foundation@latest`,
			`2. Add the module: go get github.com/mirkobrombin/go-foundation/v2@latest`,
			`3. Declare contracts, handlers, actions, and providers in normal Go.`,
			`4. foundation generate ./...`,
			`5. foundation check ./...`,
			`6. go build ./... and go test ./...`,
			`7. go vet ./... and, for anything concurrent, go test -race ./...`,
		},
		Constraints: []string{
			"Never present Foundation code as finished without running check, build, and tests. The foundation_verify tool does all of it in one call.",
			"foundation check is not a linter you can skip: it reports wiring that compiles and still cannot work.",
			"Commit generated files together with the declarations that produced them.",
		},
		Mistakes: []string{
			"Writing handlers and never running generate, then wondering why RegisterFoundation is undefined.",
			"Running go build only, which cannot see a missing dependency name or a route parameter without a field.",
		},
		Example: `foundation generate ./...
foundation check ./...
go build ./...
go test ./...`,
		SeeAlso: []string{"generation", "layering"},
	},
	{
		Topic:   "migration_v1",
		Summary: "Moving an application from v1 to v2 is a wiring migration, not an import rewrite.",
		Grammar: []string{
			`Module path: github.com/mirkobrombin/go-foundation/v2, minimum Go 1.25.`,
			`pkg/app -> app, pkg/di -> app/di, pkg/actions -> app/actions, pkg/dispatcher -> app/dispatcher.`,
			`pkg/hosting -> app/hosting, pkg/srv -> app/web (package web), pkg/testutil -> app/testing (package apptest).`,
			`every other pkg/<name> -> core/<name>.`,
		},
		Constraints: []string{
			"Handle the errors that registration and injection now return.",
			"Add inject:\"name\" to every field that used to be injected implicitly.",
			"Expect stricter JSON binding: unknown fields and oversized bodies are rejected.",
			"Reflection based registration still exists, so the move can be done in one step and the static form adopted later.",
		},
		Mistakes: []string{
			"Search and replacing import paths and declaring the migration done.",
			"Leaving untagged fields and assuming they are still injected.",
		},
		Example: `// v1
import "github.com/mirkobrombin/go-foundation/pkg/srv"

// v2
import "github.com/mirkobrombin/go-foundation/v2/app/web"`,
		SeeAlso: []string{"workflow", "dependency_injection"},
	},
}

// Rules returns every declaration topic, sorted by name.
func Rules() []Rule {
	copied := make([]Rule, len(rules))
	copy(copied, rules)
	sort.Slice(copied, func(i, j int) bool { return copied[i].Topic < copied[j].Topic })
	return copied
}

// RuleByTopic returns one topic.
func RuleByTopic(topic string) (*Rule, error) {
	wanted := strings.ToLower(strings.TrimSpace(topic))
	for index := range rules {
		if rules[index].Topic == wanted {
			return &rules[index], nil
		}
	}
	return nil, fmt.Errorf("mcp: unknown topic %q, available: %s", topic, strings.Join(RuleTopics(), ", "))
}

// RuleTopics lists the available topics.
func RuleTopics() []string {
	topics := make([]string, 0, len(rules))
	for _, rule := range rules {
		topics = append(topics, rule.Topic)
	}
	sort.Strings(topics)
	return topics
}

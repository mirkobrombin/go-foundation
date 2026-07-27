package mcp

import (
	"sort"
	"strings"
)

// Check is one diagnostic foundation check can report, with what causes it and
// what fixes it. The catalog is complete: a test compares it against every
// report site in the analyzer, so a new diagnostic cannot ship undocumented.
type Check struct {
	Message  string `json:"message"`
	Category string `json:"category"`
	Cause    string `json:"cause"`
	Fix      string `json:"fix"`
}

var checks = []Check{
	{
		Message:  "%s cannot import %s",
		Category: "layering",
		Cause:    "A package imported across a forbidden layer boundary: core importing app, or runtime code importing the development module.",
		Fix:      "Move the shared type down into core, pass it as a parameter, or keep the dependency in the app layer where it belongs.",
	},
	{
		Message:  "contracts.Implements requires an interface type",
		Category: "contracts",
		Cause:    "The type argument of contracts.Implements is not an interface.",
		Fix:      "Pass the interface the type implements. If the target is a struct, there is no contract to declare.",
	},
	{
		Message:  "%s declares %s but does not implement it",
		Category: "contracts",
		Cause:    "A type embeds contracts.Implements for an interface it does not satisfy, usually after a method was renamed or its signature changed.",
		Fix:      "Implement the missing method, fix the signature, or drop the marker. Run foundation generate so the compiler enforces it from then on.",
	},
	{
		Message:  "%s does not implement %s",
		Category: "dependency_injection",
		Cause:    "A DI registration binds an implementation to an interface it does not satisfy.",
		Fix:      "Correct the implementation or the interface argument at the registration site.",
	},
	{
		Message:  "di.%s requires an interface as its first type argument",
		Category: "dependency_injection",
		Cause:    "A generic DI registration was instantiated with a concrete type where an interface is required.",
		Fix:      "Use the interface as the first type argument, and the implementation as the value.",
	},
	{
		Message:  "di.RegisterFromFunc constructor must be a function",
		Category: "dependency_injection",
		Cause:    "RegisterFromFunc was given a value instead of a constructor.",
		Fix:      "Pass a function. To register a ready value, use Provide instead.",
	},
	{
		Message:  "constructor must return a value and optional error",
		Category: "dependency_injection",
		Cause:    "A constructor returns nothing, or returns more than a value and an error.",
		Fix:      "Return exactly one value, optionally followed by an error.",
	},
	{
		Message:  "constructor second result must be error",
		Category: "dependency_injection",
		Cause:    "A constructor returns two values where the second is not an error.",
		Fix:      "Make the second result an error, or return a single value.",
	},
	{
		Message:  "constructor returns %s, want %s",
		Category: "dependency_injection",
		Cause:    "A constructor produces a type that does not match the registered one.",
		Fix:      "Align the constructor result with the registered type, or register the type the constructor actually returns.",
	},
	{
		Message:  "dependency %q is not provided in this package",
		Category: "dependency_injection",
		Cause:    "A field asks for a name that no Provide call in the package supplies. Usually a typo.",
		Fix:      "Fix the name, or provide it. If the value is supplied by another package, the analyzer cannot see it and App.Build is the check that matters.",
	},
	{
		Message:  "dependency %q has type %s, want %s",
		Category: "dependency_injection",
		Cause:    "A name is provided with one type and injected into a field of another.",
		Fix:      "Align the field type with the provided value, or provide the type the field expects.",
	},
	{
		Message:  "inject tag cannot be empty",
		Category: "dependency_injection",
		Cause:    "A field carries inject:\"\".",
		Fix:      "Name the dependency, or remove the tag if the field is not injected.",
	},
	{
		Message:  "injected field %s must be exported",
		Category: "dependency_injection",
		Cause:    "An unexported field is tagged for injection, and cannot be set.",
		Fix:      "Export the field.",
	},
	{
		Message:  "invalid struct tag: %v",
		Category: "declaration",
		Cause:    "A struct tag is not valid Go tag syntax, most often a missing quote or a stray space around the colon.",
		Fix:      "Write tags as key:\"value\", separated by single spaces.",
	},
	{
		Message:  "%s must declare method and path together",
		Category: "http",
		Cause:    "A handler declares only one of the two route tags.",
		Fix:      "Declare both on the same blank field: _ struct{} `method:\"GET\" path:\"/users\"`.",
	},
	{
		Message:  "unsupported HTTP method %q",
		Category: "http",
		Cause:    "The method tag is not a known HTTP method, or is not uppercase.",
		Fix:      "Use an uppercase method such as GET, POST, PUT, PATCH, DELETE, HEAD or OPTIONS.",
	},
	{
		Message:  "route path must start with /",
		Category: "http",
		Cause:    "A relative route path.",
		Fix:      "Start the path with a slash.",
	},
	{
		Message:  "invalid route path: %v",
		Category: "http",
		Cause:    "The path does not parse: an empty or duplicated parameter, a catch-all that is not last, a constraint on a catch-all, or an invalid regex constraint.",
		Fix:      "Use {name}, {name:int}, {name:alpha}, {name:regex(...)}, and put a catch-all {*rest} last without constraints.",
	},
	{
		Message:  "route parameter %q has no path field",
		Category: "http",
		Cause:    "The route declares a parameter that no field binds.",
		Fix:      "Add a field tagged path:\"name\", or remove the parameter from the route.",
	},
	{
		Message:  "path field %q is not present in route %q",
		Category: "http",
		Cause:    "A field binds a route parameter that the path does not declare.",
		Fix:      "Fix the field tag, or add the parameter to the path.",
	},
	{
		Message:  "%s must implement Handle(context.Context) (any, error)",
		Category: "http",
		Cause:    "A type declares a route or an action but has no Handle method with the expected signature.",
		Fix:      "Add func (h *T) Handle(ctx context.Context) (any, error). For a schema only type, mark it with // foundation:ignore handler.",
	},
	{
		Message:  "action tag cannot be empty",
		Category: "actions",
		Cause:    "A type declares action:\"\".",
		Fix:      "Give the action a name, or remove the tag.",
	},
	{
		Message:  "action %q is already declared",
		Category: "actions",
		Cause:    "Two types in the package declare the same action name.",
		Fix:      "Rename one of them. Names are unique within a package and duplicates are rejected at registration.",
	},
	{
		Message:  "action %q is not registered in this package",
		Category: "actions",
		Cause:    "A literal dispatch names an action that the package does not declare.",
		Fix:      "Fix the name, or declare the action. For a cross package dispatch, use a typed action so the relationship is carried by the compiler.",
	},
	{
		Message:  "binding error is ignored",
		Category: "errors",
		Cause:    "The error returned by a binding or registration call is discarded.",
		Fix:      "Handle the error. In v2 these calls report real wiring problems and must not be dropped.",
	},
	{
		Message:  "scheduled job name cannot be empty",
		Category: "scheduler",
		Cause:    "A literal scheduler registration passes an empty name.",
		Fix:      "Name the job. Names identify jobs in logs and must be unique.",
	},
	{
		Message:  "scheduled job handler cannot be nil",
		Category: "scheduler",
		Cause:    "A literal scheduler registration passes a nil handler.",
		Fix:      "Pass the function to run.",
	},
	{
		Message:  "invalid cron expression %q: expected 5 fields",
		Category: "scheduler",
		Cause:    "A literal cron expression has the wrong number of fields, often a six field expression that includes seconds.",
		Fix:      "Use five fields: minute hour day-of-month month day-of-week.",
	},
	{
		Message:  "invalid cron field %q",
		Category: "scheduler",
		Cause:    "A field of a literal cron expression is not valid.",
		Fix:      "Use a number, a range, a step, a list, or *.",
	},
	{
		Message:  "conflicting declaration",
		Category: "declaration",
		Cause:    "Secondary diagnostic pointing at the other declaration involved in a duplicate.",
		Fix:      "Read it together with the primary diagnostic: one of the two must change.",
	},
	{
		Message:  "first declaration",
		Category: "declaration",
		Cause:    "Secondary diagnostic marking the declaration that came first.",
		Fix:      "Informational. It tells you which one the analyzer kept.",
	},
	{
		Message:  "first registration",
		Category: "declaration",
		Cause:    "Secondary diagnostic marking the registration that came first.",
		Fix:      "Informational. It tells you where the duplicate collides.",
	},
}

// Checks returns the diagnostic catalog, sorted by category then message.
func Checks() []Check {
	copied := make([]Check, len(checks))
	copy(copied, checks)
	sort.Slice(copied, func(i, j int) bool {
		if copied[i].Category != copied[j].Category {
			return copied[i].Category < copied[j].Category
		}
		return copied[i].Message < copied[j].Message
	})
	return copied
}

// ChecksByCategory filters the catalog.
func ChecksByCategory(category string) []Check {
	wanted := strings.ToLower(strings.TrimSpace(category))
	if wanted == "" {
		return Checks()
	}
	var filtered []Check
	for _, check := range Checks() {
		if check.Category == wanted {
			filtered = append(filtered, check)
		}
	}
	return filtered
}

// CheckCategories lists the categories in the catalog.
func CheckCategories() []string {
	seen := map[string]struct{}{}
	for _, check := range checks {
		seen[check.Category] = struct{}{}
	}
	categories := make([]string, 0, len(seen))
	for category := range seen {
		categories = append(categories, category)
	}
	sort.Strings(categories)
	return categories
}

// IgnoreDirectives documents the escape hatches, so a model reaches for them
// deliberately instead of inventing one.
func IgnoreDirectives() []string {
	return []string{
		"//foundation:ignore-file at the top of a file skips the whole file, for fixtures that contain invalid declarations on purpose.",
		"// foundation:ignore contract on a type declaration skips contract checking for it.",
		"// foundation:ignore handler on a type declaration skips the Handle requirement, for schema only types used by OpenAPI.",
		"Use these for deliberate compatibility or schema fixtures. Silencing a real diagnostic with them moves the failure to runtime.",
	}
}

package analyzer

import (
	"fmt"
	"go/ast"
	"go/types"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const (
	modulePath    = "github.com/mirkobrombin/go-foundation/v2"
	devModulePath = "github.com/mirkobrombin/go-foundation/dev/v2"
	contractsPath = "github.com/mirkobrombin/go-foundation/v2/core/contracts"
	diPath        = "github.com/mirkobrombin/go-foundation/v2/app/di"
	bindPath      = "github.com/mirkobrombin/go-foundation/v2/core/bind"
	appPath       = "github.com/mirkobrombin/go-foundation/v2/app"
	actionsPath   = "github.com/mirkobrombin/go-foundation/v2/app/actions"
)

// Analyzer checks Foundation declarations and wiring before runtime.
var Analyzer = &analysis.Analyzer{
	Name: "foundation",
	Doc:  "check Foundation contracts, handlers, actions, tags, and dependency wiring",
	Run:  run,
}

type provider struct {
	typ types.Type
	pos ast.Node
}

type injection struct {
	name string
	typ  types.Type
	node ast.Node
}

type packageState struct {
	pass        *analysis.Pass
	providers   map[string]provider
	injections  []injection
	routes      map[string]ast.Node
	routeShapes map[string]namedCall
	routeList   []namedCall
	actions     map[string]ast.Node
	schedules   map[string]ast.Node
	dispatches  []namedCall
}

type namedCall struct {
	name string
	node ast.Node
}

func run(pass *analysis.Pass) (any, error) {
	checkArchitecture(pass)

	state := &packageState{
		pass:        pass,
		providers:   make(map[string]provider),
		routes:      make(map[string]ast.Node),
		routeShapes: make(map[string]namedCall),
		actions:     make(map[string]ast.Node),
		schedules:   make(map[string]ast.Node),
	}

	for _, file := range pass.Files {
		if ignoredFile(file) {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch value := node.(type) {
			case *ast.TypeSpec:
				state.checkType(value)
			case *ast.CallExpr:
				state.checkCall(value)
			case *ast.ExprStmt:
				state.checkIgnoredError(value)
			case *ast.AssignStmt:
				state.checkBlankError(value)
			}
			return true
		})
	}

	state.checkWiring()
	state.checkDispatches()
	return nil, nil
}

func checkArchitecture(pass *analysis.Pass) {
	path := pass.Pkg.Path()
	var forbidden []string
	switch {
	case strings.HasPrefix(path, modulePath+"/core/"):
		forbidden = []string{modulePath + "/app", devModulePath}
	case path == modulePath+"/app" || strings.HasPrefix(path, modulePath+"/app/"):
		forbidden = []string{devModulePath}
	default:
		return
	}

	for _, imported := range pass.Pkg.Imports() {
		for _, prefix := range forbidden {
			if imported.Path() == prefix || strings.HasPrefix(imported.Path(), prefix+"/") {
				pass.Reportf(pass.Files[0].Package, "%s cannot import %s", path, imported.Path())
			}
		}
	}
}

func (s *packageState) checkType(spec *ast.TypeSpec) {
	structType, ok := spec.Type.(*ast.StructType)
	if !ok {
		return
	}

	s.checkContracts(spec, structType)
	s.checkTags(spec, structType)
}

func (s *packageState) checkContracts(spec *ast.TypeSpec, structType *ast.StructType) {
	if ignoredDeclaration(s.pass, spec, "contract") {
		return
	}

	declared := s.pass.TypesInfo.Defs[spec.Name]
	if declared == nil {
		return
	}

	for _, field := range structType.Fields.List {
		typeArgs, ok := genericSelector(field.Type, contractsPath, "Implements", s.pass)
		if !ok || len(typeArgs) != 1 {
			continue
		}

		contract, ok := s.pass.TypesInfo.TypeOf(typeArgs[0]).Underlying().(*types.Interface)
		if !ok {
			s.pass.Reportf(typeArgs[0].Pos(), "contracts.Implements requires an interface type")
			continue
		}

		implementation := declared.Type()
		pointer := types.NewPointer(implementation)
		if !types.Implements(implementation, contract) && !types.Implements(pointer, contract) {
			s.pass.Reportf(field.Pos(), "%s declares %s but does not implement it", spec.Name.Name, s.pass.TypesInfo.TypeOf(typeArgs[0]))
		}
	}
}

func (s *packageState) checkTags(spec *ast.TypeSpec, structType *ast.StructType) {
	var method, path, action string
	var routeNode, actionNode ast.Node
	pathFields := make(map[string]ast.Node)

	for _, field := range structType.Fields.List {
		if field.Tag == nil {
			continue
		}
		tag, err := strconv.Unquote(field.Tag.Value)
		if err != nil {
			s.pass.Reportf(field.Tag.Pos(), "invalid struct tag: %v", err)
			continue
		}
		values := reflect.StructTag(tag)

		if value, ok := values.Lookup("method"); ok {
			method = value
			routeNode = field
		}
		if value, ok := values.Lookup("path"); ok {
			if _, hasMethod := values.Lookup("method"); hasMethod {
				path = value
				routeNode = field
			} else if value != "" {
				pathFields[value] = field
			}
		}
		if value, ok := values.Lookup("action"); ok {
			action = value
			actionNode = field
		}
		if value, ok := values.Lookup("inject"); ok {
			if value == "" {
				s.pass.Reportf(field.Tag.Pos(), "inject tag cannot be empty")
				continue
			}
			if len(field.Names) > 0 && !field.Names[0].IsExported() {
				s.pass.Reportf(field.Pos(), "injected field %s must be exported", field.Names[0].Name)
			}
			s.injections = append(s.injections, injection{
				name: value,
				typ:  s.pass.TypesInfo.TypeOf(field.Type),
				node: field,
			})
		}
	}

	if method != "" || path != "" {
		s.checkRoute(spec, method, path, routeNode, pathFields)
		s.checkHandler(spec, routeNode)
	}
	if actionNode != nil {
		s.checkHandler(spec, actionNode)
		if action == "" {
			s.pass.Reportf(actionNode.Pos(), "action tag cannot be empty")
		} else if previous, exists := s.actions[action]; exists {
			s.pass.Report(analysis.Diagnostic{
				Pos:     actionNode.Pos(),
				Message: fmt.Sprintf("action %q is already declared", action),
				Related: []analysis.RelatedInformation{{
					Pos:     previous.Pos(),
					Message: "first declaration",
				}},
			})
		} else {
			s.actions[action] = actionNode
		}
	}
}

func (s *packageState) checkHandler(spec *ast.TypeSpec, node ast.Node) {
	if ignoredDeclaration(s.pass, spec, "handler") {
		return
	}
	declared := s.pass.TypesInfo.Defs[spec.Name]
	if declared == nil || foundationHandler(declared.Type()) {
		return
	}
	s.pass.Reportf(
		node.Pos(),
		"%s must implement Handle(context.Context) (any, error)",
		spec.Name.Name,
	)
}

func foundationHandler(implementation types.Type) bool {
	method := types.NewMethodSet(types.NewPointer(implementation)).Lookup(nil, "Handle")
	if method == nil {
		return false
	}
	signature, ok := method.Obj().Type().(*types.Signature)
	if !ok || signature.Params().Len() != 1 || signature.Results().Len() != 2 {
		return false
	}
	contextType, ok := signature.Params().At(0).Type().(*types.Named)
	if !ok || contextType.Obj().Pkg() == nil ||
		contextType.Obj().Pkg().Path() != "context" ||
		contextType.Obj().Name() != "Context" {
		return false
	}
	return types.Identical(signature.Results().At(0).Type(), types.Universe.Lookup("any").Type()) &&
		types.Identical(signature.Results().At(1).Type(), types.Universe.Lookup("error").Type())
}

func (s *packageState) checkRoute(spec *ast.TypeSpec, method, path string, node ast.Node, fields map[string]ast.Node) {
	if method == "" || path == "" {
		s.pass.Reportf(node.Pos(), "%s must declare method and path together", spec.Name.Name)
		return
	}
	if !validMethod(method) {
		s.pass.Reportf(node.Pos(), "unsupported HTTP method %q", method)
	}
	if !strings.HasPrefix(path, "/") {
		s.pass.Reportf(node.Pos(), "route path must start with /")
		return
	}

	params, err := routeParams(path)
	if err != nil {
		s.pass.Reportf(node.Pos(), "invalid route path: %v", err)
		return
	}
	for name, field := range fields {
		if _, ok := params[name]; !ok {
			s.pass.Reportf(field.Pos(), "path field %q is not present in route %q", name, path)
		}
	}
	for name := range params {
		if _, ok := fields[name]; !ok {
			s.pass.Reportf(node.Pos(), "route parameter %q has no path field", name)
		}
	}

	shape := routeShape(path)
	branchConflict := false
	for _, previous := range s.routeList {
		if routeBranchConflict(path, previous.name) {
			branchConflict = true
			s.pass.Report(analysis.Diagnostic{
				Pos:     node.Pos(),
				Message: fmt.Sprintf("route %q conflicts with parameter route %q", path, previous.name),
				Related: []analysis.RelatedInformation{{
					Pos:     previous.node.Pos(),
					Message: "conflicting declaration",
				}},
			})
			break
		}
	}
	if previous, exists := s.routeShapes[shape]; !branchConflict && exists && previous.name != path {
		s.pass.Report(analysis.Diagnostic{
			Pos:     node.Pos(),
			Message: fmt.Sprintf("route %q conflicts with parameter route %q", path, previous.name),
			Related: []analysis.RelatedInformation{{
				Pos:     previous.node.Pos(),
				Message: "conflicting declaration",
			}},
		})
	} else {
		s.routeShapes[shape] = namedCall{name: path, node: node}
	}
	s.routeList = append(s.routeList, namedCall{name: path, node: node})

	key := method + " " + shape
	if previous, exists := s.routes[key]; exists {
		s.pass.Report(analysis.Diagnostic{
			Pos:     node.Pos(),
			Message: fmt.Sprintf("route %s is already declared", key),
			Related: []analysis.RelatedInformation{{
				Pos:     previous.Pos(),
				Message: "first declaration",
			}},
		})
		return
	}
	s.routes[key] = node
}

func (s *packageState) checkCall(call *ast.CallExpr) {
	fn := calledFunction(call, s.pass)
	if fn == nil || fn.Pkg() == nil {
		return
	}

	switch {
	case fn.Pkg().Path() == diPath && (fn.Name() == "RegisterImpl" || fn.Name() == "RegisterAs"):
		s.checkImplementationCall(call, fn.Name())
	case fn.Pkg().Path() == diPath && fn.Name() == "RegisterFromFunc":
		s.checkConstructor(call)
	case fn.Name() == "Provide" && isProviderPackage(fn.Pkg().Path()):
		s.collectProvider(call)
	case fn.Name() == "Dispatch" && isDispatchPackage(fn.Pkg().Path()):
		s.collectDispatch(call)
	case fn.Pkg().Path() == appPath && fn.Name() == "RegisterAction":
		s.collectRegisteredAction(call)
	case fn.Pkg().Path() == appPath && fn.Name() == "Schedule":
		s.checkSchedule(call)
	}
}

func (s *packageState) checkSchedule(call *ast.CallExpr) {
	if len(call.Args) < 3 {
		return
	}
	name, nameLiteral := stringLiteral(call.Args[0])
	if nameLiteral {
		switch {
		case name == "":
			s.pass.Reportf(call.Args[0].Pos(), "scheduled job name cannot be empty")
		case s.schedules[name] != nil:
			s.pass.Report(analysis.Diagnostic{
				Pos:     call.Args[0].Pos(),
				Message: fmt.Sprintf("scheduled job %q is already registered", name),
				Related: []analysis.RelatedInformation{{
					Pos:     s.schedules[name].Pos(),
					Message: "first registration",
				}},
			})
		default:
			s.schedules[name] = call.Args[0]
		}
	}
	cron, cronLiteral := stringLiteral(call.Args[1])
	if cronLiteral {
		if err := validateCron(cron); err != nil {
			s.pass.Reportf(call.Args[1].Pos(), "%v", err)
		}
	}
	if ident, ok := call.Args[2].(*ast.Ident); ok && ident.Name == "nil" {
		s.pass.Reportf(call.Args[2].Pos(), "scheduled job handler cannot be nil")
	}
}

func validateCron(expression string) error {
	fields := strings.Fields(expression)
	if len(fields) != 5 {
		return fmt.Errorf("invalid cron expression %q: expected 5 fields", expression)
	}
	ranges := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6}}
	for index, field := range fields {
		if field == "*" {
			continue
		}
		value, err := strconv.Atoi(field)
		if err != nil || value < ranges[index][0] || value > ranges[index][1] {
			return fmt.Errorf("invalid cron field %q", field)
		}
	}
	return nil
}

func (s *packageState) checkImplementationCall(call *ast.CallExpr, name string) {
	typeArgs := instantiatedTypeArgs(call.Fun, s.pass)
	if len(typeArgs) != 2 {
		return
	}
	contract, ok := typeArgs[0].Underlying().(*types.Interface)
	if !ok {
		s.pass.Reportf(call.Fun.Pos(), "di.%s requires an interface as its first type argument", name)
		return
	}
	implementation := typeArgs[1]
	if implementation != nil && !types.Implements(implementation, contract) {
		s.pass.Reportf(call.Fun.Pos(), "%s does not implement %s", implementation, typeArgs[0])
	}
}

func (s *packageState) checkConstructor(call *ast.CallExpr) {
	typeArgs := instantiatedTypeArgs(call.Fun, s.pass)
	if len(typeArgs) != 1 || len(call.Args) < 2 {
		return
	}
	want := typeArgs[0]
	signature, ok := s.pass.TypesInfo.TypeOf(call.Args[1]).Underlying().(*types.Signature)
	if !ok {
		s.pass.Reportf(call.Args[1].Pos(), "di.RegisterFromFunc constructor must be a function")
		return
	}
	results := signature.Results()
	if results.Len() < 1 || results.Len() > 2 {
		s.pass.Reportf(call.Args[1].Pos(), "constructor must return a value and optional error")
		return
	}
	if !types.AssignableTo(results.At(0).Type(), want) {
		s.pass.Reportf(call.Args[1].Pos(), "constructor returns %s, want %s", results.At(0).Type(), want)
	}
	if results.Len() == 2 && !types.AssignableTo(results.At(1).Type(), types.Universe.Lookup("error").Type()) {
		s.pass.Reportf(call.Args[1].Pos(), "constructor second result must be error")
	}
}

func instantiatedTypeArgs(expr ast.Expr, pass *analysis.Pass) []types.Type {
	var ident *ast.Ident
	switch value := expr.(type) {
	case *ast.IndexExpr:
		ident = genericIdent(value.X)
	case *ast.IndexListExpr:
		ident = genericIdent(value.X)
	default:
		return nil
	}
	instance, ok := pass.TypesInfo.Instances[ident]
	if !ok {
		return nil
	}
	args := make([]types.Type, instance.TypeArgs.Len())
	for i := range args {
		args[i] = instance.TypeArgs.At(i)
	}
	return args
}

func genericIdent(expr ast.Expr) *ast.Ident {
	switch value := expr.(type) {
	case *ast.SelectorExpr:
		return value.Sel
	case *ast.Ident:
		return value
	default:
		return nil
	}
}

func (s *packageState) collectProvider(call *ast.CallExpr) {
	if len(call.Args) < 2 {
		return
	}
	name, ok := stringLiteral(call.Args[0])
	if !ok || name == "" {
		return
	}
	s.providers[name] = provider{
		typ: s.pass.TypesInfo.TypeOf(call.Args[1]),
		pos: call,
	}
}

func (s *packageState) collectDispatch(call *ast.CallExpr) {
	if len(call.Args) < 2 {
		return
	}
	name, ok := stringLiteral(call.Args[1])
	if ok {
		s.dispatches = append(s.dispatches, namedCall{name: name, node: call.Args[1]})
	}
}

func (s *packageState) collectRegisteredAction(call *ast.CallExpr) {
	if len(call.Args) < 1 {
		return
	}
	name, ok := stringLiteral(call.Args[0])
	if ok {
		s.actions[name] = call.Args[0]
	}
}

func (s *packageState) checkWiring() {
	if len(s.providers) == 0 {
		return
	}
	for _, injection := range s.injections {
		provider, ok := s.providers[injection.name]
		if !ok {
			s.pass.Reportf(injection.node.Pos(), "dependency %q is not provided in this package", injection.name)
			continue
		}
		if provider.typ != nil && injection.typ != nil && !types.AssignableTo(provider.typ, injection.typ) {
			s.pass.Report(analysis.Diagnostic{
				Pos:     injection.node.Pos(),
				Message: fmt.Sprintf("dependency %q has type %s, want %s", injection.name, provider.typ, injection.typ),
				Related: []analysis.RelatedInformation{{
					Pos:     provider.pos.Pos(),
					Message: "provider",
				}},
			})
		}
	}
}

func (s *packageState) checkDispatches() {
	if len(s.actions) == 0 {
		return
	}
	for _, dispatch := range s.dispatches {
		if _, ok := s.actions[dispatch.name]; !ok {
			s.pass.Reportf(dispatch.node.Pos(), "action %q is not registered in this package", dispatch.name)
		}
	}
}

func (s *packageState) checkIgnoredError(stmt *ast.ExprStmt) {
	call, ok := stmt.X.(*ast.CallExpr)
	if ok && isBindCall(call, s.pass) {
		s.pass.Reportf(call.Pos(), "binding error is ignored")
	}
}

func (s *packageState) checkBlankError(stmt *ast.AssignStmt) {
	if len(stmt.Lhs) != 1 || len(stmt.Rhs) != 1 {
		return
	}
	ident, ok := stmt.Lhs[0].(*ast.Ident)
	call, isCall := stmt.Rhs[0].(*ast.CallExpr)
	if ok && isCall && ident.Name == "_" && isBindCall(call, s.pass) {
		s.pass.Reportf(call.Pos(), "binding error is ignored")
	}
}

func genericSelector(expr ast.Expr, path, name string, pass *analysis.Pass) ([]ast.Expr, bool) {
	args := genericArgs(expr)
	var base ast.Expr
	switch value := expr.(type) {
	case *ast.IndexExpr:
		base = value.X
	case *ast.IndexListExpr:
		base = value.X
	default:
		return nil, false
	}
	selector, ok := base.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	object, ok := pass.TypesInfo.Uses[selector.Sel].(*types.TypeName)
	return args, ok && object.Pkg() != nil && object.Pkg().Path() == path && object.Name() == name
}

func genericArgs(expr ast.Expr) []ast.Expr {
	switch value := expr.(type) {
	case *ast.IndexExpr:
		return []ast.Expr{value.Index}
	case *ast.IndexListExpr:
		return value.Indices
	default:
		return nil
	}
}

func calledFunction(call *ast.CallExpr, pass *analysis.Pass) *types.Func {
	expr := call.Fun
	switch value := expr.(type) {
	case *ast.IndexExpr:
		expr = value.X
	case *ast.IndexListExpr:
		expr = value.X
	}
	switch value := expr.(type) {
	case *ast.SelectorExpr:
		if selection := pass.TypesInfo.Selections[value]; selection != nil {
			fn, _ := selection.Obj().(*types.Func)
			return fn
		}
		fn, _ := pass.TypesInfo.Uses[value.Sel].(*types.Func)
		return fn
	case *ast.Ident:
		fn, _ := pass.TypesInfo.Uses[value].(*types.Func)
		if fn == nil {
			fn, _ = pass.TypesInfo.Defs[value].(*types.Func)
		}
		return fn
	default:
		return nil
	}
}

func validMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodConnect,
		http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

func routeParams(path string) (map[string]struct{}, error) {
	params := make(map[string]struct{})
	segments := strings.Split(strings.Trim(path, "/"), "/")
	for index, segment := range segments {
		if !strings.HasPrefix(segment, "{") {
			if strings.ContainsAny(segment, "{}") {
				return nil, fmt.Errorf("malformed segment %q", segment)
			}
			continue
		}
		if !strings.HasSuffix(segment, "}") {
			return nil, fmt.Errorf("unclosed parameter %q", segment)
		}
		name := strings.TrimSuffix(strings.TrimPrefix(segment, "{"), "}")
		if strings.HasPrefix(name, "*") {
			if index != len(segments)-1 {
				return nil, fmt.Errorf("catch-all parameter must be last")
			}
			name = strings.TrimPrefix(name, "*")
			if strings.Contains(name, ":") {
				return nil, fmt.Errorf("catch-all parameters cannot have constraints")
			}
		}
		if index := strings.IndexByte(name, ':'); index >= 0 {
			if err := validateRouteConstraint(name[index+1:]); err != nil {
				return nil, err
			}
			name = name[:index]
		}
		if name == "" {
			return nil, fmt.Errorf("empty parameter")
		}
		if !validRouteParameterName(name) {
			return nil, fmt.Errorf("invalid parameter name %q", name)
		}
		if _, exists := params[name]; exists {
			return nil, fmt.Errorf("duplicate parameter %q", name)
		}
		params[name] = struct{}{}
	}
	return params, nil
}

func validateRouteConstraint(value string) error {
	if value == "int" || value == "alpha" {
		return nil
	}
	if strings.HasPrefix(value, "regex(") && strings.HasSuffix(value, ")") {
		if _, err := regexp.Compile(value[6 : len(value)-1]); err != nil {
			return fmt.Errorf("invalid regex constraint: %w", err)
		}
		return nil
	}
	for _, item := range strings.Split(value, ",") {
		if item != "int" && item != "alpha" {
			return fmt.Errorf("unknown route constraint %q", item)
		}
	}
	return nil
}

func routeShape(path string) string {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	for index, segment := range segments {
		if isRouteParameter(segment) {
			if strings.HasPrefix(segment, "{*") {
				segments[index] = "{*}"
			} else {
				segments[index] = "{}"
			}
		}
	}
	return "/" + strings.Join(segments, "/")
}

func routeBranchConflict(left, right string) bool {
	leftSegments := strings.Split(strings.Trim(left, "/"), "/")
	rightSegments := strings.Split(strings.Trim(right, "/"), "/")
	limit := min(len(leftSegments), len(rightSegments))
	for index := 0; index < limit; index++ {
		leftSegment := leftSegments[index]
		rightSegment := rightSegments[index]
		leftParameter := isRouteParameter(leftSegment)
		rightParameter := isRouteParameter(rightSegment)
		if !leftParameter || !rightParameter {
			if leftParameter != rightParameter || leftSegment != rightSegment {
				return false
			}
			continue
		}
		if leftSegment != rightSegment {
			return true
		}
	}
	return false
}

func validRouteParameterName(name string) bool {
	if name == "" {
		return false
	}
	for index, character := range name {
		if index == 0 {
			if character != '_' &&
				(character < 'A' || character > 'Z') &&
				(character < 'a' || character > 'z') {
				return false
			}
			continue
		}
		if character != '_' &&
			(character < 'A' || character > 'Z') &&
			(character < 'a' || character > 'z') &&
			(character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func isRouteParameter(segment string) bool {
	return strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}")
}

func stringLiteral(expr ast.Expr) (string, bool) {
	literal, ok := expr.(*ast.BasicLit)
	if !ok || literal.Kind.String() != "STRING" {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	return value, err == nil
}

func isProviderPackage(path string) bool {
	return path == appPath || path == actionsPath || path == diPath
}

func isDispatchPackage(path string) bool {
	return path == appPath || path == actionsPath
}

func isBindCall(call *ast.CallExpr, pass *analysis.Pass) bool {
	fn := calledFunction(call, pass)
	return fn != nil && fn.Pkg() != nil && fn.Pkg().Path() == bindPath &&
		(fn.Name() == "Bind" || fn.Name() == "BindJSON")
}

func ignored(doc *ast.CommentGroup, category string) bool {
	return doc != nil && strings.Contains(doc.Text(), "foundation:ignore "+category)
}

func ignoredDeclaration(pass *analysis.Pass, spec *ast.TypeSpec, category string) bool {
	if ignored(spec.Doc, category) {
		return true
	}
	declarationLine := pass.Fset.Position(spec.Pos()).Line
	for _, file := range pass.Files {
		if spec.Pos() < file.Pos() || spec.Pos() > file.End() {
			continue
		}
		for _, group := range file.Comments {
			if group.End() > spec.Pos() {
				continue
			}
			if pass.Fset.Position(group.End()).Line == declarationLine-1 &&
				ignored(group, category) {
				return true
			}
		}
	}
	return false
}

func ignoredFile(file *ast.File) bool {
	for _, group := range file.Comments {
		for _, comment := range group.List {
			if strings.Contains(comment.Text, "foundation:ignore-file") {
				return true
			}
		}
	}
	return false
}

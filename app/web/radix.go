package web

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

type methodHandlers map[string]HandlerFunc

type paramConstraint struct {
	name     string
	validate func(string) bool
}

type radixNode struct {
	children    []*radixNode
	segment     string
	isParam     bool
	isCatchAll  bool
	paramName   string
	constraints []paramConstraint
	handlers    methodHandlers
}

type radixTree struct {
	root *radixNode
	mu   sync.RWMutex
}

func newRadixTree() *radixTree {
	return &radixTree{
		root: &radixNode{children: make([]*radixNode, 0)},
	}
}

func (t *radixTree) insert(method, path string, handler HandlerFunc) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !validRouteMethod(method) {
		return fmt.Errorf("web: unsupported HTTP method %q", method)
	}
	if handler == nil {
		return fmt.Errorf("web: route handler cannot be nil")
	}
	if err := validateRoutePath(path); err != nil {
		return err
	}
	segments := splitPath(path)

	if len(segments) > 0 && isCatchAll(segments[len(segments)-1]) {
		segments = segments[:len(segments)-1]
		node, err := t.walkOrCreate(t.root, segments)
		if err != nil {
			return err
		}
		paramName := parseCatchAll(path)
		if paramName == "" {
			return fmt.Errorf("web: catch-all parameter name cannot be empty")
		}
		if strings.Contains(paramName, ":") {
			return fmt.Errorf("web: catch-all parameters cannot have constraints")
		}
		for _, child := range node.children {
			if child.isParam {
				return fmt.Errorf("web: catch-all route %s conflicts with a parameter route", path)
			}
			if child.isCatchAll {
				if child.paramName != paramName {
					return fmt.Errorf("web: conflicting catch-all route %s", path)
				}
				if _, exists := child.handlers[method]; exists {
					return fmt.Errorf("web: route %s %s is already registered", method, path)
				}
				child.handlers[method] = handler
				return nil
			}
		}
		catchAllNode := &radixNode{
			isCatchAll: true,
			paramName:  paramName,
			children:   make([]*radixNode, 0),
		}
		node.children = append(node.children, catchAllNode)
		if catchAllNode.handlers == nil {
			catchAllNode.handlers = make(methodHandlers)
		}
		catchAllNode.handlers[method] = handler
		return nil
	}

	node, err := t.walkOrCreate(t.root, segments)
	if err != nil {
		return err
	}
	if node.handlers == nil {
		node.handlers = make(methodHandlers)
	}
	if _, exists := node.handlers[method]; exists {
		return fmt.Errorf("web: route %s %s is already registered", method, path)
	}
	node.handlers[method] = handler
	return nil
}

func (t *radixTree) walkOrCreate(node *radixNode, segments []string) (*radixNode, error) {
	for _, seg := range segments {
		if isParam(seg) {
			name, constraints, err := parseParamConstraints(seg)
			if err != nil {
				return nil, err
			}
			node, err = findOrCreateParam(node, name, constraints)
			if err != nil {
				return nil, err
			}
		} else {
			if strings.ContainsAny(seg, "{}") {
				return nil, fmt.Errorf("web: malformed route segment %q", seg)
			}
			node = findOrCreateStatic(node, seg)
		}
	}
	return node, nil
}

func findOrCreateStatic(parent *radixNode, segment string) *radixNode {
	for _, child := range parent.children {
		if !child.isParam && !child.isCatchAll && child.segment == segment {
			return child
		}
	}
	node := &radixNode{
		segment:  segment,
		isParam:  false,
		children: make([]*radixNode, 0),
	}
	parent.children = append(parent.children, node)
	return node
}

func findOrCreateParam(parent *radixNode, paramName string, constraints []paramConstraint) (*radixNode, error) {
	if paramName == "" {
		return nil, fmt.Errorf("web: route parameter name cannot be empty")
	}
	for _, child := range parent.children {
		if child.isCatchAll {
			return nil, fmt.Errorf("web: parameter route %q conflicts with a catch-all route", paramName)
		}
		if child.isParam {
			if child.paramName != paramName || !sameConstraints(child.constraints, constraints) {
				return nil, fmt.Errorf("web: ambiguous parameter route for %q", paramName)
			}
			return child, nil
		}
	}
	node := &radixNode{
		isParam:     true,
		paramName:   paramName,
		constraints: constraints,
		children:    make([]*radixNode, 0),
	}
	parent.children = append(parent.children, node)
	return node, nil
}

func sameConstraints(left, right []paramConstraint) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i].name != right[i].name {
			return false
		}
	}
	return true
}

func (t *radixTree) lookup(method, path string) (HandlerFunc, map[string]string) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	segments := splitPath(path)
	params := make(map[string]string)
	handler, ok := lookupNode(t.root, method, segments, 0, params)
	if !ok {
		return nil, nil
	}
	return handler, params
}

func lookupNode(node *radixNode, method string, segments []string, index int, params map[string]string) (HandlerFunc, bool) {
	if index == len(segments) {
		if handler, ok := node.handlers[method]; ok {
			return handler, true
		}
		for _, child := range node.children {
			if !child.isCatchAll {
				continue
			}
			if handler, ok := child.handlers[method]; ok {
				params[child.paramName] = ""
				return handler, true
			}
		}
		return nil, false
	}

	segment := segments[index]
	for _, child := range node.children {
		if child.isParam || child.isCatchAll || child.segment != segment {
			continue
		}
		if handler, ok := lookupNode(child, method, segments, index+1, params); ok {
			return handler, true
		}
	}

	for _, child := range node.children {
		if !child.isParam || !constraintsMatch(child.constraints, segment) {
			continue
		}
		previous, existed := params[child.paramName]
		params[child.paramName] = segment
		if handler, ok := lookupNode(child, method, segments, index+1, params); ok {
			return handler, true
		}
		if existed {
			params[child.paramName] = previous
		} else {
			delete(params, child.paramName)
		}
	}

	for _, child := range node.children {
		if !child.isCatchAll {
			continue
		}
		handler, ok := child.handlers[method]
		if !ok {
			continue
		}
		params[child.paramName] = strings.Join(segments[index:], "/")
		return handler, true
	}
	return nil, false
}

func constraintsMatch(constraints []paramConstraint, value string) bool {
	for _, constraint := range constraints {
		if !constraint.validate(value) {
			return false
		}
	}
	return true
}

func splitPath(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func isParam(seg string) bool {
	return len(seg) > 2 && seg[0] == '{' && seg[len(seg)-1] == '}'
}

func isCatchAll(seg string) bool {
	return len(seg) > 3 && seg[0] == '{' && seg[1] == '*' && seg[len(seg)-1] == '}'
}

func parseCatchAll(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	last := parts[len(parts)-1]
	if isCatchAll(last) {
		return last[2 : len(last)-1]
	}
	return ""
}

func parseParamConstraints(seg string) (string, []paramConstraint, error) {
	inner := seg[1 : len(seg)-1]
	colonIdx := strings.Index(inner, ":")
	if colonIdx < 0 {
		return inner, nil, nil
	}

	name := inner[:colonIdx]
	constraintStr := inner[colonIdx+1:]
	var constraints []paramConstraint

	rawConstraints := strings.Split(constraintStr, ",")
	if strings.HasPrefix(constraintStr, "regex(") {
		rawConstraints = []string{constraintStr}
	}
	for _, c := range rawConstraints {
		switch c {
		case "int":
			constraints = append(constraints, paramConstraint{
				name:     "int",
				validate: isIntConstraint,
			})
		case "alpha":
			constraints = append(constraints, paramConstraint{
				name:     "alpha",
				validate: isAlphaConstraint,
			})
		default:
			if strings.HasPrefix(c, "regex(") && strings.HasSuffix(c, ")") {
				pattern := c[6 : len(c)-1]
				validator, err := isRegexConstraint(pattern)
				if err != nil {
					return "", nil, fmt.Errorf("web: invalid regex constraint %q: %w", pattern, err)
				}
				constraints = append(constraints, paramConstraint{
					name:     c,
					validate: validator,
				})
				continue
			}
			return "", nil, fmt.Errorf("web: unknown route constraint %q", c)
		}
	}
	return name, constraints, nil
}

func isIntConstraint(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

func isAlphaConstraint(s string) bool {
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			return false
		}
	}
	return true
}

func isRegexConstraint(pattern string) (func(string) bool, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return func(s string) bool {
		return re.MatchString(s)
	}, nil
}

func validRouteMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodConnect,
		http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

func validateRoutePath(path string) error {
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("web: route path must start with /")
	}
	seen := make(map[string]struct{})
	segments := splitPath(path)
	for index, segment := range segments {
		if !strings.ContainsAny(segment, "{}") {
			continue
		}
		if !isParam(segment) {
			return fmt.Errorf("web: malformed route segment %q", segment)
		}

		name := segment[1 : len(segment)-1]
		if strings.HasPrefix(name, "*") {
			if !isCatchAll(segment) || index != len(segments)-1 {
				return fmt.Errorf("web: catch-all parameter must be last and well formed")
			}
			name = strings.TrimPrefix(name, "*")
			if strings.Contains(name, ":") {
				return fmt.Errorf("web: catch-all parameters cannot have constraints")
			}
		} else if separator := strings.IndexByte(name, ':'); separator >= 0 {
			if _, _, err := parseParamConstraints(segment); err != nil {
				return err
			}
			name = name[:separator]
		}
		if !validRouteParameterName(name) {
			return fmt.Errorf("web: invalid route parameter name %q", name)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("web: duplicate route parameter %q", name)
		}
		seen[name] = struct{}{}
	}
	return nil
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

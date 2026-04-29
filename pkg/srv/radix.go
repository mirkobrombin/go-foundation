package srv

import (
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
	children   []*radixNode
	segment    string
	isParam    bool
	isCatchAll bool
	paramName  string
	constraints []paramConstraint
	handlers   methodHandlers
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

func (t *radixTree) insert(method, path string, handler HandlerFunc) {
	t.mu.Lock()
	defer t.mu.Unlock()

	segments := splitPath(path)

	if len(segments) > 0 && isCatchAll(segments[len(segments)-1]) {
		segments = segments[:len(segments)-1]
		node := t.walkOrCreate(t.root, segments)
		paramName, _ := parseCatchAll(path)
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
		return
	}

	node := t.walkOrCreate(t.root, segments)
	if node.handlers == nil {
		node.handlers = make(methodHandlers)
	}
	node.handlers[method] = handler
}

func (t *radixTree) walkOrCreate(node *radixNode, segments []string) *radixNode {
	for _, seg := range segments {
		if isParam(seg) {
			name, constraints := parseParamConstraints(seg)
			node = findOrCreateParam(node, name, constraints)
		} else {
			node = findOrCreateStatic(node, seg)
		}
	}
	return node
}

func findOrCreateStatic(parent *radixNode, segment string) *radixNode {
	for _, child := range parent.children {
		if !child.isParam && !child.isCatchAll && child.segment == segment {
			return child
		}
	}
	node := &radixNode{
		segment:   segment,
		isParam:   false,
		children:  make([]*radixNode, 0),
	}
	parent.children = append(parent.children, node)
	return node
}

func findOrCreateParam(parent *radixNode, paramName string, constraints []paramConstraint) *radixNode {
	for _, child := range parent.children {
		if child.isParam && child.paramName == paramName {
			if len(constraints) > 0 && len(child.constraints) == 0 {
				child.constraints = constraints
			}
			return child
		}
	}
	node := &radixNode{
		isParam:    true,
		paramName:  paramName,
		constraints: constraints,
		children:   make([]*radixNode, 0),
	}
	parent.children = append(parent.children, node)
	return node
}

func (t *radixTree) lookup(method, path string) (HandlerFunc, map[string]string) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	segments := splitPath(path)
	params := make(map[string]string)
	node := t.root

	segmentIdx := 0
	for segmentIdx < len(segments) {
		seg := segments[segmentIdx]

		var next *radixNode
		for _, child := range node.children {
			if !child.isParam && !child.isCatchAll && child.segment == seg {
				next = child
				break
			}
		}

		if next == nil {
			for _, child := range node.children {
				if child.isCatchAll {
					remaining := strings.Join(segments[segmentIdx:], "/")
					params[child.paramName] = remaining
					handler, ok := child.handlers[method]
					if !ok {
						return nil, nil
					}
					return handler, params
				}
				if child.isParam {
					if len(child.constraints) > 0 {
						allValid := true
						for _, c := range child.constraints {
							if !c.validate(seg) {
								allValid = false
								break
							}
						}
						if !allValid {
							return nil, nil
						}
					}
					params[child.paramName] = seg
					next = child
					break
				}
			}
		}

		if next == nil {
			return nil, nil
		}
		node = next
		segmentIdx++
	}

	if node.handlers == nil {
		for _, child := range node.children {
			if child.isCatchAll {
				params[child.paramName] = ""
				handler, ok := child.handlers[method]
				if !ok {
					return nil, nil
				}
				return handler, params
			}
		}
		return nil, nil
	}

	handler, ok := node.handlers[method]
	if !ok {
		return nil, nil
	}
	return handler, params
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

func parseCatchAll(path string) (string, string) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	last := parts[len(parts)-1]
	if isCatchAll(last) {
		return last[2 : len(last)-1], ""
	}
	return "", ""
}

func parseParamConstraints(seg string) (string, []paramConstraint) {
	inner := seg[1 : len(seg)-1]
	colonIdx := strings.Index(inner, ":")
	if colonIdx < 0 {
		return inner, nil
	}

	name := inner[:colonIdx]
	constraintStr := inner[colonIdx+1:]
	var constraints []paramConstraint

	for _, c := range strings.Split(constraintStr, ",") {
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
				constraints = append(constraints, paramConstraint{
					name:     c,
					validate: isRegexConstraint(pattern),
				})
			}
		}
	}
	return name, constraints
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

func isRegexConstraint(pattern string) func(string) bool {
	re := regexp.MustCompile(pattern)
	return func(s string) bool {
		return re.MatchString(s)
	}
}
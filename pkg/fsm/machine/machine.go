package machine

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/mirkobrombin/go-foundation/pkg/tags"

	"github.com/mirkobrombin/go-foundation/pkg/fsm/parser"
	"github.com/mirkobrombin/go-foundation/pkg/fsm/visualizer"
)

type stateHooks struct {
	can   func() error
	enter func()
	exit  func()
}

// Machine manages an object's state machine with transitions and hooks.
type Machine struct {
	obj           any
	val           reflect.Value
	stateField    reflect.Value
	stateType     reflect.StructField
	transitions   map[string][]string
	wildcards     []string
	initialState  string
	hooks         map[string]stateHooks
	mu            sync.RWMutex
	history       []TransitionRecord
	listeners     []Listener
	timeouts      map[string]parser.TimeoutRule
	lastStateTime time.Time
}

// New creates a Machine from a struct pointer with an "fsm" tag field.
func New(obj any) (*Machine, error) {
	val := reflect.ValueOf(obj)
	if val.Kind() != reflect.Ptr || val.Elem().Kind() != reflect.Struct {
		return nil, errors.New("obj must be a pointer to a struct")
	}

	elem := val.Elem()

	// Optimization: Use cached tags parser
	fields := tags.NewParser("fsm").ParseStruct(obj)

	for _, meta := range fields {
		if meta.Type.Kind() != reflect.String {
			return nil, fmt.Errorf("field '%s' must be a string", meta.Name)
		}

		cfg, err := parser.Parse(meta.RawTag)
		if err != nil {
			return nil, err
		}

		m := &Machine{
			obj:          obj,
			val:          elem,
			stateField:   elem.Field(meta.Index),
			stateType:    elem.Type().Field(meta.Index),
			transitions:  cfg.Transitions,
			wildcards:    cfg.Wildcards,
			initialState: cfg.InitialState,
			timeouts:     cfg.Timeouts,
			hooks:        make(map[string]stateHooks),
			history:      make([]TransitionRecord, 0),
			listeners:    make([]Listener, 0),
		}

		m.initHooks()

		current := m.stateField.String()
		if current == "" && m.initialState != "" {
			m.stateField.SetString(m.initialState)
			current = m.initialState
		}
		m.lastStateTime = time.Now()

		return m, nil
	}

	return nil, errors.New("no field with 'fsm' tag found")
}

func (m *Machine) initHooks() {
	states := make(map[string]struct{})
	if m.initialState != "" {
		states[m.initialState] = struct{}{}
	}
	for src, dsts := range m.transitions {
		states[src] = struct{}{}
		for _, dst := range dsts {
			states[dst] = struct{}{}
		}
	}
	for _, dst := range m.wildcards {
		states[dst] = struct{}{}
	}

	objVal := reflect.ValueOf(m.obj)
	getMethod := func(name string) reflect.Value { return objVal.MethodByName(name) }

	for state := range states {
		normalized := normalizeStateName(state)
		h := stateHooks{}

		if mVal := getMethod("Can" + normalized); mVal.IsValid() {
			if fn, ok := mVal.Interface().(func() error); ok {
				h.can = fn
			}
		}
		if mVal := getMethod("OnEnter" + normalized); mVal.IsValid() {
			if fn, ok := mVal.Interface().(func()); ok {
				h.enter = fn
			}
		}
		if mVal := getMethod("OnExit" + normalized); mVal.IsValid() {
			if fn, ok := mVal.Interface().(func()); ok {
				h.exit = fn
			}
		}
		m.hooks[state] = h
	}
}

// CurrentState returns the current state value.
func (m *Machine) CurrentState() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.stateField.String()
}

// History returns a copy of all transition records.
func (m *Machine) History() []TransitionRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]TransitionRecord(nil), m.history...)
}

// Subscribe registers a listener for state change events.
func (m *Machine) Subscribe(l Listener) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listeners = append(m.listeners, l)
}

func (m *Machine) emitEvent(eventType EventType, from, to string) {
	evt := Event{
		Type:      eventType,
		From:      from,
		To:        to,
		Timestamp: time.Now(),
		Machine:   m,
	}
	m.mu.RLock()
	listeners := make([]Listener, len(m.listeners))
	copy(listeners, m.listeners)
	m.mu.RUnlock()
	for _, l := range listeners {
		l(evt)
	}
}

// CanTransition checks whether a transition to target is allowed.
func (m *Machine) CanTransition(target string) error {
	m.mu.RLock()
	current := m.stateField.String()
	m.mu.RUnlock()

	allowed := slices.Contains(m.wildcards, target)
	if !allowed {
		m.mu.RLock()
		if dests, ok := m.transitions[current]; ok {
			if slices.Contains(dests, target) {
				allowed = true
			}
		}
		m.mu.RUnlock()
	}

	if !allowed {
		return fmt.Errorf("transition from '%s' to '%s' not allowed", current, target)
	}

	if h, ok := m.hooks[target]; ok && h.can != nil {
		if err := h.can(); err != nil {
			return err
		}
	}

	return nil
}

// Transition moves the machine to the target state if allowed.
func (m *Machine) Transition(target string) error {
	return m.transitionInternal(target, "manual")
}

func (m *Machine) transitionInternal(target string, trigger string) error {
	if err := m.CanTransition(target); err != nil {
		return err
	}

	m.mu.Lock()
	current := m.stateField.String()
	m.mu.Unlock()

	m.emitEvent(BeforeTransition, current, target)

	if current != "" {
		if h, ok := m.hooks[current]; ok && h.exit != nil {
			h.exit()
		}
		m.emitEvent(ExitState, current, target)
	}

	m.mu.Lock()
	m.stateField.SetString(target)
	m.lastStateTime = time.Now()
	m.history = append(m.history, TransitionRecord{
		From:      current,
		To:        target,
		Timestamp: time.Now(),
		Trigger:   trigger,
	})
	m.mu.Unlock()

	if h, ok := m.hooks[target]; ok && h.enter != nil {
		h.enter()
	}
	m.emitEvent(EnterState, current, target)
	m.emitEvent(AfterTransition, current, target)

	return nil
}

// CheckTimeouts transitions on timeout if a rule has expired.
func (m *Machine) CheckTimeouts() error {
	m.mu.RLock()
	current := m.stateField.String()
	elapsed := time.Since(m.lastStateTime)
	rule, exists := m.timeouts[current]
	m.mu.RUnlock()

	if exists && elapsed > rule.Duration {
		return m.transitionInternal(rule.ToState, "timeout")
	}
	return nil
}

// GetStructure returns the initial state, transitions, and wildcards.
func (m *Machine) GetStructure() (string, map[string][]string, []string) {
	return m.initialState, m.transitions, m.wildcards
}

// ToMermaid returns a Mermaid diagram of the state machine.
func (m *Machine) ToMermaid() string {
	return visualizer.ToMermaid(m)
}

// ToGraphviz returns a Graphviz DOT diagram of the state machine.
func (m *Machine) ToGraphviz() string {
	return visualizer.ToGraphviz(m)
}

func normalizeStateName(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		r := []rune(p)
		if len(r) > 0 {
			r[0] = unicode.ToUpper(r[0])
		}
		parts[i] = string(r)
	}
	return strings.Join(parts, "")
}

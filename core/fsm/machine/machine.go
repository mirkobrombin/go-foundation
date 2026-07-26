package machine

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/mirkobrombin/go-foundation/v2/core/tags"

	"github.com/mirkobrombin/go-foundation/v2/core/contracts"
	"github.com/mirkobrombin/go-foundation/v2/core/fsm/parser"
	"github.com/mirkobrombin/go-foundation/v2/core/fsm/visualizer"
)

var _ = contracts.Assert[visualizer.VisualizerInterface]((*Machine)(nil))

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
	transitionSet map[string]map[string]bool
	wildcards     []string
	wildcardSet   map[string]bool
	initialState  string
	hooks         map[string]stateHooks
	mu            sync.Mutex
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

	fields := tags.NewParser("fsm").ParseStruct(obj)

	for _, meta := range fields {
		if meta.Type.Kind() != reflect.String {
			return nil, fmt.Errorf("field '%s' must be a string", meta.Name)
		}

		cfg, err := parser.Parse(meta.RawTag)
		if err != nil {
			return nil, err
		}

		sf := elem.Type().Field(meta.Index)

		m := &Machine{
			obj:          obj,
			val:          elem,
			stateField:   elem.Field(meta.Index),
			stateType:    sf,
			transitions:  cfg.Transitions,
			wildcards:    cfg.Wildcards,
			initialState: cfg.InitialState,
			timeouts:     cfg.Timeouts,
			hooks:        make(map[string]stateHooks),
			history:      make([]TransitionRecord, 0),
			listeners:    make([]Listener, 0),
		}
		m.transitionSet = make(map[string]map[string]bool, len(cfg.Transitions))
		for src, dsts := range cfg.Transitions {
			s := make(map[string]bool, len(dsts))
			for _, d := range dsts {
				s[d] = true
			}
			m.transitionSet[src] = s
		}
		m.wildcardSet = make(map[string]bool, len(cfg.Wildcards))
		for _, w := range cfg.Wildcards {
			m.wildcardSet[w] = true
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
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stateField.String()
}

// History returns a copy of all transition records.
func (m *Machine) History() []TransitionRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
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
	m.mu.Lock()
	listeners := make([]Listener, len(m.listeners))
	copy(listeners, m.listeners)
	m.mu.Unlock()
	for _, l := range listeners {
		l(evt)
	}
}

// CanTransition checks whether a transition to target is allowed.
func (m *Machine) CanTransition(target string) error {
	m.mu.Lock()
	current := m.stateField.String()
	hook, err := m.checkTransitionLocked(current, target)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	if hook != nil {
		return hook()
	}
	return nil
}

func (m *Machine) checkTransitionLocked(current, target string) (func() error, error) {
	allowed := m.wildcardSet[target]
	if !allowed {
		if dests, ok := m.transitionSet[current]; ok {
			allowed = dests[target]
		}
	}

	if !allowed {
		return nil, fmt.Errorf("transition from '%s' to '%s' not allowed", current, target)
	}

	if h, ok := m.hooks[target]; ok && h.can != nil {
		return h.can, nil
	}
	return nil, nil
}

// Transition moves the machine to the target state if allowed.
func (m *Machine) Transition(target string) error {
	return m.transitionInternal(target, "manual")
}

func (m *Machine) transitionInternal(target string, trigger string) error {
	m.mu.Lock()
	current := m.stateField.String()
	canHook, err := m.checkTransitionLocked(current, target)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	if canHook != nil {
		if err := canHook(); err != nil {
			return err
		}
	}

	m.emitEvent(BeforeTransition, current, target)

	m.mu.Lock()
	if actual := m.stateField.String(); actual != current {
		m.mu.Unlock()
		return fmt.Errorf("transition from '%s' to '%s' superseded by state '%s'", current, target, actual)
	}
	if _, err := m.checkTransitionLocked(current, target); err != nil {
		m.mu.Unlock()
		return err
	}
	exitHook := m.hooks[current].exit
	enterHook := m.hooks[target].enter

	m.stateField.SetString(target)
	m.lastStateTime = time.Now()
	m.history = append(m.history, TransitionRecord{
		From:      current,
		To:        target,
		Timestamp: time.Now(),
		Trigger:   trigger,
	})
	m.mu.Unlock()

	if current != "" {
		if exitHook != nil {
			exitHook()
		}
		m.emitEvent(ExitState, current, target)
	}

	if enterHook != nil {
		enterHook()
	}
	m.emitEvent(EnterState, current, target)
	m.emitEvent(AfterTransition, current, target)

	return nil
}

// CheckTimeouts transitions on timeout if a rule has expired.
func (m *Machine) CheckTimeouts() error {
	m.mu.Lock()
	current := m.stateField.String()
	elapsed := time.Since(m.lastStateTime)
	rule, exists := m.timeouts[current]
	m.mu.Unlock()

	if exists && elapsed > rule.Duration {
		return m.transitionInternal(rule.ToState, "timeout")
	}
	return nil
}

// GetStructure returns the initial state, transitions, and wildcards.
func (m *Machine) GetStructure() (string, map[string][]string, []string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	transitions := make(map[string][]string, len(m.transitions))
	for source, targets := range m.transitions {
		transitions[source] = append([]string(nil), targets...)
	}
	return m.initialState, transitions, append([]string(nil), m.wildcards...)
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

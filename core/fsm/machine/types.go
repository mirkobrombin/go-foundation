package machine

import "time"

// EventType categorises state machine lifecycle events.
type EventType int

const (
	// BeforeTransition fires before a state transition occurs.
	BeforeTransition EventType = iota
	// AfterTransition fires after a state transition completes.
	AfterTransition
	// EnterState fires when a state is entered.
	EnterState
	// ExitState fires when a state is exited.
	ExitState
)

// TransitionRecord stores a single state transition.
type TransitionRecord struct {
	From      string
	To        string
	Timestamp time.Time
	Trigger   string
	Metadata  map[string]any
}

// Listener is called when a state machine event occurs.
type Listener func(e Event)

// Event represents a state machine lifecycle event.
type Event struct {
	Type      EventType
	From      string
	To        string
	Timestamp time.Time
	Machine   *Machine
}

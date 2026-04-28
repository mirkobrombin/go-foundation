package machine

import "time"

// EventType categorises state machine lifecycle events.
type EventType int

const (
	BeforeTransition EventType = iota
	AfterTransition
	EnterState
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

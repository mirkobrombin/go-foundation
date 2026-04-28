package machine

import (
	"sync"
	"testing"
	"time"
)

type orderFSM struct {
	State string `fsm:"initial:draft; draft->confirmed; confirmed->paid; paid->shipped; *->cancelled"`
}

func (o *orderFSM) CanPaid() error { return nil }
func (o *orderFSM) OnEnterPaid()   {}

type guardFSM struct {
	State          string `fsm:"initial:draft; draft->paid; draft->archived"`
	canPaidCalled  bool
	guardShouldErr bool
}

func (g *guardFSM) CanPaid() error {
	g.canPaidCalled = true
	if g.guardShouldErr {
		return errGuard
	}
	return nil
}

type hookFSM struct {
	State        string `fsm:"initial:idle; idle->active; active->done"`
	onExitIdle   bool
	onEnterActive bool
	onExitActive  bool
	onEnterDone   bool
}

func (h *hookFSM) OnExitIdle()    { h.onExitIdle = true }
func (h *hookFSM) OnEnterActive() { h.onEnterActive = true }
func (h *hookFSM) OnExitActive()  { h.onExitActive = true }
func (h *hookFSM) OnEnterDone()   { h.onEnterDone = true }

type timeoutFSM struct {
	State string `fsm:"initial:pending; pending->expired [50ms]"`
}

var errGuard = errGuardType("guard rejected")

type errGuardType string

func (e errGuardType) Error() string { return string(e) }

func TestNew_ValidFSM(t *testing.T) {
	o := &orderFSM{}
	m, err := New(o)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if m.CurrentState() != "draft" {
		t.Errorf("state = %q, want %q", m.CurrentState(), "draft")
	}
}

func TestNew_NonPointer(t *testing.T) {
	_, err := New(orderFSM{})
	if err == nil {
		t.Error("expected error for non-pointer")
	}
}

func TestNew_NilPointer(t *testing.T) {
	_, err := New((*orderFSM)(nil))
	if err == nil {
		t.Error("expected error for nil pointer")
	}
}

func TestNew_NoFSMTag(t *testing.T) {
	type noTag struct {
		Name string
	}
	_, err := New(&noTag{})
	if err == nil {
		t.Error("expected error when no fsm tag found")
	}
}

func TestNew_NonStringField(t *testing.T) {
	type badField struct {
		State int `fsm:"initial:0"`
	}
	_, err := New(&badField{})
	if err == nil {
		t.Error("expected error for non-string fsm field")
	}
}

func TestTransition_Valid(t *testing.T) {
	o := &orderFSM{}
	m, _ := New(o)
	if err := m.Transition("confirmed"); err != nil {
		t.Fatalf("Transition failed: %v", err)
	}
	if m.CurrentState() != "confirmed" {
		t.Errorf("state = %q, want %q", m.CurrentState(), "confirmed")
	}
}

func TestTransition_Chained(t *testing.T) {
	o := &orderFSM{}
	m, _ := New(o)
	states := []string{"confirmed", "paid", "shipped"}
	for _, s := range states {
		if err := m.Transition(s); err != nil {
			t.Fatalf("Transition to %s failed: %v", s, err)
		}
	}
	if m.CurrentState() != "shipped" {
		t.Errorf("state = %q, want %q", m.CurrentState(), "shipped")
	}
}

func TestTransition_Invalid(t *testing.T) {
	o := &orderFSM{}
	m, _ := New(o)
	if err := m.Transition("shipped"); err == nil {
		t.Error("expected error for invalid transition draft->shipped")
	}
}

func TestTransition_Wildcard(t *testing.T) {
	o := &orderFSM{}
	m, _ := New(o)
	if err := m.Transition("cancelled"); err != nil {
		t.Fatalf("Transition through wildcard failed: %v", err)
	}
	if m.CurrentState() != "cancelled" {
		t.Errorf("state = %q, want %q", m.CurrentState(), "cancelled")
	}
}

func TestTransition_GuardAllowed(t *testing.T) {
	g := &guardFSM{}
	m, _ := New(g)
	if err := m.Transition("paid"); err != nil {
		t.Fatalf("Transition with guard failed: %v", err)
	}
	if !g.canPaidCalled {
		t.Error("guard CanPaid was not called")
	}
}

func TestTransition_GuardRejected(t *testing.T) {
	g := &guardFSM{guardShouldErr: true}
	m, _ := New(g)
	if err := m.Transition("paid"); err == nil {
		t.Fatal("expected error from guard")
	}
	if m.CurrentState() != "draft" {
		t.Errorf("state should remain 'draft', got %q", m.CurrentState())
	}
}

func TestTransition_HookOrder(t *testing.T) {
	h := &hookFSM{}
	m, _ := New(h)
	m.Transition("active")
	if !h.onExitIdle {
		t.Error("OnExitIdle not called")
	}
	if !h.onEnterActive {
		t.Error("OnEnterActive not called")
	}

	m.Transition("done")
	if !h.onExitActive {
		t.Error("OnExitActive not called")
	}
	if !h.onEnterDone {
		t.Error("OnEnterDone not called")
	}
}

func TestCurrentState_Concurrent(t *testing.T) {
	o := &orderFSM{}
	m, _ := New(o)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.CurrentState()
		}()
	}
	wg.Wait()
}

func TestSubscribe_ListenerCalled(t *testing.T) {
	o := &orderFSM{}
	m, _ := New(o)
	var events []EventType
	var mu sync.Mutex

	m.Subscribe(func(e Event) {
		mu.Lock()
		events = append(events, e.Type)
		mu.Unlock()
	})

	m.Transition("confirmed")

	mu.Lock()
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}
	if events[0] != BeforeTransition {
		t.Errorf("event[0] = %v, want BeforeTransition", events[0])
	}
	if events[3] != AfterTransition {
		t.Errorf("event[3] = %v, want AfterTransition", events[3])
	}
	mu.Unlock()
}

func TestHistory_RecordsTransitions(t *testing.T) {
	o := &orderFSM{}
	m, _ := New(o)
	m.Transition("confirmed")
	m.Transition("paid")

	history := m.History()
	if len(history) != 2 {
		t.Fatalf("expected 2 history records, got %d", len(history))
	}
	if history[0].From != "draft" || history[0].To != "confirmed" {
		t.Errorf("record[0] = %+v, want {draft -> confirmed}", history[0])
	}
	if history[1].From != "confirmed" || history[1].To != "paid" {
		t.Errorf("record[1] = %+v, want {confirmed -> paid}", history[1])
	}
}

func TestHistory_Trigger(t *testing.T) {
	o := &orderFSM{}
	m, _ := New(o)
	m.Transition("confirmed")
	history := m.History()
	if history[0].Trigger != "manual" {
		t.Errorf("trigger = %q, want %q", history[0].Trigger, "manual")
	}
}

func TestHistory_Immutable(t *testing.T) {
	o := &orderFSM{}
	m, _ := New(o)
	h := m.History()
	h = append(h, TransitionRecord{})
	if len(m.History()) != 0 {
		t.Error("History should not be modifiable by caller")
	}
}

func TestCheckTimeouts_Expired(t *testing.T) {
	tm := &timeoutFSM{}
	m, _ := New(tm)
	time.Sleep(60 * time.Millisecond)
	if err := m.CheckTimeouts(); err != nil {
		t.Fatalf("CheckTimeouts failed: %v", err)
	}
	if m.CurrentState() != "expired" {
		t.Errorf("state = %q, want %q", m.CurrentState(), "expired")
	}
}

func TestCheckTimeouts_NotExpired(t *testing.T) {
	tm := &timeoutFSM{}
	m, _ := New(tm)
	if err := m.CheckTimeouts(); err != nil {
		t.Fatalf("CheckTimeouts should not error immediately: %v", err)
	}
	if m.CurrentState() != "pending" {
		t.Errorf("state should remain 'pending', got %q", m.CurrentState())
	}
}

func TestCheckTimeouts_NoTimeoutRule(t *testing.T) {
	o := &orderFSM{}
	m, _ := New(o)
	if err := m.CheckTimeouts(); err != nil {
		t.Fatalf("CheckTimeouts with no rule: %v", err)
	}
}

func TestGetStructure(t *testing.T) {
	o := &orderFSM{}
	m, _ := New(o)
	initial, transitions, wildcards := m.GetStructure()
	if initial != "draft" {
		t.Errorf("initial = %q, want %q", initial, "draft")
	}
	if _, ok := transitions["draft"]; !ok {
		t.Error("expected transitions from draft")
	}
	if len(wildcards) == 0 {
		t.Error("expected wildcards")
	}
}

func TestConcurrentTransitions(t *testing.T) {
	o := &orderFSM{}
	m, _ := New(o)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.CurrentState()
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		m.Transition("confirmed")
	}()

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.History()
		}()
	}

	wg.Wait()
}

func TestSubscribe_Concurrent(t *testing.T) {
	o := &orderFSM{}
	m, _ := New(o)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Subscribe(func(e Event) {})
		}()
	}
	wg.Wait()
}

func TestDoubleWildcard(t *testing.T) {
	type doubleWild struct {
		State string `fsm:"*->cancelled; *->archived"`
	}
	d := &doubleWild{}
	m, err := New(d)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if err := m.Transition("cancelled"); err != nil {
		t.Fatalf("Transition to cancelled failed: %v", err)
	}
}

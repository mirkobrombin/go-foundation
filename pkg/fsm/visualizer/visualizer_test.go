package visualizer

import (
	"strings"
	"testing"
)

type testFSM struct {
	initial     string
	transitions map[string][]string
	wildcards   []string
}

func (t *testFSM) GetStructure() (string, map[string][]string, []string) {
	return t.initial, t.transitions, t.wildcards
}

func TestToMermaid_Basic(t *testing.T) {
	v := &testFSM{
		initial:     "draft",
		transitions: map[string][]string{"draft": {"paid"}, "paid": {"shipped"}},
	}
	out := ToMermaid(v)
	if !strings.Contains(out, "stateDiagram-v2") {
		t.Error("expected stateDiagram-v2 header")
	}
	if !strings.Contains(out, "[*] --> draft") {
		t.Error("expected initial state arrow")
	}
	if !strings.Contains(out, "draft --> paid") {
		t.Error("expected draft->paid transition")
	}
}

func TestToMermaid_NoInitial(t *testing.T) {
	v := &testFSM{
		transitions: map[string][]string{"a": {"b"}},
	}
	out := ToMermaid(v)
	if strings.Contains(out, "[*] -->") {
		t.Error("expected no initial state")
	}
}

func TestToMermaid_Wildcards(t *testing.T) {
	v := &testFSM{
		initial:   "active",
		wildcards: []string{"cancelled", "archived"},
	}
	out := ToMermaid(v)
	if !strings.Contains(out, "(Wildcard)") {
		t.Error("expected wildcard label")
	}
	if !strings.Contains(out, "cancelled") || !strings.Contains(out, "archived") {
		t.Error("expected both wildcard destinations")
	}
}

func TestToGraphviz_Basic(t *testing.T) {
	v := &testFSM{
		initial:     "start",
		transitions: map[string][]string{"start": {"end"}},
	}
	out := ToGraphviz(v)
	if !strings.Contains(out, "digraph FSM") {
		t.Error("expected graphviz header")
	}
	if !strings.Contains(out, "start [shape=point]") {
		t.Error("expected start node")
	}
	if !strings.Contains(out, "start -> end") {
		t.Error("expected start->end edge")
	}
}

func TestToGraphviz_Wildcards(t *testing.T) {
	v := &testFSM{
		wildcards: []string{"cancelled"},
	}
	out := ToGraphviz(v)
	if !strings.Contains(out, "ANY_STATE -> cancelled") {
		t.Error("expected wildcard edge from ANY_STATE")
	}
}

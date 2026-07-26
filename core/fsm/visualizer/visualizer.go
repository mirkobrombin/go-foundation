package visualizer

import (
	"fmt"
	"sort"
	"strings"
)

// VisualizerInterface is implemented by objects that can render their FSM structure.
type VisualizerInterface interface {
	GetStructure() (initial string, transitions map[string][]string, wildcards []string)
}

// ToMermaid renders the FSM as a Mermaid state diagram.
func ToMermaid(v VisualizerInterface) string {
	initial, transitions, wildcards := v.GetStructure()
	var sb strings.Builder
	sb.WriteString("stateDiagram-v2\n")

	if initial != "" {
		sb.WriteString(fmt.Sprintf("    [*] --> %s\n", initial))
	}

	var keys []string
	for k := range transitions {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, src := range keys {
		dsts := transitions[src]
		sort.Strings(dsts)
		for _, dst := range dsts {
			sb.WriteString(fmt.Sprintf("    %s --> %s\n", src, dst))
		}
	}

	if len(wildcards) > 0 {
		sort.Strings(wildcards)
		for _, dst := range wildcards {
			sb.WriteString(fmt.Sprintf("    [*] --> %s : (Wildcard)\n", dst))
		}
	}

	return sb.String()
}

// ToGraphviz renders the FSM as a Graphviz DOT digraph.
func ToGraphviz(v VisualizerInterface) string {
	initial, transitions, wildcards := v.GetStructure()
	var sb strings.Builder
	sb.WriteString("digraph FSM {\n")
	sb.WriteString("    rankdir=LR;\n")
	sb.WriteString("    node [shape=box style=rounded];\n")

	if initial != "" {
		sb.WriteString("    start [shape=point];\n")
		sb.WriteString(fmt.Sprintf("    start -> %s;\n", initial))
	}

	var keys []string
	for k := range transitions {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, src := range keys {
		dsts := transitions[src]
		sort.Strings(dsts)
		for _, dst := range dsts {
			sb.WriteString(fmt.Sprintf("    %s -> %s;\n", src, dst))
		}
	}

	if len(wildcards) > 0 {
		sort.Strings(wildcards)
		for _, dst := range wildcards {
			sb.WriteString(fmt.Sprintf("    ANY_STATE -> %s [label=\"*\"];\n", dst))
		}
	}

	sb.WriteString("}\n")
	return sb.String()
}

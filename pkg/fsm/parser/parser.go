package parser

import (
	"fmt"
	"strings"
	"time"

	"github.com/mirkobrombin/go-foundation/pkg/tags"
)

// Config holds the parsed FSM definition from a tag.
type Config struct {
	InitialState string
	Transitions  map[string][]string
	Wildcards    []string
	Timeouts     map[string]TimeoutRule
}

// TimeoutRule defines an automatic transition after a duration.
type TimeoutRule struct {
	FromState string
	ToState   string
	Duration  time.Duration
}

var tagParser = tags.NewParser("fsm")

// Parse parses an FSM tag string into a Config.
func Parse(tag string) (*Config, error) {
	cfg := &Config{
		Transitions: make(map[string][]string),
		Timeouts:    make(map[string]TimeoutRule),
	}

	parsed := tagParser.Parse(tag)

	if initial, ok := parsed["initial"]; ok && len(initial) > 0 {
		cfg.InitialState = initial[0]
	}

	for key := range parsed {
		if key == "initial" {
			continue
		}

		part := key
		var timeoutDuration time.Duration

		if startBracket := strings.Index(part, "["); startBracket != -1 {
			endBracket := strings.Index(part, "]")
			if endBracket > startBracket {
				durStr := part[startBracket+1 : endBracket]
				var err error
				timeoutDuration, err = time.ParseDuration(durStr)
				if err != nil {
					return nil, fmt.Errorf("invalid timeout duration: %s", durStr)
				}
				part = strings.TrimSpace(part[:startBracket])
			}
		}

		before, after, ok := strings.Cut(part, "->")
		if !ok {
			continue
		}

		src := strings.TrimSpace(before)
		dst := strings.TrimSpace(after)

		if src == "*" {
			cfg.Wildcards = append(cfg.Wildcards, dst)
		} else {
			cfg.Transitions[src] = append(cfg.Transitions[src], dst)
		}

		if timeoutDuration > 0 {
			cfg.Timeouts[src] = TimeoutRule{
				FromState: src,
				ToState:   dst,
				Duration:  timeoutDuration,
			}
		}
	}

	return cfg, nil
}

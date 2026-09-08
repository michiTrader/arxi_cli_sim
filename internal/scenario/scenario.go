// Package scenario loads a recorded conversation: an NDJSON file where each line
// is either a delayed event or a barrier that waits for the human.
//
//	{"after_ms": 170, "event": {…}}   play the event 170 ms after the previous one
//	{"await": "inbox-1"}             stop until that inbox is answered for real
//	{"await": "prompt"}              stop until the human types a new prompt
//
// The wrapper fields are the only thing that is not arxi's own format, so
// `jq -c .event` on a scenario yields a legitimate run log.
package scenario

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"arxi.local/sim/internal/event"
)

// Epoch is the wall clock a scenario pretends to start at. Fixed so that
// timestamps are reproducible and golden files stay stable.
var Epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// AwaitPrompt is the barrier that hands the input bar back to the human.
const AwaitPrompt = "prompt"

// Step is one line of a scenario file. Exactly one of Event and Await is set.
type Step struct {
	Line    int           // 1-based line number, for error messages
	AfterMS int           // delay from the previous step
	Event   *event.Event  // nil on a barrier
	Await   string        // "" unless this step is a barrier
	At      time.Duration // cumulative offset from the start of the run
}

// IsBarrier reports whether the player must stop here and wait for a human.
func (s Step) IsBarrier() bool { return s.Await != "" }

// Scenario is a whole file, in play order.
type Scenario struct {
	Path  string
	Steps []Step
}

// Duration is the simulated time from the first step to the last one.
func (s *Scenario) Duration() time.Duration {
	if len(s.Steps) == 0 {
		return 0
	}
	return s.Steps[len(s.Steps)-1].At
}

// rawStep is the on-disk shape. Pointers distinguish "absent" from "zero", and
// unknown fields are rejected so a typo fails loudly instead of playing silently.
type rawStep struct {
	AfterMS *int         `json:"after_ms"`
	Event   *event.Event `json:"event"`
	Await   *string      `json:"await"`
}

// Load reads and validates a scenario. Every problem in the file is reported,
// not just the first one.
func Load(path string) (*Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sc := &Scenario{Path: path}
	var problems []error
	var elapsed time.Duration

	sn := bufio.NewScanner(bytes.NewReader(data))
	sn.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for line := 1; sn.Scan(); line++ {
		text := strings.TrimSpace(sn.Text())
		if text == "" {
			problems = append(problems, fmt.Errorf("line %d: blank line", line))
			continue
		}
		var raw rawStep
		dec := json.NewDecoder(strings.NewReader(text))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&raw); err != nil {
			problems = append(problems, fmt.Errorf("line %d: %w", line, err))
			continue
		}
		step := Step{Line: line}
		switch {
		case raw.Await != nil:
			if raw.AfterMS != nil || raw.Event != nil {
				problems = append(problems, fmt.Errorf("line %d: a barrier carries only \"await\"", line))
				continue
			}
			step.Await = *raw.Await
		case raw.Event != nil:
			if raw.AfterMS == nil {
				problems = append(problems, fmt.Errorf("line %d: event without \"after_ms\"", line))
				continue
			}
			if *raw.AfterMS < 0 {
				problems = append(problems, fmt.Errorf("line %d: after_ms must not be negative", line))
			}
			step.AfterMS = *raw.AfterMS
			elapsed += time.Duration(step.AfterMS) * time.Millisecond
			ev := *raw.Event
			ev.TS = Epoch.Add(elapsed).Format("2006-01-02T15:04:05.000Z")
			step.Event = &ev
		default:
			problems = append(problems, fmt.Errorf("line %d: expected \"await\" or \"after_ms\"+\"event\"", line))
			continue
		}
		step.At = elapsed
		sc.Steps = append(sc.Steps, step)
	}
	if err := sn.Err(); err != nil {
		return nil, err
	}
	problems = append(problems, Validate(sc)...)
	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}
	return sc, nil
}

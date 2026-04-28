package logger

import (
	"encoding/json"
	"io"
	"os"
	"time"
)

// clefLevel maps internal level names to CLEF level strings.
// Information is omitted per the CLEF specification.
var clefLevel = map[string]string{
	"debug": "Debug",
	"warn":  "Warning",
	"error": "Error",
}

// clefEntry is the JSON structure for a Compact Log Event Format (CLEF) line.
// Fields are flattened at root level; @l is omitted for Information.
type clefEntry struct {
	Timestamp time.Time `json:"@t"`
	Level     string    `json:"@l,omitempty"`
	Message   string    `json:"@m"`
}

// CLEFSink writes log entries as CLEF-compatible JSON lines.
// Each line is a single JSON object with @t, @l (omitted for Information),
// @m, and all structured fields flattened at the root level, matching the
// format emitted by Serilog's RenderedCompactJsonFormatter, so that Go and
// C# service logs are queryable with the same field selectors in Grafana/Loki.
type CLEFSink struct {
	w io.Writer
}

// NewCLEFSink constructs a CLEFSink. When w is nil, os.Stdout is used.
func NewCLEFSink(w io.Writer) *CLEFSink {
	if w == nil {
		w = os.Stdout
	}
	return &CLEFSink{w: w}
}

// Log writes e as a CLEF JSON line to the underlying writer.
func (c *CLEFSink) Log(e Entry) error {
	// Build a flat map so structured fields sit at the root level.
	m := make(map[string]any, len(e.Fields)+3)
	for k, v := range e.Fields {
		m[k] = v
	}

	m["@t"] = e.Time.UTC().Format(time.RFC3339Nano)
	m["@m"] = e.Msg

	// @l is omitted for Information per the CLEF spec.
	if lvl, ok := clefLevel[e.Level]; ok {
		m["@l"] = lvl
	}

	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = c.w.Write(append(b, '\n'))
	return err
}

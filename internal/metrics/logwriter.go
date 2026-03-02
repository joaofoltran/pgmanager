package metrics

import (
	"encoding/json"
	"io"
	"time"
)

// LogWriter implements io.Writer for zerolog, routing log entries into the Collector.
// When used as the zerolog output, logs appear in the TUI log panel
// instead of leaking to stderr behind the alt screen.
type LogWriter struct {
	collector *Collector
}

// NewLogWriter creates a LogWriter that feeds into the given Collector.
// The collector may be nil and set later via SetCollector.
func NewLogWriter(c *Collector) *LogWriter {
	return &LogWriter{collector: c}
}

// SetCollector changes the target Collector. Use this when the Collector
// is not available at construction time (e.g. it lives inside a Pipeline
// that is created after the logger).
func (w *LogWriter) SetCollector(c *Collector) {
	w.collector = c
}

func (w *LogWriter) Write(p []byte) (int, error) {
	if w.collector == nil {
		return len(p), nil
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(p, &raw); err != nil {
		w.collector.AddLog(LogEntry{
			Time:    time.Now(),
			Level:   "info",
			Message: string(p),
		})
		return len(p), nil
	}

	entry := LogEntry{
		Time:   time.Now(),
		Fields: make(map[string]string),
	}

	if lvl, ok := raw["level"].(string); ok {
		entry.Level = lvl
	}
	if msg, ok := raw["message"].(string); ok {
		entry.Message = msg
	}
	if t, ok := raw["time"].(string); ok {
		if parsed, err := time.Parse(time.RFC3339, t); err == nil {
			entry.Time = parsed
		}
	}

	for k, v := range raw {
		switch k {
		case "level", "message", "time":
			continue
		default:
			if s, ok := v.(string); ok {
				entry.Fields[k] = s
			}
		}
	}

	w.collector.AddLog(entry)
	return len(p), nil
}

var _ io.Writer = (*LogWriter)(nil)

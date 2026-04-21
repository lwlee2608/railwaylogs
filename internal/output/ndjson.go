package output

import (
	"encoding/json"
	"io"
	"strings"
	"sync"
)

// LogLine matches the shape returned by DeploymentLogs.
type LogLine struct {
	Timestamp  string
	Message    string
	Attributes []Attribute
}

type Attribute struct {
	Key   string
	Value string
}

// Writer emits NDJSON — one JSON object per line — so humanlog can auto-detect.
type Writer struct {
	mu  sync.Mutex
	out io.Writer
}

func NewWriter(w io.Writer) *Writer {
	return &Writer{out: w}
}

func (w *Writer) Write(line LogLine) error {
	obj := make(map[string]any, 2+len(line.Attributes))
	obj["timestamp"] = line.Timestamp
	obj["message"] = line.Message
	for _, a := range line.Attributes {
		obj[a.Key] = decodeAttrValue(a.Value)
	}

	buf, err := json.Marshal(obj)
	if err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := w.out.Write(buf); err != nil {
		return err
	}
	_, err = w.out.Write([]byte{'\n'})
	return err
}

// decodeAttrValue unwraps JSON-encoded attribute values (Railway stores them as
// JSON strings like `"info"`), falling back to the raw string.
func decodeAttrValue(v string) any {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return v
	}
	var parsed any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
		return parsed
	}
	return v
}

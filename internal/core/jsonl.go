package core

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Field names are literal top-level keys, not paths or expressions.
type JSONLFields struct {
	Message string `json:"message,omitempty"`
	Level   string `json:"level,omitempty"`
	Time    string `json:"time,omitempty"`
}

func parseJSONLine(raw string, reference time.Time, fields JSONLFields) (*Entry, error) {
	e := ParseLine(raw, reference)
	var object map[string]json.RawMessage
	// Scalars and arrays are valid JSONL too; preserve them as plain text.
	if json.Unmarshal([]byte(raw), &object) != nil {
		if !json.Valid([]byte(raw)) {
			return e, fmt.Errorf("JSON 无效，已保留原文")
		}
		return e, nil
	}
	if object == nil {
		return e, nil
	}
	// Require the recording signature, not merely successful unmarshalling:
	// unknown business JSON keys would otherwise silently become an empty Entry.
	if object["seq"] != nil && object["received"] != nil && object["raw"] != nil && object["message"] != nil {
		var recorded Entry
		if json.Unmarshal([]byte(raw), &recorded) == nil && recorded.Seq > 0 && !recorded.Received.IsZero() && string(object["raw"]) != "null" && string(object["message"]) != "null" {
			recorded.RecordedSeq = recorded.Seq
			return &recorded, nil
		}
	}
	value := func(explicit string, aliases ...string) json.RawMessage {
		if explicit != "" {
			return object[explicit]
		}
		for _, name := range aliases {
			if v := object[name]; len(v) > 0 && string(v) != "null" {
				return v
			}
		}
		return nil
	}
	if v := value(fields.Message, "message", "msg", "log", "text"); len(v) > 0 && string(v) != "null" {
		var message string
		if json.Unmarshal(v, &message) == nil {
			e.Message = message
		} else {
			e.Message = string(v) // Keep objects and large numbers losslessly.
		}
	}
	var level string
	if json.Unmarshal(value(fields.Level, "level", "severity", "log_level"), &level) == nil {
		if n := LevelValue(level); n > 0 {
			e.Level = []string{"", "V", "D", "I", "W", "E", "F"}[n]
		}
	}
	if v := value(fields.Time, "time", "timestamp", "@timestamp", "ts"); len(v) > 0 {
		e.Time = jsonLogTime(v)
	}
	// Raw always remains the full original JSON, including unrecognized keys.
	return e, nil
}

func jsonLogTime(raw json.RawMessage) time.Time {
	value := string(raw)
	if strings.HasPrefix(value, `"`) {
		if json.Unmarshal(raw, &value) != nil {
			return time.Time{}
		}
		if parsed, err := ParseTime(value); err == nil {
			return parsed
		}
	}
	// Integers use Unix seconds / milliseconds / microseconds / nanoseconds.
	// Avoid float64 so nanoseconds and identifiers never lose precision.
	if n, err := strconv.ParseInt(value, 10, 64); err == nil {
		switch {
		case n <= -1e17 || n >= 1e17:
			return time.Unix(n/1e9, n%1e9)
		case n <= -1e14 || n >= 1e14:
			return time.Unix(n/1e6, n%1e6*1e3)
		case n <= -1e11 || n >= 1e11:
			return time.Unix(n/1e3, n%1e3*1e6)
		default:
			return time.Unix(n, 0)
		}
	}
	// Decimal Unix seconds, with at most nanosecond precision.
	whole, fraction, ok := strings.Cut(value, ".")
	if !ok || len(fraction) == 0 || len(fraction) > 9 {
		return time.Time{}
	}
	for _, digit := range fraction {
		if digit < '0' || digit > '9' {
			return time.Time{}
		}
	}
	seconds, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return time.Time{}
	}
	nanos, _ := strconv.ParseInt(fraction+strings.Repeat("0", 9-len(fraction)), 10, 64)
	if strings.HasPrefix(whole, "-") {
		nanos = -nanos
	}
	return time.Unix(seconds, nanos)
}

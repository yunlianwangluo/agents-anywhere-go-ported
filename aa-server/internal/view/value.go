// Package view renders domain values into the exact wire shapes the mobile
// client decodes. It holds no state and performs no I/O, so both the router and
// business layers can format responses through it without coupling.
package view

import (
	"fmt"
	"time"
)

// Now formats the current instant the way the client expects.
func Now() string { return time.Now().UTC().Format(time.RFC3339) }

// startedAt keeps runtime facts stable per process: clients hash these views, so
// a value that changes on every poll makes the runtime look modified.
var startedAt = time.Now().UTC().Format(time.RFC3339)

// StableNow returns a per-process timestamp for values the client hashes.
func StableNow() string { return startedAt }

// Timestamp renders a stored time, falling back to now when it was never set.
func Timestamp(value time.Time) string {
	if value.IsZero() {
		return Now()
	}
	return value.UTC().Format(time.RFC3339)
}

// StringValue reads a string out of a decoded JSON value.
func StringValue(value any) string { result, _ := value.(string); return result }

// MapValue reads an object out of a decoded JSON value.
func MapValue(value any) map[string]any { result, _ := value.(map[string]any); return result }

// NumberValue reads a number out of a decoded JSON value.
func NumberValue(value any) float64 { result, _ := value.(float64); return result }

// FirstNonEmpty returns the first non-empty string.
func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// ProjectID derives the synthetic id for a connector directory that has no
// stored project yet.
func ProjectID(connectorID, cwd string) string {
	return "project-" + connectorID + "-" + fmt.Sprintf("%x", []byte(cwd))
}

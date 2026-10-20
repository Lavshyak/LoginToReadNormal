package models

import "time"

// TimeLayout is the single on-disk timestamp format: RFC3339 in UTC.
// All stored values use the "...Z" suffix and a fixed width, so lexical
// comparison in SQLite matches chronological order.
const TimeLayout = time.RFC3339

// FormatUTC renders a time as a UTC RFC3339 string.
func FormatUTC(t time.Time) string { return t.UTC().Format(TimeLayout) }

// ParseUTC parses a stored RFC3339 string into a UTC time.
func ParseUTC(s string) (time.Time, error) { return time.Parse(TimeLayout, s) }

// FormatDisplay renders a time for the UI (always UTC, as required).
func FormatDisplay(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05") }

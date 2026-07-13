package config

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// PresetDateExpression is the deliberately small date arithmetic supported in
// preset values. Offset is signed; calendar units use AddDate while clock
// units use time.Duration.
type PresetDateExpression struct {
	Base   string
	Offset int
	Unit   string
}

var presetDateExpressionPattern = regexp.MustCompile(`^(now|today)\s*([+-])\s*(\d+)\s*(min|mo|m|h|d|w)$`)

// ParsePresetDateExpression parses one optional-offset expression. The bool
// reports whether the value looks like a date expression; ordinary variables
// such as {{board}} return false and nil so callers can validate them normally.
func ParsePresetDateExpression(value string) (PresetDateExpression, bool, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "now") && !strings.HasPrefix(value, "today") {
		return PresetDateExpression{}, false, nil
	}

	match := presetDateExpressionPattern.FindStringSubmatch(value)
	if match == nil {
		return PresetDateExpression{}, true, fmt.Errorf("expected {{now+2h}}, {{today-3d}}, or another single offset")
	}
	offset, err := strconv.Atoi(match[2] + match[3])
	if err != nil {
		return PresetDateExpression{}, true, fmt.Errorf("offset is out of range")
	}
	return PresetDateExpression{Base: match[1], Offset: offset, Unit: match[4]}, true, nil
}

// Evaluate resolves the expression using now as the evaluation instant.
// today preserves a date-only result; now preserves an RFC3339 timestamp.
func (e PresetDateExpression) Evaluate(now time.Time) (string, error) {
	value := now
	if e.Base == "today" {
		value = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	}

	switch e.Unit {
	case "m", "min":
		value = value.Add(time.Duration(e.Offset) * time.Minute)
	case "h":
		value = value.Add(time.Duration(e.Offset) * time.Hour)
	case "d":
		value = value.AddDate(0, 0, e.Offset)
	case "w":
		value = value.AddDate(0, 0, 7*e.Offset)
	case "mo":
		value = value.AddDate(0, e.Offset, 0)
	default:
		return "", fmt.Errorf("unsupported date unit %q", e.Unit)
	}

	if e.Base == "today" {
		return value.Format(time.DateOnly), nil
	}
	return value.Format(time.RFC3339), nil
}

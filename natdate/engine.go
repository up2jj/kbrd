package natdate

import (
	"strconv"
	"strings"
	"time"
)

// tokenize lowercases the input and splits it into punctuation-stripped tokens.
// Trimming only the ends keeps clock tokens like "19:09" intact.
func tokenize(s string) []string {
	s = strings.ToLower(strings.TrimSpace(s))
	fields := strings.Fields(s)
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.Trim(f, ".,;:!?()[]{}\"'")
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// parseTokens scans tokens left-to-right, accumulating at most one date clause and
// one time-of-day clause (so "wednesday at 19:09" combines). A second date or time
// clause is ErrAmbiguous; an unrecognized token is ErrUnparseable (fail closed).
func parseTokens(tokens []string, now time.Time, lex *merged, ws time.Weekday) (time.Time, error) {
	var (
		dateDay   time.Time
		haveDate  bool
		hour, min int
		haveTime  bool
	)
	setDate := func(d time.Time) error {
		if haveDate {
			return ErrAmbiguous
		}
		dateDay, haveDate = d, true
		return nil
	}
	setTime := func(h, m int) error {
		if haveTime {
			return ErrAmbiguous
		}
		hour, min, haveTime = h, m, true
		return nil
	}

	i := 0
	for i < len(tokens) {
		tok := tokens[i]

		// Standalone day anchor: today / tomorrow / yesterday / jutro / wczoraj …
		if off, ok := lex.keywords[tok]; ok {
			if err := setDate(dateOnly(now).AddDate(0, 0, off)); err != nil {
				return time.Time{}, err
			}
			i++
			continue
		}

		// Bare weekday → the upcoming occurrence (today counts).
		if wd, ok := lex.weekdays[tok]; ok {
			if err := setDate(resolveWeekday(now, wd, roleThis, ws)); err != nil {
				return time.Time{}, err
			}
			i++
			continue
		}

		// Bare clock without a prefix, e.g. "19:09".
		if h, m, ok := parseClock(tok); ok {
			if err := setTime(h, m); err != nil {
				return time.Time{}, err
			}
			i++
			continue
		}

		// Absolute date literal: ISO "2026-06-24" / "2026/06/24" or European
		// "24.06.2026" / "24.06". Language-agnostic, so handled here, and it
		// composes with a time clause ("2026-06-24 at 19:09").
		if d, ok := parseDateLiteral(tok, now); ok {
			if err := setDate(d); err != nil {
				return time.Time{}, err
			}
			i++
			continue
		}

		// Function word: in/at/next/this/last (and Polish equivalents).
		if r, ok := lex.prefixes[tok]; ok {
			consumed, err := applyPrefix(r, tokens, i, now, lex, ws, setDate, setTime)
			if err != nil {
				return time.Time{}, err
			}
			i += consumed
			continue
		}

		// Number-led offset: "N <unit> [ago|temu|from now]".
		if n, ok := parseInt(tok); ok {
			consumed, err := applyNumber(n, tokens, i, now, lex, setDate)
			if err != nil {
				return time.Time{}, err
			}
			i += consumed
			continue
		}

		return time.Time{}, ErrUnparseable
	}

	if !haveDate && !haveTime {
		return time.Time{}, ErrUnparseable
	}

	day := dateDay
	if !haveDate {
		day = now // time-only phrase resolves to today
	}
	h, m, sec, nsec := now.Hour(), now.Minute(), now.Second(), now.Nanosecond()
	if haveTime {
		h, m, sec, nsec = hour, min, 0, 0
	}
	return time.Date(day.Year(), day.Month(), day.Day(), h, m, sec, nsec, now.Location()), nil
}

// applyPrefix handles a function word and the token(s) it governs, returning how
// many tokens were consumed.
func applyPrefix(r role, tokens []string, i int, now time.Time, lex *merged, ws time.Weekday,
	setDate func(time.Time) error, setTime func(int, int) error) (int, error) {

	switch r {
	case roleAt: // "at"/"o" <clock>
		if i+1 >= len(tokens) {
			return 0, ErrUnparseable
		}
		h, m, ok := parseClock(tokens[i+1])
		if !ok {
			return 0, ErrUnparseable
		}
		return 2, setTime(h, m)

	case roleIn: // "in"/"za" <number> <unit>
		if i+2 >= len(tokens) {
			return 0, ErrUnparseable
		}
		n, ok := parseInt(tokens[i+1])
		if !ok {
			return 0, ErrUnparseable
		}
		u, ok := lex.units[tokens[i+2]]
		if !ok {
			return 0, ErrUnparseable
		}
		return 3, setDate(addUnit(dateOnly(now), u, n))

	case roleNext, roleLast: // "next"/"last" <weekday|unit>
		if i+1 >= len(tokens) {
			return 0, ErrUnparseable
		}
		nxt := tokens[i+1]
		if wd, ok := lex.weekdays[nxt]; ok {
			return 2, setDate(resolveWeekday(now, wd, r, ws))
		}
		if u, ok := lex.units[nxt]; ok {
			return 2, setDate(resolvePeriod(now, u, r, ws))
		}
		return 0, ErrUnparseable

	case roleThis: // "this"/"w"/"we" <weekday|unit>, incl. Polish "w przyszłym/zeszłym/tym <unit>"
		if i+1 >= len(tokens) {
			return 0, ErrUnparseable
		}
		nxt := tokens[i+1]
		// Stacked prefix: "w przyszłym tygodniu" / "w zeszłym roku" / "w tym tygodniu".
		if r2, ok := lex.prefixes[nxt]; ok && (r2 == roleNext || r2 == roleLast || r2 == roleThis) {
			if i+2 >= len(tokens) {
				return 0, ErrUnparseable
			}
			u, ok := lex.units[tokens[i+2]]
			if !ok {
				return 0, ErrUnparseable
			}
			return 3, setDate(resolvePeriod(now, u, r2, ws))
		}
		if wd, ok := lex.weekdays[nxt]; ok {
			return 2, setDate(resolveWeekday(now, wd, roleThis, ws))
		}
		if u, ok := lex.units[nxt]; ok {
			return 2, setDate(resolvePeriod(now, u, roleThis, ws))
		}
		return 0, ErrUnparseable
	}
	return 0, ErrUnparseable
}

// applyNumber handles "N <unit>" optionally followed by a past tail ("ago"/"temu")
// or "from now", returning how many tokens were consumed.
func applyNumber(n int, tokens []string, i int, now time.Time, lex *merged,
	setDate func(time.Time) error) (int, error) {

	if i+1 >= len(tokens) {
		return 0, ErrUnparseable
	}
	u, ok := lex.units[tokens[i+1]]
	if !ok {
		return 0, ErrUnparseable
	}
	sign, consumed := 1, 2
	if i+2 < len(tokens) {
		switch {
		case lex.pastTails[tokens[i+2]]: // "2 weeks ago", "2 tygodnie temu"
			sign, consumed = -1, 3
		case tokens[i+2] == "from" && i+3 < len(tokens) && tokens[i+3] == "now":
			sign, consumed = 1, 4
		}
	}
	return consumed, setDate(addUnit(dateOnly(now), u, sign*n))
}

// parseInt accepts an unsigned base-10 integer; negative offsets are expressed by
// keywords/tails, not by a sign here.
func parseInt(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// parseClock recognizes "HH:MM", "H:MM" (24-hour) and am/pm forms "7pm", "7:30am",
// "12am"→00:00, "12pm"→12:00. A bare hour without am/pm is rejected to avoid
// treating an arbitrary number as a time.
func parseClock(s string) (hour, minute int, ok bool) {
	ampm := ""
	switch {
	case strings.HasSuffix(s, "am"):
		ampm, s = "am", strings.TrimSuffix(s, "am")
	case strings.HasSuffix(s, "pm"):
		ampm, s = "pm", strings.TrimSuffix(s, "pm")
	}

	if strings.Contains(s, ":") {
		parts := strings.SplitN(s, ":", 2)
		h, err1 := strconv.Atoi(parts[0])
		m, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil {
			return 0, 0, false
		}
		hour, minute = h, m
	} else {
		if ampm == "" {
			return 0, 0, false // bare number is not a clock
		}
		h, err := strconv.Atoi(s)
		if err != nil {
			return 0, 0, false
		}
		hour, minute = h, 0
	}

	if ampm != "" {
		if hour < 1 || hour > 12 {
			return 0, 0, false
		}
		if ampm == "pm" && hour != 12 {
			hour += 12
		}
		if ampm == "am" && hour == 12 {
			hour = 0
		}
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, false
	}
	return hour, minute, true
}

// parseDateLiteral recognizes absolute dates: ISO "YYYY-MM-DD" / "YYYY/MM/DD"
// (4-digit year first) and European "DD.MM.YYYY" / "DD.MM" (day first, year last
// or the current year). The day-first/year-first split per separator keeps the
// forms unambiguous; ambiguous slash forms like "06/24" are intentionally not
// accepted. Returns the date at midnight in now's location.
func parseDateLiteral(tok string, now time.Time) (time.Time, bool) {
	if p := strings.Split(tok, "-"); len(p) == 3 {
		return makeDate(p[0], p[1], p[2], now.Location()) // YYYY-MM-DD
	}
	if p := strings.Split(tok, "/"); len(p) == 3 {
		return makeDate(p[0], p[1], p[2], now.Location()) // YYYY/MM/DD
	}
	if p := strings.Split(tok, "."); len(p) == 3 {
		return makeDate(p[2], p[1], p[0], now.Location()) // DD.MM.YYYY
	}
	if p := strings.Split(tok, "."); len(p) == 2 {
		return makeDate(strconv.Itoa(now.Year()), p[1], p[0], now.Location()) // DD.MM
	}
	return time.Time{}, false
}

// makeDate parses year/month/day strings and validates the calendar date by
// round-tripping through time.Date (so Feb 30 or month 13 are rejected, not
// silently normalized).
func makeDate(ys, ms, ds string, loc *time.Location) (time.Time, bool) {
	y, ok1 := parseInt(ys)
	mo, ok2 := parseInt(ms)
	d, ok3 := parseInt(ds)
	if !ok1 || !ok2 || !ok3 || len(ys) != 4 || mo < 1 || mo > 12 || d < 1 || d > 31 {
		return time.Time{}, false
	}
	t := time.Date(y, time.Month(mo), d, 0, 0, 0, 0, loc)
	if t.Year() != y || t.Month() != time.Month(mo) || t.Day() != d {
		return time.Time{}, false
	}
	return t, true
}

// dateOnly returns t's calendar day at midnight in t's location.
func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// addUnit adds n units (which may be negative) to t using calendar arithmetic.
func addUnit(t time.Time, u Unit, n int) time.Time {
	switch u {
	case UnitDay:
		return t.AddDate(0, 0, n)
	case UnitWeek:
		return t.AddDate(0, 0, 7*n)
	case UnitMonth:
		return t.AddDate(0, n, 0)
	case UnitYear:
		return t.AddDate(n, 0, 0)
	}
	return t
}

// daysUntil is the number of days to advance from weekday `from` to reach `target`,
// in [0,6] (0 means today).
func daysUntil(from, target time.Weekday) int {
	return int((target - from + 7) % 7)
}

// weekBucket returns the date of the week-start day for the week containing t.
func weekBucket(t time.Time, ws time.Weekday) time.Time {
	back := int((t.Weekday() - ws + 7) % 7)
	return dateOnly(t).AddDate(0, 0, -back)
}

// resolveWeekday maps a weekday + role to a date. Bare/this → upcoming occurrence
// (today counts); next → the occurrence in the following week bucket; last → the
// most recent past occurrence (strictly before today).
func resolveWeekday(now time.Time, target time.Weekday, r role, ws time.Weekday) time.Time {
	if r == roleLast {
		back := int((now.Weekday() - target + 7) % 7)
		if back == 0 {
			back = 7
		}
		return dateOnly(now).AddDate(0, 0, -back)
	}
	day := dateOnly(now).AddDate(0, 0, daysUntil(now.Weekday(), target))
	if r == roleNext && weekBucket(day, ws).Equal(weekBucket(now, ws)) {
		day = day.AddDate(0, 0, 7)
	}
	return day
}

// resolvePeriod maps a period unit + role to the start date of that period
// (start-of-week bucket, first of month, first of year). Day acts as today/±1.
func resolvePeriod(now time.Time, u Unit, r role, ws time.Weekday) time.Time {
	step := 0
	switch r {
	case roleNext:
		step = 1
	case roleLast:
		step = -1
	}
	switch u {
	case UnitDay:
		return dateOnly(now).AddDate(0, 0, step)
	case UnitWeek:
		return weekBucket(now, ws).AddDate(0, 0, 7*step)
	case UnitMonth:
		first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		return first.AddDate(0, step, 0)
	case UnitYear:
		first := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, now.Location())
		return first.AddDate(step, 0, 0)
	}
	return dateOnly(now)
}

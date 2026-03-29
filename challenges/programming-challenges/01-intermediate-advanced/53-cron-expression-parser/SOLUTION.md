# 53. Cron Expression Parser -- Solution

## Architecture Overview

The solution follows a three-layer pipeline:

1. **Tokenizer/Parser** -- splits the expression string into fields, parses each field into a set of allowed values
2. **Expression Engine** -- holds the parsed fields and implements matching and next-occurrence logic
3. **Scheduler Interface** -- public API for `Next`, `NextN`, `Matches`, and validation

```
"*/5 * 1-15 * MON-FRI"
    |
    v
[Tokenize + Split Fields]
    |
    v
[Parse Each Field -> Bitset of Allowed Values]
    |
    v
CronExpr { second, minute, hour, dayOfMonth, month, dayOfWeek, flags }
    |
    v
Next(from) / Matches(t) / NextN(from, n)
```

## Complete Solution (Go)

### cron.go

```go
package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type FieldType int

const (
	FieldSecond FieldType = iota
	FieldMinute
	FieldHour
	FieldDayOfMonth
	FieldMonth
	FieldDayOfWeek
)

var fieldBounds = map[FieldType][2]int{
	FieldSecond:     {0, 59},
	FieldMinute:     {0, 59},
	FieldHour:       {0, 23},
	FieldDayOfMonth: {1, 31},
	FieldMonth:      {1, 12},
	FieldDayOfWeek:  {0, 6},
}

var monthNames = map[string]int{
	"JAN": 1, "FEB": 2, "MAR": 3, "APR": 4, "MAY": 5, "JUN": 6,
	"JUL": 7, "AUG": 8, "SEP": 9, "OCT": 10, "NOV": 11, "DEC": 12,
}

var dayNames = map[string]int{
	"SUN": 0, "MON": 1, "TUE": 2, "WED": 3, "THU": 4, "FRI": 5, "SAT": 6,
}

var macros = map[string]string{
	"@yearly":  "0 0 1 1 *",
	"@monthly": "0 0 1 * *",
	"@weekly":  "0 0 * * 0",
	"@daily":   "0 0 * * *",
	"@hourly":  "0 * * * *",
}

type CronExpr struct {
	Second     Bitset
	Minute     Bitset
	Hour       Bitset
	DayOfMonth Bitset
	Month      Bitset
	DayOfWeek  Bitset

	HasSeconds   bool
	DOMWildcard  bool
	DOWWildcard  bool
	LastDOM      bool       // L flag for day-of-month
	WeekdayDOM   int        // W flag: day number, -1 if not set
	LastDOW      int        // nL: last nth weekday, -1 if not set
	NthDOW       [2]int     // n#m: [weekday, nth], [-1,-1] if not set
	Location     *time.Location
}

type Bitset [64]bool

func (b *Bitset) Set(v int)        { b[v] = true }
func (b *Bitset) Has(v int) bool   { return v >= 0 && v < 64 && b[v] }

func (b *Bitset) NextFrom(v, max int) (int, bool) {
	for i := v; i <= max; i++ {
		if b[i] {
			return i, true
		}
	}
	return 0, false
}

func (b *Bitset) Min(lo, hi int) (int, bool) {
	for i := lo; i <= hi; i++ {
		if b[i] {
			return i, true
		}
	}
	return 0, false
}

type ParseError struct {
	Field   FieldType
	Index   int
	Token   string
	Reason  string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("field %d (%s): %s", e.Index, e.Token, e.Reason)
}

func Parse(expr string, loc *time.Location) (*CronExpr, error) {
	expr = strings.TrimSpace(expr)
	if loc == nil {
		loc = time.UTC
	}

	if expanded, ok := macros[expr]; ok {
		expr = expanded
	}

	fields := strings.Fields(expr)
	c := &CronExpr{
		WeekdayDOM: -1,
		LastDOW:    -1,
		NthDOW:     [2]int{-1, -1},
		Location:   loc,
	}

	switch len(fields) {
	case 5:
		c.HasSeconds = false
		return c, parseFields(c, fields, false)
	case 6:
		c.HasSeconds = true
		return c, parseFields(c, fields, true)
	default:
		return nil, fmt.Errorf("expected 5 or 6 fields, got %d", len(fields))
	}
}

func parseFields(c *CronExpr, fields []string, hasSec bool) error {
	idx := 0
	if hasSec {
		if err := parseField(fields[0], FieldSecond, &c.Second, c); err != nil {
			return err
		}
		idx = 1
	} else {
		c.Second.Set(0) // default: fire at second 0
	}

	parsers := []struct {
		field FieldType
		bs    *Bitset
	}{
		{FieldMinute, &c.Minute},
		{FieldHour, &c.Hour},
		{FieldDayOfMonth, &c.DayOfMonth},
		{FieldMonth, &c.Month},
		{FieldDayOfWeek, &c.DayOfWeek},
	}

	for i, p := range parsers {
		raw := fields[idx+i]

		if p.field == FieldDayOfMonth && raw == "*" {
			c.DOMWildcard = true
		}
		if p.field == FieldDayOfWeek && raw == "*" {
			c.DOWWildcard = true
		}

		if err := parseField(raw, p.field, p.bs, c); err != nil {
			return err
		}
	}
	return nil
}

func parseField(raw string, ft FieldType, bs *Bitset, c *CronExpr) error {
	bounds := fieldBounds[ft]
	lo, hi := bounds[0], bounds[1]

	if ft == FieldDayOfMonth {
		if raw == "L" {
			c.LastDOM = true
			for i := lo; i <= hi; i++ {
				bs.Set(i)
			}
			return nil
		}
		if strings.HasSuffix(raw, "W") {
			numStr := strings.TrimSuffix(raw, "W")
			n, err := strconv.Atoi(numStr)
			if err != nil || n < 1 || n > 31 {
				return &ParseError{ft, int(ft), raw, "invalid weekday-nearest value"}
			}
			c.WeekdayDOM = n
			for i := lo; i <= hi; i++ {
				bs.Set(i)
			}
			return nil
		}
	}

	if ft == FieldDayOfWeek {
		if strings.HasSuffix(raw, "L") {
			numStr := strings.TrimSuffix(raw, "L")
			n, err := resolveDOW(numStr)
			if err != nil {
				return &ParseError{ft, int(ft), raw, "invalid last-weekday value"}
			}
			c.LastDOW = n
			for i := lo; i <= hi; i++ {
				bs.Set(i)
			}
			return nil
		}
		if strings.Contains(raw, "#") {
			parts := strings.SplitN(raw, "#", 2)
			dow, err := resolveDOW(parts[0])
			if err != nil {
				return &ParseError{ft, int(ft), raw, "invalid weekday in # expression"}
			}
			nth, err := strconv.Atoi(parts[1])
			if err != nil || nth < 1 || nth > 5 {
				return &ParseError{ft, int(ft), raw, "nth must be 1-5"}
			}
			c.NthDOW = [2]int{dow, nth}
			for i := lo; i <= hi; i++ {
				bs.Set(i)
			}
			return nil
		}
	}

	parts := strings.Split(raw, ",")
	for _, part := range parts {
		if err := parseAtom(part, ft, bs, lo, hi); err != nil {
			return err
		}
	}
	return nil
}

func parseAtom(part string, ft FieldType, bs *Bitset, lo, hi int) error {
	var step int
	base := part

	if idx := strings.Index(part, "/"); idx >= 0 {
		stepStr := part[idx+1:]
		s, err := strconv.Atoi(stepStr)
		if err != nil || s <= 0 {
			return &ParseError{ft, int(ft), part, "invalid step value"}
		}
		step = s
		base = part[:idx]
	}

	if base == "*" {
		start := lo
		if step == 0 {
			step = 1
		}
		for i := start; i <= hi; i += step {
			bs.Set(i)
		}
		return nil
	}

	if idx := strings.Index(base, "-"); idx >= 0 {
		fromStr, toStr := base[:idx], base[idx+1:]
		from, err := resolveValue(fromStr, ft)
		if err != nil {
			return &ParseError{ft, int(ft), part, "invalid range start"}
		}
		to, err := resolveValue(toStr, ft)
		if err != nil {
			return &ParseError{ft, int(ft), part, "invalid range end"}
		}
		if from > to {
			return &ParseError{ft, int(ft), part, fmt.Sprintf("range start %d > end %d", from, to)}
		}
		if step == 0 {
			step = 1
		}
		for i := from; i <= to; i += step {
			bs.Set(i)
		}
		return nil
	}

	val, err := resolveValue(base, ft)
	if err != nil {
		return &ParseError{ft, int(ft), part, "invalid value"}
	}
	if val < lo || val > hi {
		return &ParseError{ft, int(ft), part, fmt.Sprintf("value %d outside range %d-%d", val, lo, hi)}
	}
	if step > 0 {
		for i := val; i <= hi; i += step {
			bs.Set(i)
		}
	} else {
		bs.Set(val)
	}
	return nil
}

func resolveValue(s string, ft FieldType) (int, error) {
	if ft == FieldMonth {
		if v, ok := monthNames[strings.ToUpper(s)]; ok {
			return v, nil
		}
	}
	if ft == FieldDayOfWeek {
		return resolveDOW(s)
	}
	return strconv.Atoi(s)
}

func resolveDOW(s string) (int, error) {
	if v, ok := dayNames[strings.ToUpper(s)]; ok {
		return v, nil
	}
	return strconv.Atoi(s)
}
```

### scheduler.go

```go
package cron

import "time"

const maxIterations = 366 * 24 * 60 * 5 // ~5 years of minutes, safety limit

func (c *CronExpr) Matches(t time.Time) bool {
	t = t.In(c.Location)

	if !c.Second.Has(t.Second()) {
		return false
	}
	if !c.Minute.Has(t.Minute()) {
		return false
	}
	if !c.Hour.Has(t.Hour()) {
		return false
	}
	if !c.Month.Has(int(t.Month())) {
		return false
	}

	return c.matchDay(t)
}

func (c *CronExpr) matchDay(t time.Time) bool {
	dom := c.matchDayOfMonth(t)
	dow := c.matchDayOfWeek(t)

	// Union semantics: if both fields are specified (non-wildcard), match either
	if !c.DOMWildcard && !c.DOWWildcard {
		return dom || dow
	}
	// If one is wildcard, only the other matters
	return dom && dow
}

func (c *CronExpr) matchDayOfMonth(t time.Time) bool {
	day := t.Day()

	if c.LastDOM {
		return day == lastDayOfMonth(t.Year(), t.Month())
	}
	if c.WeekdayDOM >= 0 {
		return day == nearestWeekday(t.Year(), t.Month(), c.WeekdayDOM)
	}
	return c.DayOfMonth.Has(day)
}

func (c *CronExpr) matchDayOfWeek(t time.Time) bool {
	dow := int(t.Weekday()) // Sunday = 0

	if c.LastDOW >= 0 {
		return dow == c.LastDOW && isLastOccurrenceInMonth(t)
	}
	if c.NthDOW[0] >= 0 {
		return dow == c.NthDOW[0] && nthWeekdayOccurrence(t) == c.NthDOW[1]
	}
	return c.DayOfWeek.Has(dow)
}

func (c *CronExpr) Next(from time.Time) time.Time {
	results := c.NextN(from, 1)
	if len(results) == 0 {
		return time.Time{}
	}
	return results[0]
}

func (c *CronExpr) NextN(from time.Time, n int) []time.Time {
	if n <= 0 {
		return nil
	}

	results := make([]time.Time, 0, n)
	t := from.In(c.Location).Truncate(time.Second).Add(time.Second)

	for i := 0; i < maxIterations && len(results) < n; i++ {
		next, found := c.findNext(t)
		if !found {
			break
		}
		results = append(results, next)
		t = next.Add(time.Second)
	}

	return results
}

func (c *CronExpr) findNext(from time.Time) (time.Time, bool) {
	t := from

	for year := t.Year(); year <= t.Year()+5; {
		// Advance month
		mon, ok := c.Month.NextFrom(int(t.Month()), 12)
		if !ok {
			t = time.Date(t.Year()+1, 1, 1, 0, 0, 0, 0, c.Location)
			year = t.Year()
			continue
		}
		if mon != int(t.Month()) {
			t = time.Date(t.Year(), time.Month(mon), 1, 0, 0, 0, 0, c.Location)
		}

		// Advance day
		maxDay := lastDayOfMonth(t.Year(), t.Month())
		dayFound := false
		for d := t.Day(); d <= maxDay; d++ {
			candidate := time.Date(t.Year(), t.Month(), d, t.Hour(), t.Minute(), t.Second(), 0, c.Location)
			if d != t.Day() {
				candidate = time.Date(t.Year(), t.Month(), d, 0, 0, 0, 0, c.Location)
			}
			if c.matchDay(candidate) {
				t = candidate
				dayFound = true
				break
			}
		}
		if !dayFound {
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, c.Location)
			continue
		}

		// Advance hour
		hr, ok := c.Hour.NextFrom(t.Hour(), 23)
		if !ok {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, c.Location)
			continue
		}
		if hr != t.Hour() {
			t = time.Date(t.Year(), t.Month(), t.Day(), hr, 0, 0, 0, c.Location)
		}

		// Advance minute
		mn, ok := c.Minute.NextFrom(t.Minute(), 59)
		if !ok {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, c.Location)
			continue
		}
		if mn != t.Minute() {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), mn, 0, 0, c.Location)
		}

		// Advance second
		sec, ok := c.Second.NextFrom(t.Second(), 59)
		if !ok {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute()+1, 0, 0, c.Location)
			continue
		}
		t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), sec, 0, c.Location)

		// Final day re-check after all adjustments
		if c.matchDay(t) && c.Matches(t) {
			return t, true
		}
		t = t.Add(time.Second)
	}

	return time.Time{}, false
}

func lastDayOfMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func nearestWeekday(year int, month time.Month, day int) int {
	maxDay := lastDayOfMonth(year, month)
	if day > maxDay {
		day = maxDay
	}
	t := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	wd := t.Weekday()

	switch wd {
	case time.Saturday:
		if day > 1 {
			return day - 1 // Friday
		}
		return day + 2 // Monday (don't cross month boundary backward)
	case time.Sunday:
		if day < maxDay {
			return day + 1 // Monday
		}
		return day - 2 // Friday (don't cross month boundary forward)
	default:
		return day
	}
}

func isLastOccurrenceInMonth(t time.Time) bool {
	nextWeek := t.AddDate(0, 0, 7)
	return nextWeek.Month() != t.Month()
}

func nthWeekdayOccurrence(t time.Time) int {
	return (t.Day()-1)/7 + 1
}
```

### cron_test.go

```go
package cron

import (
	"testing"
	"time"
)

func mustParse(t *testing.T, expr string) *CronExpr {
	t.Helper()
	c, err := Parse(expr, time.UTC)
	if err != nil {
		t.Fatalf("Parse(%q) failed: %v", expr, err)
	}
	return c
}

func TestParseBasicFields(t *testing.T) {
	tests := []struct {
		expr    string
		wantErr bool
	}{
		{"* * * * *", false},
		{"0 0 * * *", false},
		{"*/5 * * * *", false},
		{"1-5 * * * *", false},
		{"1,3,5 * * * *", false},
		{"0 0 1 JAN MON", false},
		{"0 0 L * *", false},
		{"0 0 15W * *", false},
		{"0 0 * * 5#3", false},
		{"0 0 * * 5L", false},
		{"@daily", false},
		{"@hourly", false},
		{"invalid", true},
		{"* * * *", true}, // only 4 fields
	}

	for _, tt := range tests {
		_, err := Parse(tt.expr, time.UTC)
		if (err != nil) != tt.wantErr {
			t.Errorf("Parse(%q): err=%v, wantErr=%v", tt.expr, err, tt.wantErr)
		}
	}
}

func TestSixFieldWithSeconds(t *testing.T) {
	c := mustParse(t, "30 */5 * * * *")
	if !c.HasSeconds {
		t.Fatal("expected HasSeconds=true for 6-field expression")
	}
	if !c.Second.Has(30) {
		t.Fatal("expected second 30 to be set")
	}
}

func TestMatches(t *testing.T) {
	c := mustParse(t, "0 12 * * *")
	noon := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	if !c.Matches(noon) {
		t.Error("expected noon to match '0 12 * * *'")
	}
	notNoon := time.Date(2025, 6, 15, 13, 0, 0, 0, time.UTC)
	if c.Matches(notNoon) {
		t.Error("expected 13:00 to not match '0 12 * * *'")
	}
}

func TestNextEveryFiveMinutes(t *testing.T) {
	c := mustParse(t, "*/5 * * * *")
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	results := c.NextN(from, 5)

	expected := []time.Time{
		time.Date(2025, 1, 1, 0, 5, 0, 0, time.UTC),
		time.Date(2025, 1, 1, 0, 10, 0, 0, time.UTC),
		time.Date(2025, 1, 1, 0, 15, 0, 0, time.UTC),
		time.Date(2025, 1, 1, 0, 20, 0, 0, time.UTC),
		time.Date(2025, 1, 1, 0, 25, 0, 0, time.UTC),
	}

	if len(results) != len(expected) {
		t.Fatalf("expected %d results, got %d", len(expected), len(results))
	}
	for i, r := range results {
		if !r.Equal(expected[i]) {
			t.Errorf("result[%d] = %v, want %v", i, r, expected[i])
		}
	}
}

func TestNextDaily(t *testing.T) {
	c := mustParse(t, "@daily")
	from := time.Date(2025, 3, 15, 10, 30, 0, 0, time.UTC)
	results := c.NextN(from, 3)

	expected := []time.Time{
		time.Date(2025, 3, 16, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 3, 17, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 3, 18, 0, 0, 0, 0, time.UTC),
	}

	for i, r := range results {
		if !r.Equal(expected[i]) {
			t.Errorf("result[%d] = %v, want %v", i, r, expected[i])
		}
	}
}

func TestLastDayOfMonth(t *testing.T) {
	c := mustParse(t, "0 0 L * *")
	from := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC) // 2024 is leap year
	next := c.Next(from)

	if next.Day() != 29 {
		t.Errorf("expected Feb 29 (leap year), got day %d", next.Day())
	}
}

func TestWeekdayNearest(t *testing.T) {
	// 2025-03-15 is Saturday -> nearest weekday is Friday 14th
	expected := nearestWeekday(2025, 3, 15)
	if expected != 14 {
		t.Errorf("nearestWeekday(2025, 3, 15) = %d, want 14 (Friday)", expected)
	}

	// 2025-03-16 is Sunday -> nearest weekday is Monday 17th
	expected = nearestWeekday(2025, 3, 16)
	if expected != 17 {
		t.Errorf("nearestWeekday(2025, 3, 16) = %d, want 17 (Monday)", expected)
	}
}

func TestNthDayOfWeek(t *testing.T) {
	// Third Friday of Jan 2025: Jan 17
	c := mustParse(t, "0 0 * * 5#3")
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	next := c.Next(from)

	if next.Day() != 17 || next.Month() != 1 {
		t.Errorf("expected Jan 17 (3rd Friday), got %v", next)
	}
}

func TestValidationErrors(t *testing.T) {
	invalids := []string{
		"60 * * * *",    // minute > 59
		"* 24 * * *",    // hour > 23
		"* * 32 * *",    // day > 31
		"* * * 13 *",    // month > 12
		"* * * * 8",     // dow > 6 (7 might be accepted as Sunday alias, but 8 is invalid)
		"*/0 * * * *",   // step of 0
		"5-2 * * * *",   // range start > end
	}

	for _, expr := range invalids {
		_, err := Parse(expr, time.UTC)
		if err == nil {
			t.Errorf("expected error for %q, got nil", expr)
		}
	}
}

func TestNamedMonthsAndDays(t *testing.T) {
	c := mustParse(t, "0 0 * JAN-MAR MON-FRI")
	if !c.Month.Has(1) || !c.Month.Has(2) || !c.Month.Has(3) {
		t.Error("expected months 1-3 to be set")
	}
	if c.Month.Has(4) {
		t.Error("expected month 4 to NOT be set")
	}
	if !c.DayOfWeek.Has(1) || !c.DayOfWeek.Has(5) {
		t.Error("expected MON-FRI (1-5) to be set")
	}
	if c.DayOfWeek.Has(0) || c.DayOfWeek.Has(6) {
		t.Error("expected SUN and SAT to NOT be set")
	}
}

func TestUnionSemantics(t *testing.T) {
	// Both day-of-month and day-of-week specified: union semantics
	// "on the 1st AND on every Monday"
	c := mustParse(t, "0 0 1 * 1")

	// 2025-01-01 is Wednesday, day=1 -> matches via DOM
	jan1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if !c.Matches(jan1) {
		t.Error("expected Jan 1 to match via day-of-month")
	}

	// 2025-01-06 is Monday, day=6 -> matches via DOW
	jan6 := time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC)
	if !c.Matches(jan6) {
		t.Error("expected Jan 6 (Monday) to match via day-of-week")
	}

	// 2025-01-07 is Tuesday, day=7 -> matches neither
	jan7 := time.Date(2025, 1, 7, 0, 0, 0, 0, time.UTC)
	if c.Matches(jan7) {
		t.Error("expected Jan 7 (Tuesday, day=7) to NOT match")
	}
}
```

## Running the Solution

```bash
# Initialize module
mkdir -p cron && cd cron
go mod init cron

# Run tests
go test -v ./...

# Run benchmarks (optional)
go test -bench=. -benchmem ./...
```

## Expected Output

```
=== RUN   TestParseBasicFields
--- PASS: TestParseBasicFields (0.00s)
=== RUN   TestSixFieldWithSeconds
--- PASS: TestSixFieldWithSeconds (0.00s)
=== RUN   TestMatches
--- PASS: TestMatches (0.00s)
=== RUN   TestNextEveryFiveMinutes
--- PASS: TestNextEveryFiveMinutes (0.00s)
=== RUN   TestNextDaily
--- PASS: TestNextDaily (0.00s)
=== RUN   TestLastDayOfMonth
--- PASS: TestLastDayOfMonth (0.00s)
=== RUN   TestWeekdayNearest
--- PASS: TestWeekdayNearest (0.00s)
=== RUN   TestNthDayOfWeek
--- PASS: TestNthDayOfWeek (0.00s)
=== RUN   TestValidationErrors
--- PASS: TestValidationErrors (0.00s)
=== RUN   TestNamedMonthsAndDays
--- PASS: TestNamedMonthsAndDays (0.00s)
=== RUN   TestUnionSemantics
--- PASS: TestUnionSemantics (0.00s)
PASS
ok      cron    0.003s
```

## Design Decisions

1. **Bitset for field representation**: Each field's allowed values are stored as a boolean array indexed by value. This makes `Has()` O(1) and `NextFrom()` O(range). Since the maximum range is 0-59, the memory cost is trivial. Alternatives like sorted slices would be slower for lookups and more complex for range iteration.

2. **Forward scanning with field-level backtracking**: The `findNext` algorithm scans fields from most-significant (month) to least-significant (second). When a lower field wraps (e.g., no valid minute remaining in the current hour), it increments the next higher field and restarts. This avoids the brute-force approach of checking every second.

3. **Union semantics for DOM/DOW**: Following the POSIX specification, when both day-of-month and day-of-week are non-wildcard, the expression matches if either condition is satisfied. This is counterintuitive (many assume intersection) but matches standard cron behavior. The `DOMWildcard` and `DOWWildcard` flags track whether each field was originally `*`.

4. **Special characters as flags**: `L`, `W`, and `#` cannot be represented as simple bitsets. They require runtime computation that depends on the specific month/year being evaluated. These are stored as flags on `CronExpr` and evaluated dynamically in `matchDayOfMonth` and `matchDayOfWeek`.

5. **Safety limit on iteration**: The `maxIterations` constant prevents infinite loops on expressions that match very rarely or never (e.g., February 30). The 5-year window is generous enough for any practical expression.

## Common Mistakes

1. **Intersection instead of union for DOM+DOW**: The most common bug. When both fields are specified, standard cron fires if either matches, not both. Test with `1 * 1 * MON` and verify it fires on the 1st AND on every Monday.

2. **Forgetting to handle month rollover in `lastDayOfMonth`**: Calling `time.Date(year, month+1, 0, ...)` is the Go idiom. Manually tracking month lengths leads to leap year bugs.

3. **Off-by-one in `NextFrom`**: When computing the next occurrence, start from `from + 1 second`, not `from`. Otherwise `Matches(t)` followed by `Next(t)` returns `t` itself.

4. **Ignoring month boundaries for `W`**: If the 1st is Saturday, the nearest weekday is Monday the 3rd, not Friday of the previous month. The `W` modifier never crosses month boundaries.

5. **Named value case sensitivity**: `JAN`, `Jan`, and `jan` should all parse to month 1. Always normalize to uppercase before lookup.

## Performance Notes

- **Field matching**: O(1) per field via bitset lookup. Full match check is O(6) -- constant time.
- **Next occurrence**: Worst case is proportional to the gap between `from` and the next match. For typical expressions (e.g., `*/5 * * * *`), this is a handful of iterations. For rare expressions (e.g., `0 0 29 2 *` -- only Feb 29), it may scan up to 4 years.
- **Memory**: A `CronExpr` is approximately 400 bytes (6 bitsets of 64 bools plus flags). Negligible.
- **Parsing**: O(n) where n is the expression length. Dominated by string splitting and numeric conversion.
- For high-throughput schedulers managing thousands of cron expressions, consider precomputing the next occurrence for each and using a min-heap priority queue to determine which fires next.

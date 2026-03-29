# 53. Cron Expression Parser

<!--
difficulty: intermediate-advanced
category: databases-time-series-tools
languages: [go]
concepts: [parsing, time-computation, cron-semantics, scheduling, field-validation]
estimated_time: 8-12 hours
bloom_level: apply, analyze
prerequisites: [go-basics, time-package, string-parsing, iterators]
-->

## Languages

- Go (1.22+)

## Prerequisites

- Go standard library `time` package (parsing, comparison, time zone handling)
- String splitting and numeric parsing
- Understanding of cron syntax at a user level (crontab)
- Familiarity with calendar edge cases (month lengths, leap years, day-of-week numbering)

## Learning Objectives

- **Implement** a tokenizer and field parser for standard 5-field and extended 6-field cron expressions
- **Apply** set-based time matching to determine whether a given instant satisfies a cron expression
- **Analyze** the interaction between day-of-month and day-of-week fields (union vs. intersection semantics)
- **Design** a forward-scanning algorithm that computes the next N occurrences from any starting time
- **Evaluate** edge cases: February 29, `L` (last day), `W` (nearest weekday), step overflow

## The Challenge

Cron expressions are the universal language for scheduling recurring tasks. Every CI/CD pipeline, database backup job, and monitoring alert uses them. Despite their apparent simplicity -- five fields separated by spaces -- the semantics are surprisingly intricate. Wildcards, ranges, steps, lists, and special characters like `L` and `W` interact in ways that produce subtle bugs in naive implementations.

Your task is to build a cron expression parser and scheduler in Go. The parser must accept both standard 5-field expressions (`minute hour day-of-month month day-of-week`) and extended 6-field expressions (prepend `seconds`). Given a parsed expression and a reference time, compute the next N timestamps that match.

The core difficulty is not parsing the syntax -- it is computing forward occurrences correctly. You must iterate through time fields from most-significant (year) to least-significant (second/minute), backtracking when a lower field wraps around. Miss one edge case and your scheduler fires at the wrong time or loops forever.

## Requirements

1. Parse standard 5-field cron: `minute(0-59) hour(0-23) day-of-month(1-31) month(1-12) day-of-week(0-6, 0=Sunday)`
2. Parse extended 6-field cron: `second(0-59) minute hour day-of-month month day-of-week`
3. Support all field expression types:
   - Wildcard: `*` (match any value)
   - Specific value: `5` (match exactly)
   - List: `1,3,5` (match any in set)
   - Range: `1-5` (match inclusive range)
   - Step: `*/5` or `1-10/2` (match at interval)
   - Named values in month (`JAN`-`DEC`) and day-of-week (`SUN`-`SAT`)
4. Support special characters:
   - `L` in day-of-month: last day of the month
   - `W` in day-of-month: nearest weekday to the given day (e.g., `15W`)
   - `L` in day-of-week: last occurrence of a weekday in the month (e.g., `5L` = last Friday)
   - `#` in day-of-week: nth occurrence (e.g., `5#3` = third Friday)
5. Validate expressions and return structured error messages with field index and reason
6. Implement `NextN(from time.Time, n int) []time.Time` -- compute next N matching times after `from`
7. Implement `Matches(t time.Time) bool` -- check if a specific time matches the expression
8. Handle predefined macros: `@yearly`, `@monthly`, `@weekly`, `@daily`, `@hourly`
9. Day-of-month and day-of-week use union semantics when both are specified (standard cron behavior): a time matches if either field matches
10. Handle time zones correctly: the caller passes a `*time.Location` and all computations respect it

## Hints

<details>
<summary>Hint 1: Field representation</summary>

Represent each field as a set of allowed values. A bitset works well since all fields have small ranges:

```go
type CronField struct {
    Allowed [60]bool // large enough for any field (seconds/minutes: 0-59)
    Min, Max int
}

func (f *CronField) Contains(v int) bool {
    return v >= f.Min && v <= f.Max && f.Allowed[v]
}
```

Parse each field expression into this set representation. `*/5` with range 0-59 sets positions 0, 5, 10, ..., 55.
</details>

<details>
<summary>Hint 2: Forward scanning for next occurrence</summary>

Scan fields from most to least significant. When you advance a field, reset all less-significant fields to their minimum allowed value:

```go
func (c *CronExpr) Next(from time.Time) time.Time {
    t := from.Add(time.Second) // start after 'from'
    // Truncate to seconds
    t = t.Truncate(time.Second)

    // Advance month until it matches, reset day/hour/min/sec
    // Advance day until it matches, reset hour/min/sec
    // Advance hour until it matches, reset min/sec
    // ...
    // If any field wraps past its max, carry to the next higher field
}
```

Use a loop with a year limit (e.g., 5 years) to prevent infinite loops on impossible expressions.
</details>

<details>
<summary>Hint 3: Last-day and weekday-nearest</summary>

For `L` in day-of-month, compute the last day dynamically:

```go
func lastDayOfMonth(year int, month time.Month) int {
    return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}
```

For `W` (nearest weekday), find the given day, check if it is a weekday. If Saturday, use Friday (unless it crosses month boundary). If Sunday, use Monday. This must be recalculated for each candidate month.
</details>

<details>
<summary>Hint 4: Day-of-week and day-of-month union</summary>

Standard cron uses union semantics: if both fields are non-wildcard, a day matches if it satisfies either field. Only when one field is `*` does the other field alone determine the match. This is the POSIX specification and catches many people off guard.
</details>

## Acceptance Criteria

- [ ] Parses valid 5-field and 6-field expressions without error
- [ ] Rejects invalid expressions with meaningful error messages (field index, expected range, actual value)
- [ ] `*/5` in minute field produces correct next times (0, 5, 10, ...)
- [ ] `1-5` range and `1,3,5` list produce correct matches
- [ ] Named months (`JAN`, `FEB`) and days (`MON`, `TUE`) work correctly
- [ ] `L` in day-of-month correctly resolves to last day (28, 29, 30, or 31 depending on month/year)
- [ ] `15W` resolves to nearest weekday to the 15th
- [ ] `5#3` resolves to third Friday of the month
- [ ] `@daily`, `@hourly`, and other macros expand to correct expressions
- [ ] `NextN` returns exactly N timestamps in ascending order
- [ ] `Matches` correctly identifies matching and non-matching times
- [ ] Union semantics for day-of-month + day-of-week when both are specified
- [ ] Handles February 29 in leap years correctly
- [ ] All tests pass with `go test ./...`

## Research Resources

- [Cron Expression Format (Wikipedia)](https://en.wikipedia.org/wiki/Cron#CRON_expression) -- standard and extended syntax
- [POSIX crontab specification](https://pubs.opengroup.org/onlinepubs/9699919799/utilities/crontab.html) -- authoritative day-of-month/day-of-week union semantics
- [Quartz Scheduler CronExpression](http://www.quartz-scheduler.org/documentation/quartz-2.3.0/tutorials/crontrigger.html) -- reference for L, W, # extensions
- [robfig/cron (Go)](https://github.com/robfig/cron) -- production Go cron library for comparison
- [cronexpr (Go)](https://github.com/gorhill/cronexpr) -- another Go implementation with extended syntax

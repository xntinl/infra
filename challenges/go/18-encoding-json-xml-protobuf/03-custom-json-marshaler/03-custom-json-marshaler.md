# Exercise 03: Custom JSON Marshaler

**Difficulty:** Intermediate | **Estimated Time:** 25 minutes | **Section:** 18 - Encoding

## Overview

Sometimes struct tags are not enough. You need dates in a particular format, enums as strings, or durations as human-readable text. Go lets you implement the `json.Marshaler` and `json.Unmarshaler` interfaces on any type to take full control of its JSON representation.

## Prerequisites

- Exercises 01-02 (JSON basics, struct tags)
- Interfaces
- `time` package basics

## The Interfaces

```go
type Marshaler interface {
    MarshalJSON() ([]byte, error)
}

type Unmarshaler interface {
    UnmarshalJSON([]byte) error
}
```

When `encoding/json` encounters a type that implements either interface, it calls that method instead of using the default encoding/decoding logic.

## Task

Build a small scheduling system with custom JSON representations:

1. **`Priority` type** -- an `int` enum (`Low=1`, `Medium=2`, `High=3`, `Critical=4`) that marshals to/from its string name rather than a raw number.

2. **`DateOnly` type** -- wraps `time.Time` but marshals as `"2006-01-02"` (date only, no time or timezone).

3. **`Duration` type** -- wraps `time.Duration` but marshals as a human string like `"2h30m"` instead of nanoseconds.

4. **`Task` struct** -- uses all three custom types:
   ```
   ID          int
   Title       string
   Priority    Priority
   Deadline    DateOnly
   Estimate    Duration
   ```

5. Create a slice of tasks, marshal to JSON, print it, then unmarshal it back and verify the round-trip.

## Hints

- `MarshalJSON` must return valid JSON. For a string value, you must include the quotes. The easiest way: `return json.Marshal(stringValue)`.
- `UnmarshalJSON` receives the raw JSON token including quotes. Use `json.Unmarshal` into a `string` first, then parse.
- For `Priority`, use a pair of maps: `priorityToString` and `stringToPriority`.
- For `Duration`, `time.ParseDuration` handles the `"2h30m"` format natively.
- Remember to use **pointer receivers** on `UnmarshalJSON` so the method can modify the value.

## Verification

Your program should produce output matching this structure:

```json
[
  {
    "id": 1,
    "title": "Write docs",
    "priority": "High",
    "deadline": "2026-06-15",
    "estimate": "2h30m0s"
  },
  {
    "id": 2,
    "title": "Fix bug",
    "priority": "Critical",
    "deadline": "2026-03-20",
    "estimate": "45m0s"
  }
]
```

After unmarshaling, printing the tasks with `%+v` should show the original Go values restored correctly.

## Key Takeaways

- Implement `json.Marshaler` / `json.Unmarshaler` for full control
- Return valid JSON from `MarshalJSON` -- include quotes for strings
- Use pointer receivers on `UnmarshalJSON`
- Delegating to `json.Marshal`/`json.Unmarshal` for inner encoding avoids manual quote handling
- Custom types compose naturally inside structs without any special wiring

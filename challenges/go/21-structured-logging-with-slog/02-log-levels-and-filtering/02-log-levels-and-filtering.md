# 2. Log Levels and Filtering

<!--
difficulty: basic
concepts: [log-levels, slog-leveler, handler-options, level-var, runtime-level-change]
tools: [go]
estimated_time: 15m
bloom_level: remember
prerequisites: [slog-basics]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [01 - Slog Basics](../01-slog-basics/01-slog-basics.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Recall** the four standard slog levels and their numeric values
- **Configure** a handler to filter logs by minimum level
- **Change** the log level at runtime using `slog.LevelVar`

## Why Log Levels and Filtering

In production you want Info and above. During debugging you want Debug too. If you cannot control the level, you either miss important details or drown in noise. `slog` handlers accept a minimum level so you can filter at the source rather than grepping after the fact.

The ability to change the level at runtime is critical for production systems. When a bug appears, you enable Debug logging via an HTTP endpoint or signal without restarting the service, collect the data you need, and switch back to Info.

## Step 1 -- Understanding Level Values

Create a project:

```bash
mkdir -p ~/go-exercises/slog-levels
cd ~/go-exercises/slog-levels
go mod init slog-levels
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"log/slog"
)

func main() {
	fmt.Println("Debug:", slog.LevelDebug)
	fmt.Println("Info: ", slog.LevelInfo)
	fmt.Println("Warn: ", slog.LevelWarn)
	fmt.Println("Error:", slog.LevelError)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Debug: DEBUG
Info:  INFO
Warn:  WARN
Error: ERROR
```

Internally, these are integers: Debug=-4, Info=0, Warn=4, Error=8. The gaps allow custom levels between them.

## Step 2 -- Set the Minimum Level

Create a handler with a minimum level:

```go
package main

import (
	"log/slog"
	"os"
)

func main() {
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	logger := slog.New(handler)

	logger.Debug("this is a debug message", "detail", "verbose")
	logger.Info("this is an info message", "status", "ok")
	logger.Warn("this is a warning", "threshold", 90)
	logger.Error("this is an error", "code", 500)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (all four levels appear because minimum is Debug):

```
time=2025-01-15T10:30:00.000Z level=DEBUG msg="this is a debug message" detail=verbose
time=2025-01-15T10:30:00.000Z level=INFO msg="this is an info message" status=ok
time=2025-01-15T10:30:00.000Z level=WARN msg="this is a warning" threshold=90
time=2025-01-15T10:30:00.000Z level=ERROR msg="this is an error" code=500
```

## Step 3 -- Filter at Warn Level

Change the minimum level to `Warn` to suppress Debug and Info:

```go
package main

import (
	"log/slog"
	"os"
)

func main() {
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	})
	logger := slog.New(handler)

	logger.Debug("hidden debug")
	logger.Info("hidden info")
	logger.Warn("visible warning", "action", "investigate")
	logger.Error("visible error", "action", "fix immediately")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (only Warn and Error appear):

```
time=2025-01-15T10:30:00.000Z level=WARN msg="visible warning" action=investigate
time=2025-01-15T10:30:00.000Z level=ERROR msg="visible error" action="fix immediately"
```

## Step 4 -- Change Level at Runtime with LevelVar

`slog.LevelVar` allows changing the level without recreating the handler:

```go
package main

import (
	"log/slog"
	"os"
)

func main() {
	var level slog.LevelVar
	level.Set(slog.LevelInfo)

	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: &level,
	})
	logger := slog.New(handler)

	logger.Debug("before: debug hidden")
	logger.Info("before: info visible")

	// Simulate enabling debug at runtime
	level.Set(slog.LevelDebug)

	logger.Debug("after: debug now visible", "reason", "runtime change")
	logger.Info("after: info still visible")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
time=2025-01-15T10:30:00.000Z level=INFO msg="before: info visible"
time=2025-01-15T10:30:00.000Z level=DEBUG msg="after: debug now visible" reason="runtime change"
time=2025-01-15T10:30:00.000Z level=INFO msg="after: info still visible"
```

The first Debug line is suppressed. After changing the level, the second Debug line appears.

## Step 5 -- Custom Log Levels

You can define custom levels between the standard ones:

```go
package main

import (
	"log/slog"
	"os"
)

const (
	LevelTrace = slog.Level(-8)
	LevelNotice = slog.Level(2)
)

func main() {
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: LevelTrace,
	})
	logger := slog.New(handler)

	logger.Log(nil, LevelTrace, "very detailed trace", "bytes", 1024)
	logger.Debug("standard debug")
	logger.Log(nil, LevelNotice, "notice level", "event", "deploy")
	logger.Info("standard info")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
time=2025-01-15T10:30:00.000Z level=DEBUG-4 msg="very detailed trace" bytes=1024
time=2025-01-15T10:30:00.000Z level=DEBUG msg="standard debug"
time=2025-01-15T10:30:00.000Z level=INFO+2 msg="notice level" event=deploy
time=2025-01-15T10:30:00.000Z level=INFO msg="standard info"
```

Custom levels show relative to the nearest standard level.

## Common Mistakes

### Setting Level on the Default Logger

**Wrong:**

```go
slog.SetLogLoggerLevel(slog.LevelDebug)
slog.Debug("still might not work")
```

**What happens:** `SetLogLoggerLevel` only affects the bridge between `log` and `slog`, not the slog default handler's minimum level.

**Fix:** Create a new handler with the desired level and use `slog.SetDefault`.

### Passing a Level Instead of a Pointer

**Wrong:**

```go
var level slog.LevelVar
handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
    Level: level, // not a pointer -- changes won't propagate
})
```

**Fix:** Use `&level` to allow runtime changes.

## Verify What You Learned

Run the program and confirm that runtime level changes take effect immediately without restarting.

```bash
go run main.go
```

## What's Next

Continue to [03 - JSON Handler vs Text Handler](../03-json-handler-vs-text-handler/03-json-handler-vs-text-handler.md) to choose between human-readable and machine-readable output formats.

## Summary

- Standard levels: Debug (-4), Info (0), Warn (4), Error (8)
- Set `HandlerOptions.Level` to filter logs by minimum level
- Use `slog.LevelVar` with a pointer to change levels at runtime
- Custom levels fit in the gaps between standard levels
- The default handler minimum is `Info` -- Debug is suppressed unless configured

## Reference

- [slog.HandlerOptions](https://pkg.go.dev/log/slog#HandlerOptions)
- [slog.LevelVar](https://pkg.go.dev/log/slog#LevelVar)
- [slog.Level](https://pkg.go.dev/log/slog#Level)

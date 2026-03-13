<!--
difficulty: advanced
concepts: go-plugin, shared-objects, runtime-loading, symbol-lookup, extensibility
tools: go build -buildmode=plugin, plugin.Open, plugin.Lookup
estimated_time: 35m
bloom_level: creating
prerequisites: interfaces, packages-and-modules, build-constraints
-->

# Exercise 29.8: Plugin System

## Prerequisites

Before starting this exercise, you should be comfortable with:

- Interfaces and type assertions
- Go packages and modules
- Build constraints (Exercise 29.6)

## Learning Objectives

By the end of this exercise, you will be able to:

1. Build Go plugins as shared objects using `-buildmode=plugin`
2. Load plugins at runtime with `plugin.Open` and look up symbols with `plugin.Lookup`
3. Define a shared interface contract between a host application and its plugins
4. Handle plugin loading errors and version mismatches gracefully

## Why This Matters

Plugin systems enable extensible architectures where new behavior is added without recompiling the host application. Go's `plugin` package supports this on Linux and macOS by building `.so` files that are loaded at runtime. This pattern appears in monitoring agents, data pipeline processors, and CLI tools that support user-defined extensions.

---

## Problem

Build a text processing pipeline where the host application loads transformation plugins at runtime. Each plugin implements a shared `Transformer` interface. The host discovers `.so` files in a `plugins/` directory, loads them, and chains their transformations together.

### Hints

- Plugins must be built with `go build -buildmode=plugin -o plugin.so`
- Each plugin exports a symbol (typically a function or variable) that the host looks up by name
- The plugin and host must agree on the interface type -- define it in a shared package
- Plugins are only supported on Linux and macOS (`darwin`), not Windows
- `plugin.Lookup` returns an `interface{}` that must be type-asserted

### Step 1: Create the project

```bash
mkdir -p plugin-system && cd plugin-system
go mod init plugin-system
mkdir -p shared plugins/uppercase plugins/reverse plugins/leetspeak
```

### Step 2: Define the shared interface

Create `shared/transformer.go`:

```go
package shared

// Transformer defines the contract all plugins must implement.
type Transformer interface {
	Name() string
	Transform(input string) string
}
```

### Step 3: Write the plugins

Create `plugins/uppercase/main.go`:

```go
package main

import "strings"

type uppercaseTransformer struct{}

func (t *uppercaseTransformer) Name() string            { return "uppercase" }
func (t *uppercaseTransformer) Transform(s string) string { return strings.ToUpper(s) }

// NewTransformer is the symbol the host looks up.
var NewTransformer = func() interface{} {
	return &uppercaseTransformer{}
}
```

Create `plugins/reverse/main.go`:

```go
package main

type reverseTransformer struct{}

func (t *reverseTransformer) Name() string { return "reverse" }
func (t *reverseTransformer) Transform(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

var NewTransformer = func() interface{} {
	return &reverseTransformer{}
}
```

Create `plugins/leetspeak/main.go`:

```go
package main

import "strings"

type leetspeakTransformer struct{}

func (t *leetspeakTransformer) Name() string { return "leetspeak" }
func (t *leetspeakTransformer) Transform(s string) string {
	replacer := strings.NewReplacer(
		"a", "4", "A", "4",
		"e", "3", "E", "3",
		"i", "1", "I", "1",
		"o", "0", "O", "0",
		"s", "5", "S", "5",
		"t", "7", "T", "7",
	)
	return replacer.Replace(s)
}

var NewTransformer = func() interface{} {
	return &leetspeakTransformer{}
}
```

### Step 4: Write the host application

Create `main.go`:

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"plugin"
	"strings"

	"plugin-system/shared"
)

func loadPlugins(dir string) ([]shared.Transformer, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.so"))
	if err != nil {
		return nil, err
	}

	var transformers []shared.Transformer
	for _, path := range matches {
		p, err := plugin.Open(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to load %s: %v\n", path, err)
			continue
		}

		sym, err := p.Lookup("NewTransformer")
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s missing NewTransformer symbol: %v\n", path, err)
			continue
		}

		factory, ok := sym.(*func() interface{})
		if !ok {
			fmt.Fprintf(os.Stderr, "warning: %s NewTransformer has wrong type\n", path)
			continue
		}

		t, ok := (*factory)().(shared.Transformer)
		if !ok {
			fmt.Fprintf(os.Stderr, "warning: %s does not implement Transformer\n", path)
			continue
		}

		fmt.Printf("Loaded plugin: %s\n", t.Name())
		transformers = append(transformers, t)
	}

	return transformers, nil
}

func main() {
	input := "Hello World from Go Plugins"
	if len(os.Args) > 1 {
		input = strings.Join(os.Args[1:], " ")
	}

	transformers, err := loadPlugins("./build/plugins")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading plugins: %v\n", err)
		os.Exit(1)
	}

	if len(transformers) == 0 {
		fmt.Println("No plugins found in ./build/plugins/")
		os.Exit(1)
	}

	fmt.Printf("\nInput: %s\n\n", input)

	result := input
	for _, t := range transformers {
		result = t.Transform(result)
		fmt.Printf("[%s] => %s\n", t.Name(), result)
	}

	fmt.Printf("\nFinal: %s\n", result)
}
```

### Step 5: Build the plugins and host

```bash
mkdir -p build/plugins

go build -buildmode=plugin -o build/plugins/uppercase.so ./plugins/uppercase/
go build -buildmode=plugin -o build/plugins/reverse.so ./plugins/reverse/
go build -buildmode=plugin -o build/plugins/leetspeak.so ./plugins/leetspeak/

go build -o build/host .
```

### Step 6: Run the pipeline

```bash
./build/host "Go plugins are powerful"
```

Expected output (plugin load order may vary based on glob sorting):

```
Loaded plugin: leetspeak
Loaded plugin: reverse
Loaded plugin: uppercase

Input: Go plugins are powerful

[leetspeak] => G0 plug1n5 4r3 p0w3rful
[reverse] => luFr3w0p 3r4 5n1gulp 0G
[uppercase] => LUFR3W0P 3R4 5N1GULP 0G

Final: LUFR3W0P 3R4 5N1GULP 0G
```

---

## Common Mistakes

1. **Building plugin and host with different Go versions** -- The plugin `.so` and the host must be compiled with the exact same Go toolchain version.
2. **Expecting Windows support** -- `plugin` only works on Linux and macOS. Use build constraints to provide an alternative on unsupported platforms.
3. **Module path mismatches** -- If the plugin imports a shared package, the module paths must match exactly or you get "plugin was built with a different version" errors.
4. **Forgetting the pointer indirection on Lookup** -- `plugin.Lookup` returns a pointer to the symbol. A `var` declared as `func() interface{}` is returned as `*func() interface{}`.

---

## Verify

```bash
ls build/plugins/*.so
./build/host "Test Input"
```

Confirm that at least one plugin loads and transforms the input string. Each plugin's transformation should be visible in the chained output.

---

## What's Next

In the next exercise, you will build a complete CLI code generator that combines AST parsing, templates, and user interaction into a production-quality tool.

## Summary

- `go build -buildmode=plugin` produces `.so` shared objects loadable at runtime
- `plugin.Open` loads a `.so` and `plugin.Lookup` retrieves exported symbols by name
- Define shared interfaces in a common package imported by both host and plugins
- Plugin and host must be built with the same Go version and compatible module paths
- Always handle missing or incompatible plugins gracefully with clear error messages

## Reference

- [plugin package](https://pkg.go.dev/plugin)
- [Go build modes](https://pkg.go.dev/cmd/go#hdr-Build_modes)
- [Plugin limitations and considerations](https://pkg.go.dev/plugin#hdr-Warnings)

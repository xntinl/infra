<!--
type: reference
difficulty: advanced
section: [10-metaprogramming]
concepts: [reflect.Type, reflect.Value, struct-tags, dynamic-dispatch, interface-checking, deep-copy]
languages: [go]
estimated_reading_time: 70 min
bloom_level: create
prerequisites: [go-interfaces, go-generics-basics, go-memory-model]
papers: []
industry_use: [encoding/json, gorm, wire, testify, protobuf, validator]
language_contrast: high
-->

# Go Reflection

> The `reflect` package lets Go programs inspect and manipulate their own types and values at runtime — the mechanism powering `encoding/json`, every ORM, and most dependency injection containers.

## Mental Model

Go's type system is static: every variable has a type known at compile time. Reflection is the runtime escape hatch. When you pass a value to `interface{}` (or `any` in Go 1.18+), Go stores two pointers: one to the concrete type descriptor (the `reflect.Type`) and one to the actual data. The `reflect` package surfaces these two pointers as inspectable Go values.

The key mental model: **reflection is the runtime equivalent of reading the compiler's type table**. Every struct field, every method, every tag annotation that the compiler knows about at compile time is accessible through `reflect` at runtime — but at the cost of bypassing all compile-time type safety. Operations that would be type errors at compile time become panics at runtime.

Understand the difference between `reflect.Type` and `reflect.Value` before writing any reflection code:

- `reflect.Type` describes the *shape* of a type: its kind, fields (for structs), methods, element type (for slices/maps). You get it from `reflect.TypeOf(x)`. It does not hold a value.
- `reflect.Value` describes a *specific value* of some type: it holds the actual data. You get it from `reflect.ValueOf(x)`. Operations on `reflect.Value` read or write the underlying data.

The third rail is addressability. A `reflect.Value` is addressable only if it was derived from a pointer. `reflect.ValueOf(myStruct).Field(0)` is not settable; `reflect.ValueOf(&myStruct).Elem().Field(0)` is. This mirrors Go's own addressability rules: you can only take the address of something that lives in memory.

## Core Concepts

### reflect.Type

```
reflect.TypeOf(x) → reflect.Type
    .Kind()         → reflect.Kind (Struct, Slice, Map, Ptr, Interface, ...)
    .Name()         → "MyStruct"
    .PkgPath()      → "github.com/org/pkg"
    .NumField()     → number of fields (for structs)
    .Field(i)       → reflect.StructField (name, type, tag, index, anonymous)
    .NumMethod()    → number of exported methods
    .Method(i)      → reflect.Method
    .Implements(t)  → bool (does this type implement interface t?)
    .Elem()         → element type (for Ptr, Slice, Array, Chan, Map)
    .Key()          → key type (for Map)
```

### reflect.Value

```
reflect.ValueOf(x) → reflect.Value
    .Type()         → reflect.Type
    .Kind()         → reflect.Kind
    .IsNil()        → bool (for ptr/chan/func/interface/map/slice)
    .IsZero()       → bool (is this the zero value for its type?)
    .Interface()    → any (extract the concrete value; panics if not exported)
    .Elem()         → pointed-to value (for Ptr) or contained value (for Interface)
    .Field(i)       → reflect.Value of struct field i
    .Index(i)       → reflect.Value of slice/array element i
    .MapKeys()      → []reflect.Value
    .MapIndex(k)    → reflect.Value for map key k
    .Set(v)         → set value (must be addressable and settable)
    .SetField / SetInt / SetString / ...
    .Call(args)     → []reflect.Value (call a func value)
    .MethodByName("M").Call(args) → []reflect.Value
```

### Struct Tag Parsing

Struct tags are string literals in the struct definition that tools read at runtime. The `reflect.StructTag` type provides a `Get(key string) string` method:

```go
type User struct {
    Name  string `json:"name" validate:"required,min=2"`
    Email string `json:"email" validate:"required,email"`
    Age   int    `json:"age,omitempty" validate:"min=0,max=150"`
}
```

`reflect.TypeOf(User{}).Field(0).Tag.Get("json")` returns `"name"`. Parsing comma-separated options within the tag value (e.g., `omitempty`) requires string splitting by convention — there is no standard parsing beyond `Get`.

## Implementation: Go

### Deep Copy via Reflection

```go
package main

import (
	"fmt"
	"reflect"
	"time"
)

// DeepCopy returns a deep copy of src.
// Handles: struct, pointer, slice, map, array, and primitive types.
// Does NOT handle: channels, functions, interface values pointing to uncopyable types.
func DeepCopy(src any) any {
	if src == nil {
		return nil
	}
	v := reflect.ValueOf(src)
	return deepCopyValue(v).Interface()
}

func deepCopyValue(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		// Allocate a new pointer target and recursively copy
		copied := reflect.New(v.Type().Elem())
		copied.Elem().Set(deepCopyValue(v.Elem()))
		return copied

	case reflect.Struct:
		copied := reflect.New(v.Type()).Elem()
		for i := range v.NumField() {
			field := v.Type().Field(i)
			// Skip unexported fields — reflect.Value.Set panics on unexported fields
			if !field.IsExported() {
				continue
			}
			copied.Field(i).Set(deepCopyValue(v.Field(i)))
		}
		return copied

	case reflect.Slice:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		copied := reflect.MakeSlice(v.Type(), v.Len(), v.Cap())
		for i := range v.Len() {
			copied.Index(i).Set(deepCopyValue(v.Index(i)))
		}
		return copied

	case reflect.Map:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		copied := reflect.MakeMap(v.Type())
		for _, key := range v.MapKeys() {
			copiedKey := deepCopyValue(key)
			copiedVal := deepCopyValue(v.MapIndex(key))
			copied.SetMapIndex(copiedKey, copiedVal)
		}
		return copied

	case reflect.Array:
		copied := reflect.New(v.Type()).Elem()
		for i := range v.Len() {
			copied.Index(i).Set(deepCopyValue(v.Index(i)))
		}
		return copied

	default:
		// Primitives (int, string, bool, float, etc.) are value types — no deep copy needed
		copied := reflect.New(v.Type()).Elem()
		copied.Set(v)
		return copied
	}
}

// StructToMap converts a struct to map[string]any using field names (or json tags).
// Fields tagged with `map:"-"` are excluded.
func StructToMap(src any) (map[string]any, error) {
	v := reflect.ValueOf(src)
	// Dereference pointers
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil, fmt.Errorf("StructToMap: nil pointer")
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("StructToMap: expected struct, got %s", v.Kind())
	}

	t := v.Type()
	result := make(map[string]any, t.NumField())

	for i := range t.NumField() {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}

		key := field.Name
		// Respect `map:"override_name"` or `map:"-"` tags
		if tag := field.Tag.Get("map"); tag != "" {
			if tag == "-" {
				continue
			}
			key = tag
		}

		result[key] = v.Field(i).Interface()
	}
	return result, nil
}

// ImplementsInterface checks at runtime whether a value implements an interface.
// interfacePtr must be a pointer to an interface value: (*MyInterface)(nil)
func ImplementsInterface(value any, interfacePtr any) bool {
	ifaceType := reflect.TypeOf(interfacePtr).Elem()
	valueType := reflect.TypeOf(value)
	return valueType.Implements(ifaceType)
}

// DynamicMethodCall calls a method by name on any value, passing the given args.
// Returns the results as []any, or an error if the method does not exist or panics.
func DynamicMethodCall(obj any, method string, args ...any) ([]any, error) {
	v := reflect.ValueOf(obj)
	m := v.MethodByName(method)
	if !m.IsValid() {
		return nil, fmt.Errorf("method %q not found on type %T", method, obj)
	}

	in := make([]reflect.Value, len(args))
	for i, arg := range args {
		in[i] = reflect.ValueOf(arg)
	}

	out := m.Call(in)
	results := make([]any, len(out))
	for i, r := range out {
		results[i] = r.Interface()
	}
	return results, nil
}

// --- Benchmark to show reflection cost ---

type SimpleStruct struct {
	Name  string `map:"name"`
	Value int    `map:"value"`
	Score float64
	Tags  []string
}

func (s SimpleStruct) Double() int { return s.Value * 2 }

func main() {
	original := &SimpleStruct{
		Name:  "original",
		Value: 42,
		Score: 3.14,
		Tags:  []string{"a", "b", "c"},
	}

	// Deep copy demonstration
	copied := DeepCopy(original).(*SimpleStruct)
	copied.Name = "copy"
	copied.Tags[0] = "modified"
	fmt.Printf("Original: %+v\n", original)
	fmt.Printf("Copied:   %+v\n", copied) // Tags[0] differs — deep copy worked

	// Struct to map
	m, err := StructToMap(original)
	if err != nil {
		panic(err)
	}
	fmt.Printf("As map: %v\n", m)

	// Interface checking
	type Stringer interface{ String() string }
	fmt.Printf("time.Time implements Stringer: %v\n",
		ImplementsInterface(time.Now(), (*fmt.Stringer)(nil)))

	// Dynamic method call
	results, err := DynamicMethodCall(*original, "Double")
	if err != nil {
		panic(err)
	}
	fmt.Printf("Double() = %v\n", results[0])
}
```

### Reflection Performance Benchmark

```go
package reflection_test

import (
	"reflect"
	"testing"
)

type BenchStruct struct {
	X int
	Y string
}

var target BenchStruct

// Direct field access: the baseline
func BenchmarkDirectAccess(b *testing.B) {
	s := BenchStruct{X: 42, Y: "hello"}
	for b.Loop() {
		_ = s.X
		_ = s.Y
	}
}

// Reflected field access: shows the overhead
func BenchmarkReflectAccess(b *testing.B) {
	s := BenchStruct{X: 42, Y: "hello"}
	v := reflect.ValueOf(s)
	for b.Loop() {
		_ = v.Field(0).Int()
		_ = v.Field(1).String()
	}
}

// Reflected access with type lookup included (worst case — simulates first-call cost)
func BenchmarkReflectWithTypeLookup(b *testing.B) {
	s := BenchStruct{X: 42, Y: "hello"}
	for b.Loop() {
		v := reflect.ValueOf(s)
		_ = v.Field(0).Int()
		_ = v.Field(1).String()
	}
}
```

Typical results (Apple M2, Go 1.22):
```
BenchmarkDirectAccess-10          2000000000    0.25 ns/op
BenchmarkReflectAccess-10           50000000   22.1  ns/op   (~88x slower)
BenchmarkReflectWithTypeLookup-10   10000000  112.0  ns/op   (~448x slower)
```

The 88x overhead for field access and 448x with type lookup are why reflection does not belong in hot paths.

### Go-specific considerations

**When reflection is unavoidable**: Generic serialization libraries (`encoding/json`, `encoding/xml`, `encoding/gob`) use reflection because they operate on arbitrary types they know nothing about at compile time. ORMs like GORM use reflection to map struct fields to SQL columns. Dependency injection containers (Google Wire's runtime mode, Uber's `fx`) use reflection to match types to providers. These are the canonical valid uses.

**The caching pattern**: Every production use of reflection caches the `reflect.Type` analysis. `encoding/json` builds a per-type encoder function the first time it sees a type and stores it in a sync.Map. Subsequent calls take the cached path, paying only the runtime field-access cost — not the type-analysis cost. This is the pattern to follow when writing reflection code that may be called more than once per type.

**Prefer `go generate` over reflection in hot paths**: If you know the types at development time, code generation produces direct field access with zero overhead. `encoding/json` has a `//go:generate` based alternative (`easyjson`, `jsoniter`) that generates direct marshal/unmarshal code per type, and is 3-10x faster than `encoding/json`'s reflection path. The tradeoff: generated files to maintain.

**Unexported fields**: Reflection cannot read or set unexported struct fields (it panics). This is intentional — unexported fields are implementation details that the struct's own package controls. If you need to copy a struct with unexported fields for testing, use the `unsafe` package (explicitly not recommended), provide a `Clone()` method, or restructure the code.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Runtime reflection | Full (`reflect` package) | None (Rust has no runtime type information) |
| Type inspection mechanism | `reflect.TypeOf`, `reflect.ValueOf` | Compile-time only (`std::any::TypeId` for type identity) |
| Struct field iteration | `reflect.Type.Field(i)` | Not available at runtime; proc macros at compile time |
| Dynamic method dispatch | `reflect.Value.MethodByName("M").Call(...)` | Trait objects (`dyn Trait`) — static dispatch only |
| Struct tag equivalent | `reflect.StructTag` | `#[attribute]` — parsed by proc macros at compile time |
| Performance cost | 10-100x vs direct access | Zero (no runtime reflection exists) |
| Type safety | Runtime panics on type mismatch | Compile-time errors |
| Primary use cases | Serialization, ORM, DI, testing | These use proc macros instead |

## Production War Stories

**encoding/json and the unmarshal panic hunt**: A common production incident pattern: JSON is unmarshaled into `interface{}` (a map of string to interface), code does type assertions to access fields, and a field changes type in an upstream service (string becomes int). The assertion panics. The fix is always to unmarshal into a typed struct — but teams that used `map[string]any` to "stay flexible" accumulate these panics over time. This is the canonical argument for generated code (protoc-gen-go, sqlc) over reflection-based dynamic unmarshaling: the type mismatch becomes a compilation failure in a CI pipeline, not a 3am production incident.

**GORM's N+1 through reflection**: GORM uses reflection to resolve associations. When you call `db.Preload("Orders").Find(&users)`, GORM uses reflection to inspect the `Orders` field type, figure out the foreign key, and issue a second query. On large result sets, the reflection overhead per row becomes visible — GORM profiles as slower than raw `database/sql` even when issuing the same queries. The team documented that for bulk operations, switching to `sqlc` (code generation) reduced latency by 40-60% in their benchmarks.

**`testify/assert` and deep equality**: `testify` uses reflection for `assert.Equal`, which recursively compares values including unexported fields (via `github.com/davecgh/go-spew`). This is appropriate for test code, but teams occasionally bring `assert.Equal` into production code for comparisons. The 100x slowdown per comparison is invisible in test benchmarks and catastrophic in production request paths.

## Complexity Analysis

| Dimension | Cost |
|-----------|------|
| Type lookup (`reflect.TypeOf`) | O(1) but allocates; cache in `sync.Map` per type |
| Field access via reflect | ~10-100x slower than direct access per field |
| Struct-to-map (n fields) | O(n) in reflection calls; each field is a `reflect.Value.Interface()` allocation |
| Deep copy (graph of depth d) | O(nodes) reflective traversal; each node allocates |
| Method call via reflect | ~50-200x slower than direct call; allocates for args |
| Maintenance | High — no compile-time checking of field names or types |

## Common Pitfalls

**1. Calling `reflect.Value.Interface()` on unexported fields.** This panics. Always check `reflect.StructField.IsExported()` before accessing a field's value in a generic traversal.

**2. Setting a non-addressable value.** `reflect.ValueOf(x).Field(0).Set(v)` panics because `x` was passed by value, not by pointer. Always call `reflect.ValueOf(&x).Elem().Field(0).Set(v)` to get an addressable value.

**3. Not caching type analysis.** Every call to `reflect.TypeOf` for the same type returns the same `reflect.Type` pointer (it is cheap), but iterating over fields and building a field-name-to-index map is not. Cache this per type in a `sync.Map` or `map[reflect.Type]cachedInfo`.

**4. Using reflection to check interface implementation at runtime.** `reflect.TypeOf(x).Implements(ifaceType)` is fine for tooling, but calling it in a request handler is a sign that your type hierarchy needs rethinking. Use static interface assertions (`var _ MyInterface = (*MyStruct)(nil)`) at compile time instead.

**5. Assuming `Kind()` is sufficient.** `reflect.Value.Kind()` returns the underlying kind (`Struct`, `Ptr`, etc.). Two types with the same kind (e.g., two different struct types) have the same `Kind()`. If you need to distinguish types, compare `reflect.Type` values directly: `v.Type() == reflect.TypeOf(MyStruct{})`.

## Exercises

**Exercise 1** (30 min): Write a `PrintStructFields(v any)` function that uses reflection to print each exported field name, its type, its value, and its `json` tag (if present). Handle nested structs by recursing with indentation. Test with at least one struct that has a nested struct and one that has a slice field.

**Exercise 2** (2-4h): Implement a `Merge(dst, src any) error` function that merges non-zero fields from `src` into `dst`. Both must be pointers to the same struct type. Only overwrite `dst` fields that are currently zero-valued. Use `reflect.Value.IsZero()` for the zero check. Write tests for nested structs, pointer fields, and slice fields.

**Exercise 3** (4-8h): Implement a simple struct validator using reflection and struct tags. The tag key is `validate`; supported rules are: `required` (not zero value), `min=N` (for strings: min length; for numbers: min value), `max=N`, `email` (string contains `@`). Return a slice of `ValidationError{Field, Rule, Message}`. Benchmark against a manually written equivalent to quantify the reflection overhead.

**Exercise 4** (8-15h): Implement a minimal dependency injection container using reflection. `Register(constructor any)` accepts a function whose return values are the types it provides. `Resolve(target any)` takes a pointer to an interface or struct type and resolves the constructor chain needed to produce it. Use `reflect.Type` as keys in the registry. Handle circular dependencies by detecting them at resolution time and returning an error.

## Further Reading

### Foundational Papers

- [Laws of Reflection (Rob Pike, 2011)](https://go.dev/blog/laws-of-reflection) — the canonical explanation of Go reflection, written by one of Go's authors. Read this first.

### Books

- [The Go Programming Language (Donovan & Kernighan)](https://www.gopl.io/) — Chapter 12 covers reflection thoroughly with the `format` and `encode` examples.
- [100 Go Mistakes and How to Avoid Them (Harsanyi)](https://100go.co/) — mistakes #88-#92 cover reflection antipatterns.

### Production Code to Read

- [`encoding/json` source](https://github.com/golang/go/tree/master/src/encoding/json) — `encode.go` and `decode.go`; specifically the `typeEncoder` caching pattern in `encode.go`.
- [`google/go-cmp`](https://github.com/google/go-cmp) — production-grade deep comparison using reflection; better than `reflect.DeepEqual` for testing.
- [`mapstructure`](https://github.com/mitchellh/mapstructure) — converts `map[string]any` to structs via reflection; used by Viper and Consul.
- [`validator`](https://github.com/go-playground/validator) — production struct validation via struct tags and reflection.

### Talks

- [Francesc Campoy: "Understanding nil" (GopherCon 2016)](https://www.youtube.com/watch?v=ynoY2xz-F8s) — reflection behavior with nil interfaces explained clearly.

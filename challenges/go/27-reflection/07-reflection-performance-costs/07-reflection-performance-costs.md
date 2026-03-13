# 7. Reflection Performance Costs

<!--
difficulty: advanced
concepts: [reflection-overhead, benchmark-comparison, type-caching, code-generation-alternative, hot-path-reflection]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [reflect-typeof-valueof, inspecting-struct-fields-tags, benchmarking-methodology]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of reflection basics (TypeOf, ValueOf, field iteration)
- Experience writing Go benchmarks

## Learning Objectives

After completing this exercise, you will be able to:

- **Measure** the concrete performance cost of reflection operations
- **Compare** reflection-based code against direct and interface-based alternatives
- **Apply** caching strategies to reduce repeated reflection overhead
- **Decide** when reflection is acceptable and when to avoid it

## Why Understanding Reflection Performance Costs

Reflection is powerful but not free. Every `reflect.ValueOf` call boxes the value into an interface, every `FieldByName` performs a linear scan, and every `Call` must validate arguments at runtime. On hot paths handling millions of requests per second, these costs matter. On cold paths executed once during initialization, they are irrelevant. Benchmarking both tells you exactly where the boundary is.

## The Problem

Benchmark reflection operations against their direct equivalents to quantify the overhead. Then apply caching to reduce repeated reflection costs.

## Requirements

1. Benchmark field access: direct vs `FieldByName` vs `Field(i)` vs cached index
2. Benchmark method calls: direct vs `MethodByName` + `Call`
3. Benchmark struct creation: direct vs `reflect.New` + `Set`
4. Implement a type-info cache that eliminates repeated `TypeOf` and `FieldByName` lookups

## Step 1 -- Benchmark Field Access

```bash
mkdir -p ~/go-exercises/reflect-perf && cd ~/go-exercises/reflect-perf
go mod init reflect-perf
```

Create `bench_test.go`:

```go
package main

import (
	"reflect"
	"testing"
)

type User struct {
	ID    int
	Name  string
	Email string
	Age   int
	Admin bool
}

var sampleUser = User{
	ID: 1, Name: "Alice", Email: "alice@example.com", Age: 30, Admin: true,
}

// Direct field access
func BenchmarkDirectField(b *testing.B) {
	u := sampleUser
	var s string
	for i := 0; i < b.N; i++ {
		s = u.Name
	}
	_ = s
}

// reflect.Value.FieldByName (includes lookup cost)
func BenchmarkReflectFieldByName(b *testing.B) {
	u := sampleUser
	var s string
	for i := 0; i < b.N; i++ {
		v := reflect.ValueOf(u)
		s = v.FieldByName("Name").String()
	}
	_ = s
}

// reflect.Value.Field(index) (skip lookup)
func BenchmarkReflectFieldByIndex(b *testing.B) {
	u := sampleUser
	nameIndex := 1 // pre-computed
	var s string
	for i := 0; i < b.N; i++ {
		v := reflect.ValueOf(u)
		s = v.Field(nameIndex).String()
	}
	_ = s
}

// Cached reflect.Value (reuse ValueOf)
func BenchmarkReflectCachedValue(b *testing.B) {
	u := sampleUser
	v := reflect.ValueOf(u)
	nameIndex := 1
	var s string
	for i := 0; i < b.N; i++ {
		s = v.Field(nameIndex).String()
	}
	_ = s
}
```

```bash
go test -bench=BenchmarkDirect -benchmem
go test -bench=BenchmarkReflect -benchmem
```

### Intermediate Verification

Direct field access is essentially free (sub-nanosecond). `FieldByName` is the slowest due to string lookup. `Field(index)` with cached `ValueOf` is the fastest reflection path.

## Step 2 -- Benchmark Method Calls

```go
type Calculator struct{}

func (c Calculator) Add(a, b int) int { return a + b }

func BenchmarkDirectMethodCall(b *testing.B) {
	c := Calculator{}
	var result int
	for i := 0; i < b.N; i++ {
		result = c.Add(3, 4)
	}
	_ = result
}

func BenchmarkReflectMethodCall(b *testing.B) {
	c := Calculator{}
	v := reflect.ValueOf(c)
	method := v.MethodByName("Add")
	args := []reflect.Value{reflect.ValueOf(3), reflect.ValueOf(4)}
	var result int
	for i := 0; i < b.N; i++ {
		out := method.Call(args)
		result = int(out[0].Int())
	}
	_ = result
}

func BenchmarkReflectMethodLookupAndCall(b *testing.B) {
	c := Calculator{}
	var result int
	for i := 0; i < b.N; i++ {
		v := reflect.ValueOf(c)
		method := v.MethodByName("Add")
		args := []reflect.Value{reflect.ValueOf(3), reflect.ValueOf(4)}
		out := method.Call(args)
		result = int(out[0].Int())
	}
	_ = result
}
```

## Step 3 -- Type Info Cache

Build a cache that stores field indices by name, eliminating repeated lookups:

```go
package main

import (
	"reflect"
	"sync"
)

type FieldCache struct {
	mu     sync.RWMutex
	cache  map[reflect.Type]map[string]int
}

func NewFieldCache() *FieldCache {
	return &FieldCache{
		cache: make(map[reflect.Type]map[string]int),
	}
}

func (fc *FieldCache) GetFieldIndex(t reflect.Type, fieldName string) (int, bool) {
	fc.mu.RLock()
	fields, ok := fc.cache[t]
	fc.mu.RUnlock()

	if !ok {
		fields = fc.buildCache(t)
	}

	idx, found := fields[fieldName]
	return idx, found
}

func (fc *FieldCache) buildCache(t reflect.Type) map[string]int {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	// Double-check after acquiring write lock
	if fields, ok := fc.cache[t]; ok {
		return fields
	}

	fields := make(map[string]int, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		fields[t.Field(i).Name] = i
	}
	fc.cache[t] = fields
	return fields
}
```

Benchmark the cached version:

```go
var globalCache = NewFieldCache()

func BenchmarkCachedFieldAccess(b *testing.B) {
	u := sampleUser
	t := reflect.TypeOf(u)
	var s string
	for i := 0; i < b.N; i++ {
		idx, _ := globalCache.GetFieldIndex(t, "Name")
		v := reflect.ValueOf(u)
		s = v.Field(idx).String()
	}
	_ = s
}
```

## Step 4 -- Interface-Based Alternative

Compare reflection against an interface-based approach:

```go
type FieldGetter interface {
	GetField(name string) interface{}
}

func (u User) GetField(name string) interface{} {
	switch name {
	case "ID":
		return u.ID
	case "Name":
		return u.Name
	case "Email":
		return u.Email
	case "Age":
		return u.Age
	case "Admin":
		return u.Admin
	default:
		return nil
	}
}

func BenchmarkInterfaceFieldAccess(b *testing.B) {
	u := sampleUser
	var s interface{}
	for i := 0; i < b.N; i++ {
		s = u.GetField("Name")
	}
	_ = s
}
```

## Hints

- `reflect.ValueOf` allocates when the value escapes to the heap
- `FieldByName` does a linear scan of field names on every call
- Caching `reflect.Type` is safe since types are immutable and globally unique
- The `Call` overhead comes from argument validation and slice allocation
- On cold paths (startup, config loading), reflection overhead is negligible
- On hot paths (per-request, per-row), prefer interfaces or code generation

## Verification

- Direct field access is 100-1000x faster than `FieldByName` reflection
- Cached field index access reduces the reflection overhead significantly
- `MethodByName` + `Call` is ~100x slower than direct method calls
- The interface-based alternative sits between direct and reflection
- `b.ReportAllocs()` shows reflection allocates while direct access does not

## What's Next

Understanding the cost of reflection sets the stage for the capstone exercises. The next exercise builds a complete ORM layer using reflection.

## Summary

Reflection operations are 50-500x slower than direct equivalents due to runtime type checking, `interface{}` boxing, and string-based lookups. Reduce the cost by caching `reflect.Type` and field indices, pre-looking up methods, and reusing `reflect.Value`. On hot paths, prefer interfaces or code generation. On cold paths (init, config, schema), reflection overhead is acceptable.

## Reference

- [reflect package performance notes](https://pkg.go.dev/reflect)
- [Go testing benchmarks](https://pkg.go.dev/testing#hdr-Benchmarks)
- [Benchmark flags: -bench, -benchmem, -count](https://pkg.go.dev/cmd/go#hdr-Testing_flags)

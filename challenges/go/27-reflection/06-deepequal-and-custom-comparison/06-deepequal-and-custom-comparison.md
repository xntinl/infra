# 6. DeepEqual and Custom Comparison

<!--
difficulty: advanced
concepts: [deepequal, reflect-comparison, custom-equality, cmp-package, diff-generation]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [reflect-typeof-valueof, inspecting-struct-fields-tags, testing-fundamentals]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of `reflect.TypeOf` and `reflect.ValueOf`
- Experience writing tests in Go

## Learning Objectives

After completing this exercise, you will be able to:

- **Use** `reflect.DeepEqual` to compare complex data structures
- **Identify** the limitations and edge cases of `DeepEqual`
- **Build** custom comparison functions using reflection
- **Apply** the `google/go-cmp` package for readable test diffs

## Why DeepEqual and Custom Comparison

Testing often requires comparing complex nested structures: slices of structs, maps with pointer values, deeply nested JSON responses. The `==` operator only works on comparable types and does not recurse into slices or maps. `reflect.DeepEqual` fills this gap, but it has sharp edges: it considers nil and empty slices different, treats unexported fields as comparable (and panics if they differ in some cases), and provides no diff output. Understanding these nuances and knowing when to use `go-cmp` instead is essential for robust test assertions.

## The Problem

Build a comparison utility that handles common testing scenarios, including ignoring certain fields, tolerating floating-point differences, and generating human-readable diffs.

## Requirements

1. Demonstrate `reflect.DeepEqual` behavior with slices, maps, pointers, and nil values
2. Identify at least 3 edge cases where `DeepEqual` surprises developers
3. Build a custom comparator that ignores specified fields
4. Use `go-cmp` to produce readable diffs

## Step 1 -- DeepEqual Behavior

```bash
mkdir -p ~/go-exercises/deep-compare && cd ~/go-exercises/deep-compare
go mod init deep-compare
```

Create `compare_test.go`:

```go
package main

import (
	"reflect"
	"testing"
	"time"
)

func TestDeepEqualBasics(t *testing.T) {
	// Slices
	a := []int{1, 2, 3}
	b := []int{1, 2, 3}
	if !reflect.DeepEqual(a, b) {
		t.Error("identical slices should be equal")
	}

	// Maps
	m1 := map[string]int{"a": 1, "b": 2}
	m2 := map[string]int{"b": 2, "a": 1}
	if !reflect.DeepEqual(m1, m2) {
		t.Error("maps with same entries should be equal regardless of order")
	}

	// Structs
	type Point struct{ X, Y int }
	p1 := Point{1, 2}
	p2 := Point{1, 2}
	if !reflect.DeepEqual(p1, p2) {
		t.Error("identical structs should be equal")
	}
}

func TestDeepEqualEdgeCases(t *testing.T) {
	// Edge case 1: nil slice vs empty slice
	var nilSlice []int
	emptySlice := []int{}
	if reflect.DeepEqual(nilSlice, emptySlice) {
		t.Error("DeepEqual should consider nil and empty slices different")
	}
	t.Logf("nil slice == empty slice: %v", reflect.DeepEqual(nilSlice, emptySlice))

	// Edge case 2: nil map vs empty map
	var nilMap map[string]int
	emptyMap := map[string]int{}
	if reflect.DeepEqual(nilMap, emptyMap) {
		t.Error("DeepEqual should consider nil and empty maps different")
	}

	// Edge case 3: time.Time with different locations
	t1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.FixedZone("UTC", 0))
	equal := reflect.DeepEqual(t1, t2)
	t.Logf("Same instant, different Location: DeepEqual=%v", equal)
	// DeepEqual compares Location pointers, so this may return false
}

func TestDeepEqualPointers(t *testing.T) {
	x := 42
	y := 42
	// DeepEqual follows pointers
	if !reflect.DeepEqual(&x, &y) {
		t.Error("pointers to equal values should be deeply equal")
	}

	// Pointer to different values
	z := 99
	if reflect.DeepEqual(&x, &z) {
		t.Error("pointers to different values should not be equal")
	}
}
```

```bash
go test -v -run TestDeepEqual
```

## Step 2 -- Custom Comparator with Field Ignoring

```go
package main

import (
	"reflect"
)

// EqualIgnoring compares two structs, ignoring the specified field names.
func EqualIgnoring(a, b interface{}, ignoreFields ...string) bool {
	va := reflect.ValueOf(a)
	vb := reflect.ValueOf(b)

	if va.Type() != vb.Type() {
		return false
	}
	if va.Kind() == reflect.Ptr {
		va = va.Elem()
		vb = vb.Elem()
	}
	if va.Kind() != reflect.Struct {
		return reflect.DeepEqual(a, b)
	}

	ignore := make(map[string]bool)
	for _, f := range ignoreFields {
		ignore[f] = true
	}

	t := va.Type()
	for i := 0; i < t.NumField(); i++ {
		if ignore[t.Field(i).Name] {
			continue
		}
		if !reflect.DeepEqual(va.Field(i).Interface(), vb.Field(i).Interface()) {
			return false
		}
	}
	return true
}
```

Test it:

```go
func TestEqualIgnoring(t *testing.T) {
	type Record struct {
		ID        int
		Name      string
		UpdatedAt time.Time
	}

	r1 := Record{ID: 1, Name: "Alice", UpdatedAt: time.Now()}
	r2 := Record{ID: 1, Name: "Alice", UpdatedAt: time.Now().Add(time.Second)}

	if reflect.DeepEqual(r1, r2) {
		t.Error("records with different timestamps should not be equal")
	}

	if !EqualIgnoring(r1, r2, "UpdatedAt") {
		t.Error("records should be equal when ignoring UpdatedAt")
	}
}
```

## Step 3 -- Using go-cmp for Test Diffs

```bash
go get github.com/google/go-cmp/cmp
```

```go
package main

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

type User struct {
	Name      string
	Email     string
	Age       int
	Tags      []string
	CreatedAt time.Time
}

func TestGoCmpDiff(t *testing.T) {
	want := User{
		Name:  "Alice",
		Email: "alice@example.com",
		Age:   30,
		Tags:  []string{"admin", "active"},
	}
	got := User{
		Name:  "Alice",
		Email: "alice@corp.com",
		Age:   31,
		Tags:  []string{"admin", "inactive"},
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("User mismatch (-want +got):\n%s", diff)
	}
}

func TestGoCmpOptions(t *testing.T) {
	a := User{Name: "Alice", CreatedAt: time.Now()}
	b := User{Name: "Alice", CreatedAt: time.Now().Add(time.Millisecond)}

	// Ignore CreatedAt field
	if diff := cmp.Diff(a, b, cmpopts.IgnoreFields(User{}, "CreatedAt")); diff != "" {
		t.Errorf("mismatch:\n%s", diff)
	}

	// Treat nil and empty slices as equal
	x := User{Name: "Bob", Tags: nil}
	y := User{Name: "Bob", Tags: []string{}}
	if diff := cmp.Diff(x, y, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("nil vs empty mismatch:\n%s", diff)
	}

	// Float tolerance
	type Measurement struct {
		Value float64
	}
	m1 := Measurement{Value: 3.14159}
	m2 := Measurement{Value: 3.14160}
	if diff := cmp.Diff(m1, m2, cmpopts.EquateApprox(0, 0.001)); diff != "" {
		t.Errorf("float mismatch:\n%s", diff)
	}
}
```

```bash
go test -v -run TestGoCmp
```

## Hints

- `reflect.DeepEqual` follows pointers and compares their targets
- nil slice and empty slice are NOT equal according to `DeepEqual`
- `time.Time` comparison with `DeepEqual` depends on the `Location` pointer, not just the instant
- `go-cmp` produces readable diffs; prefer it over `DeepEqual` in tests
- `cmpopts.IgnoreFields`, `cmpopts.EquateEmpty`, and `cmpopts.EquateApprox` cover the most common edge cases
- Unexported fields cause `go-cmp` to panic unless you use `cmpopts.IgnoreUnexported`

## Verification

- `DeepEqual` correctly compares slices, maps, and structs
- nil vs empty slice returns `false` from `DeepEqual`
- `EqualIgnoring` skips specified fields and compares the rest
- `go-cmp` produces human-readable diffs showing exactly which fields differ
- Float comparison with tolerance works via `cmpopts.EquateApprox`

## What's Next

With comparison utilities mastered, the next exercise benchmarks the performance cost of reflection to understand when it is and is not appropriate.

## Summary

`reflect.DeepEqual` recursively compares values including slices, maps, and pointer targets. Its edge cases -- nil vs empty, `time.Time` locations, unexported fields -- make it unreliable for tests. Build custom comparators for field-ignoring scenarios. Use `google/go-cmp` in tests for readable diffs, field ignoring, float tolerance, and nil/empty equivalence.

## Reference

- [reflect.DeepEqual](https://pkg.go.dev/reflect#DeepEqual)
- [google/go-cmp](https://pkg.go.dev/github.com/google/go-cmp/cmp)
- [cmpopts package](https://pkg.go.dev/github.com/google/go-cmp/cmp/cmpopts)

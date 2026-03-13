# 24. Property-Based Testing with rapid

<!--
difficulty: insane
concepts: [property-based-testing, rapid, generators, shrinking, invariants, stateful-testing, model-checking]
tools: [go test, rapid]
estimated_time: 60m
bloom_level: create
prerequisites: [01-your-first-test, 02-table-driven-tests, 06-fuzz-testing, 14-parallel-tests]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01, 02, 06, and 14 in this section
- Understanding of fuzz testing concepts (invariant checking, property verification)
- Familiarity with `testing.F` and `f.Fuzz`

## The Challenge

Property-based testing (PBT) generates thousands of random inputs and verifies that invariants hold across all of them. Unlike example-based tests that check specific input-output pairs, PBT discovers edge cases you never imagined. When a property fails, the framework automatically "shrinks" the failing input to the minimal reproducing case -- this is the key advantage over Go's built-in fuzz testing, which finds a failing input but does not minimize it.

Build a comprehensive PBT suite for a sorted set data structure and a binary codec. Use the `pgregory.net/rapid` library. The tests should define mathematical properties (idempotency, commutativity, roundtrip correctness) rather than checking specific values. The hardest part is choosing strong properties. "It doesn't crash" is weak. "Inserting into a sorted set preserves sorted order" is strong.

## Requirements

1. **Implement a `SortedSet` data structure** that maintains a sorted slice of unique integers with `Add`, `Remove`, `Contains`, `Len`, `Slice`, `Union`, `Intersection`, `Difference`, `Equal`, `Encode`, and `Decode`:

```go
// sortedset.go
package sortedset

import (
	"fmt"
	"sort"
)

type SortedSet struct {
	items []int
}

func New() *SortedSet {
	return &SortedSet{}
}

func FromSlice(vals []int) *SortedSet {
	s := New()
	for _, v := range vals {
		s.Add(v)
	}
	return s
}

func (s *SortedSet) Add(val int) {
	i := sort.SearchInts(s.items, val)
	if i < len(s.items) && s.items[i] == val {
		return
	}
	s.items = append(s.items, 0)
	copy(s.items[i+1:], s.items[i:])
	s.items[i] = val
}

func (s *SortedSet) Remove(val int) {
	i := sort.SearchInts(s.items, val)
	if i < len(s.items) && s.items[i] == val {
		s.items = append(s.items[:i], s.items[i+1:]...)
	}
}

func (s *SortedSet) Contains(val int) bool {
	i := sort.SearchInts(s.items, val)
	return i < len(s.items) && s.items[i] == val
}

func (s *SortedSet) Len() int { return len(s.items) }

func (s *SortedSet) Slice() []int {
	out := make([]int, len(s.items))
	copy(out, s.items)
	return out
}

func (s *SortedSet) Union(other *SortedSet) *SortedSet {
	result := New()
	i, j := 0, 0
	for i < len(s.items) && j < len(other.items) {
		if s.items[i] < other.items[j] {
			result.items = append(result.items, s.items[i])
			i++
		} else if s.items[i] > other.items[j] {
			result.items = append(result.items, other.items[j])
			j++
		} else {
			result.items = append(result.items, s.items[i])
			i++
			j++
		}
	}
	result.items = append(result.items, s.items[i:]...)
	result.items = append(result.items, other.items[j:]...)
	return result
}

func (s *SortedSet) Intersection(other *SortedSet) *SortedSet {
	result := New()
	i, j := 0, 0
	for i < len(s.items) && j < len(other.items) {
		if s.items[i] < other.items[j] {
			i++
		} else if s.items[i] > other.items[j] {
			j++
		} else {
			result.items = append(result.items, s.items[i])
			i++
			j++
		}
	}
	return result
}

func (s *SortedSet) Difference(other *SortedSet) *SortedSet {
	result := New()
	i, j := 0, 0
	for i < len(s.items) && j < len(other.items) {
		if s.items[i] < other.items[j] {
			result.items = append(result.items, s.items[i])
			i++
		} else if s.items[i] > other.items[j] {
			j++
		} else {
			i++
			j++
		}
	}
	result.items = append(result.items, s.items[i:]...)
	return result
}

func (s *SortedSet) Equal(other *SortedSet) bool {
	if len(s.items) != len(other.items) {
		return false
	}
	for i := range s.items {
		if s.items[i] != other.items[i] {
			return false
		}
	}
	return true
}

func (s *SortedSet) Encode() []int {
	out := make([]int, 0, len(s.items)+1)
	out = append(out, len(s.items))
	out = append(out, s.items...)
	return out
}

func Decode(data []int) (*SortedSet, error) {
	if len(data) == 0 {
		return New(), nil
	}
	n := data[0]
	if n < 0 || n+1 > len(data) {
		return nil, fmt.Errorf("invalid data: length %d but only %d elements", n, len(data)-1)
	}
	s := &SortedSet{items: make([]int, n)}
	copy(s.items, data[1:1+n])
	return s, nil
}
```

2. **Write property-based tests** using `rapid` that verify algebraic properties. Install the library and create a custom generator:

```go
// sortedset_prop_test.go
package sortedset

import (
	"sort"
	"testing"

	"pgregory.net/rapid"
)

func genSortedSet() *rapid.Generator[*SortedSet] {
	return rapid.Custom(func(t *rapid.T) *SortedSet {
		vals := rapid.SliceOf(rapid.IntRange(-1000, 1000)).Draw(t, "values")
		return FromSlice(vals)
	})
}

func TestProperty_AlwaysSorted(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := genSortedSet().Draw(t, "set")
		items := s.Slice()
		if !sort.IntsAreSorted(items) {
			t.Fatalf("set is not sorted: %v", items)
		}
	})
}

func TestProperty_NoDuplicates(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := genSortedSet().Draw(t, "set")
		items := s.Slice()
		seen := make(map[int]bool)
		for _, v := range items {
			if seen[v] {
				t.Fatalf("duplicate value %d in set %v", v, items)
			}
			seen[v] = true
		}
	})
}

func TestProperty_AddIdempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := genSortedSet().Draw(t, "set")
		val := rapid.IntRange(-1000, 1000).Draw(t, "value")
		s.Add(val)
		lenAfterFirst := s.Len()
		s.Add(val)
		if s.Len() != lenAfterFirst {
			t.Fatalf("Add(%d) is not idempotent: len %d then %d", val, lenAfterFirst, s.Len())
		}
	})
}

func TestProperty_AddContains(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := genSortedSet().Draw(t, "set")
		val := rapid.IntRange(-1000, 1000).Draw(t, "value")
		s.Add(val)
		if !s.Contains(val) {
			t.Fatalf("after Add(%d), Contains returns false", val)
		}
	})
}

func TestProperty_RemoveNotContains(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := genSortedSet().Draw(t, "set")
		val := rapid.IntRange(-1000, 1000).Draw(t, "value")
		s.Remove(val)
		if s.Contains(val) {
			t.Fatalf("after Remove(%d), Contains returns true", val)
		}
	})
}

func TestProperty_UnionCommutative(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := genSortedSet().Draw(t, "a")
		b := genSortedSet().Draw(t, "b")
		if !a.Union(b).Equal(b.Union(a)) {
			t.Fatalf("Union not commutative: a=%v b=%v", a.Slice(), b.Slice())
		}
	})
}

func TestProperty_IntersectionCommutative(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := genSortedSet().Draw(t, "a")
		b := genSortedSet().Draw(t, "b")
		if !a.Intersection(b).Equal(b.Intersection(a)) {
			t.Fatalf("Intersection not commutative: a=%v b=%v", a.Slice(), b.Slice())
		}
	})
}

func TestProperty_UnionAssociative(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := genSortedSet().Draw(t, "a")
		b := genSortedSet().Draw(t, "b")
		c := genSortedSet().Draw(t, "c")
		lhs := a.Union(b).Union(c)
		rhs := a.Union(b.Union(c))
		if !lhs.Equal(rhs) {
			t.Fatalf("Union not associative")
		}
	})
}

func TestProperty_UnionSelfIsIdentity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := genSortedSet().Draw(t, "set")
		if !s.Union(s).Equal(s) {
			t.Fatalf("s | s != s: %v", s.Slice())
		}
	})
}

func TestProperty_IntersectionSelfIsIdentity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := genSortedSet().Draw(t, "set")
		if !s.Intersection(s).Equal(s) {
			t.Fatalf("s & s != s: %v", s.Slice())
		}
	})
}

func TestProperty_DifferenceSelfIsEmpty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := genSortedSet().Draw(t, "set")
		d := s.Difference(s)
		if d.Len() != 0 {
			t.Fatalf("s - s is not empty: %v", d.Slice())
		}
	})
}

func TestProperty_UnionSizeBound(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := genSortedSet().Draw(t, "a")
		b := genSortedSet().Draw(t, "b")
		u := a.Union(b)
		if u.Len() > a.Len()+b.Len() {
			t.Fatalf("|a|b| = %d > |a|+|b| = %d", u.Len(), a.Len()+b.Len())
		}
	})
}

func TestProperty_IntersectionSubset(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := genSortedSet().Draw(t, "a")
		b := genSortedSet().Draw(t, "b")
		inter := a.Intersection(b)
		for _, v := range inter.Slice() {
			if !a.Contains(v) || !b.Contains(v) {
				t.Fatalf("intersection element %d not in both sets", v)
			}
		}
	})
}

func TestProperty_EncodeDecodeRoundtrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := genSortedSet().Draw(t, "set")
		encoded := s.Encode()
		decoded, err := Decode(encoded)
		if err != nil {
			t.Fatalf("Decode failed: %v", err)
		}
		if !s.Equal(decoded) {
			t.Fatalf("roundtrip failed: %v -> %v -> %v", s.Slice(), encoded, decoded.Slice())
		}
	})
}

func TestProperty_DecodeArbitraryNoPanic(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		data := rapid.SliceOf(rapid.IntRange(-10, 100)).Draw(t, "data")
		// Must not panic regardless of input
		_, _ = Decode(data)
	})
}
```

3. **Write a stateful property test** that models `SortedSet` operations against a reference `map[int]bool`:

```go
// sortedset_stateful_test.go
package sortedset

import (
	"sort"
	"testing"

	"pgregory.net/rapid"
)

func TestStateful_SortedSetVsMap(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		set := New()
		model := make(map[int]bool)

		nOps := rapid.IntRange(1, 200).Draw(t, "nOps")
		for i := 0; i < nOps; i++ {
			op := rapid.IntRange(0, 2).Draw(t, "op")
			val := rapid.IntRange(-100, 100).Draw(t, "val")

			switch op {
			case 0: // Add
				set.Add(val)
				model[val] = true
			case 1: // Remove
				set.Remove(val)
				delete(model, val)
			case 2: // Contains
				got := set.Contains(val)
				want := model[val]
				if got != want {
					t.Fatalf("Contains(%d): set=%v, model=%v after %d ops", val, got, want, i+1)
				}
			}
		}

		// Final consistency check
		if set.Len() != len(model) {
			t.Fatalf("Len mismatch: set=%d, model=%d", set.Len(), len(model))
		}
		for val := range model {
			if !set.Contains(val) {
				t.Fatalf("model has %d but set does not", val)
			}
		}
		items := set.Slice()
		if !sort.IntsAreSorted(items) {
			t.Fatalf("set is not sorted after %d operations: %v", nOps, items)
		}
	})
}
```

4. **Compare `rapid` with `testing.F`** by writing the same roundtrip property both ways:

```go
// compare_test.go
package sortedset

import (
	"testing"

	"pgregory.net/rapid"
)

// testing.F version: fuzz the roundtrip with raw bytes
func FuzzEncodeDecodeRoundtrip(f *testing.F) {
	f.Add([]byte{0})
	f.Add([]byte{3, 0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0})

	f.Fuzz(func(t *testing.T, data []byte) {
		// Convert bytes to ints (4 bytes each)
		if len(data)%4 != 0 {
			return
		}
		ints := make([]int, len(data)/4)
		for i := range ints {
			ints[i] = int(data[i*4])<<24 | int(data[i*4+1])<<16 |
				int(data[i*4+2])<<8 | int(data[i*4+3])
		}
		// Must not panic
		_, _ = Decode(ints)
	})
}

// rapid version: test with structured int slices (better coverage)
func TestRapid_EncodeDecodeStructured(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := genSortedSet().Draw(t, "set")
		encoded := s.Encode()
		decoded, err := Decode(encoded)
		if err != nil {
			t.Fatalf("Decode(Encode(s)) failed: %v", err)
		}
		if !s.Equal(decoded) {
			t.Fatalf("roundtrip mismatch")
		}
	})
}
```

Key differences to document:
- `testing.F` works with raw bytes/strings; `rapid` works with structured types
- `rapid` shrinks failing inputs to minimal examples; `testing.F` does not
- `testing.F` is built into Go; `rapid` is an external dependency
- `rapid` runs as a regular `Test` function; `testing.F` requires `-fuzz` flag for generation

## Hints

- Install rapid: `go get pgregory.net/rapid`
- `rapid.Check` runs 100 iterations by default. Use `-rapid.checks=10000` for thorough testing.
- Custom generators compose: `rapid.Custom(func(t *rapid.T) MyType { ... })` can call other generators internally.
- When a property fails, `rapid` prints the seed and the shrunk minimal input. Include the seed in bug reports.
- For stateful testing, generate a sequence of operations and apply them to both the real implementation and a simple reference model (like `map[int]bool`).
- Properties should be fast -- each `rapid.Check` runs 100+ iterations. Avoid I/O or expensive setup inside property functions.
- Strong property categories: algebraic (commutativity, associativity, idempotence), roundtrip (encode/decode), model-based (compare against reference), size bounds (|A union B| <= |A| + |B|), invariant preservation (always sorted).

## Success Criteria

1. All algebraic properties (commutativity, associativity, idempotence, identity) pass for 1000+ generated inputs
2. The encode/decode roundtrip property passes for all generated sets including empty sets and single-element sets
3. The stateful test runs 200-operation sequences and catches any inconsistency between `SortedSet` and the map model
4. `Decode` with arbitrary random data never panics (verified by both `rapid` and `testing.F`)
5. If you introduce a bug (e.g., comment out the duplicate check in `Add`), `rapid` catches it and shrinks to a minimal 2-3 element example
6. The comparison documents concrete differences between `rapid` and `testing.F` with code examples

## Research Resources

- [pgregory.net/rapid](https://pkg.go.dev/pgregory.net/rapid) -- Go property-based testing library with integrated shrinking
- [rapid GitHub](https://github.com/flyingmutant/rapid) -- examples and documentation
- [John Hughes: QuickCheck Testing for Fun and Profit](https://www.youtube.com/watch?v=zi0rHwfiX1Q) -- the seminal talk on property-based testing
- [Choosing properties for property-based testing](https://fsharpforfunandprofit.com/posts/property-based-testing-2/) -- how to think about properties (F# examples, concepts are universal)
- [Go testing.F documentation](https://go.dev/doc/security/fuzz/) -- built-in fuzz testing
- [rapid vs testing.F comparison](https://github.com/flyingmutant/rapid#comparison-with-testingf) -- when to use which

## What's Next

Continue to [25 - Building a Test Suite for a Production Service](../25-building-a-test-suite/25-building-a-test-suite.md) to bring together everything you have learned into a comprehensive test suite for a realistic service.

## Summary

- Property-based testing verifies invariants over thousands of generated inputs
- `rapid` provides custom generators, automatic shrinking, and stateful testing
- Algebraic properties (commutativity, associativity, idempotence) encode mathematical truths about your data structures
- Roundtrip properties verify encode/decode and serialize/deserialize pairs
- Stateful testing compares your implementation against a simple reference model over random operation sequences
- Shrinking reduces failing inputs to minimal examples, making bugs easier to diagnose
- Property-based testing complements (not replaces) example-based tests and fuzz testing
- `rapid` is more expressive than `testing.F` (structured generators, shrinking) at the cost of an external dependency

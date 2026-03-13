# 7. Method Sets and Addressability

<!--
difficulty: intermediate
concepts: [method-sets, addressability, T-vs-pointer-T, interface-satisfaction-rules]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [methods-value-vs-pointer-receivers, implicit-interface-satisfaction]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercise 03 (Methods: Value vs Pointer Receivers)
- Basic understanding of interfaces

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** the rules governing which methods belong to `T` vs `*T` method sets
- **Analyze** why a value type fails to satisfy an interface that a pointer type satisfies
- **Determine** when a value is addressable and how that affects method calls

## Why Method Sets and Addressability

The method set of a type determines which interfaces it satisfies. This is one of Go's most subtle and commonly misunderstood rules. The method set of `T` includes only value receiver methods. The method set of `*T` includes both value and pointer receiver methods. This asymmetry has practical consequences: a value stored in an interface cannot have its address taken, so pointer receiver methods are unreachable.

Understanding method sets prevents frustrating "does not implement" compiler errors and helps you make deliberate choices about receiver types.

## Step 1 -- The Method Set Rule

```bash
mkdir -p ~/go-exercises/method-sets
cd ~/go-exercises/method-sets
go mod init method-sets
```

Create `main.go`:

```go
package main

import "fmt"

type Speaker interface {
	Speak() string
}

type Dog struct {
	Name string
}

// Value receiver -- in method set of BOTH Dog and *Dog
func (d Dog) Speak() string {
	return fmt.Sprintf("%s says: Woof!", d.Name)
}

func announce(s Speaker) {
	fmt.Println(s.Speak())
}

func main() {
	dog := Dog{Name: "Rex"}
	pdog := &Dog{Name: "Buddy"}

	// Both work because Speak has a value receiver
	announce(dog)  // Dog value
	announce(pdog) // *Dog pointer
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Rex says: Woof!
Buddy says: Woof!
```

## Step 2 -- Pointer Receiver Restricts the Method Set

```go
type Mutable interface {
	SetName(name string)
}

// Pointer receiver -- only in method set of *Dog, NOT Dog
func (d *Dog) SetName(name string) {
	d.Name = name
}

func rename(m Mutable, name string) {
	m.SetName(name)
}

func main() {
	dog := Dog{Name: "Rex"}
	pdog := &Dog{Name: "Buddy"}

	// This works -- *Dog has SetName in its method set
	rename(pdog, "Max")
	fmt.Printf("Renamed: %s\n", pdog.Name)

	// This does NOT compile:
	// rename(dog, "Max")
	// Error: Dog does not implement Mutable (SetName method has pointer receiver)

	// But direct method calls on addressable values work:
	dog.SetName("Fido") // Go auto-takes &dog
	fmt.Printf("Direct call: %s\n", dog.Name)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Renamed: Max
Direct call: Fido
```

## Step 3 -- Addressability Explained

A value is addressable if you can take its address with `&`. Most local variables and struct fields are addressable. Map values and function return values are NOT addressable.

```go
package main

import "fmt"

type Counter struct {
	N int
}

func (c *Counter) Increment() {
	c.N++
}

func main() {
	// Addressable: local variable
	c := Counter{N: 0}
	c.Increment() // Go takes &c automatically
	fmt.Printf("Local variable: %d\n", c.N)

	// NOT addressable: map value
	counters := map[string]Counter{
		"hits": {N: 10},
	}
	// counters["hits"].Increment() // COMPILE ERROR: cannot take address of map value

	// Workaround: use a map of pointers
	pcounters := map[string]*Counter{
		"hits": {N: 10},
	}
	pcounters["hits"].Increment()
	fmt.Printf("Map pointer: %d\n", pcounters["hits"].N)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Local variable: 1
Map pointer: 11
```

## Step 4 -- Putting It Together with Interfaces

```go
package main

import "fmt"

type Stringer interface {
	String() string
}

type Resettable interface {
	Reset()
}

type Timer struct {
	Label   string
	Seconds int
}

func (t Timer) String() string {
	return fmt.Sprintf("%s: %ds", t.Label, t.Seconds)
}

func (t *Timer) Reset() {
	t.Seconds = 0
}

func main() {
	t := Timer{Label: "elapsed", Seconds: 42}

	// Timer satisfies Stringer (value receiver)
	var s Stringer = t
	fmt.Println(s.String())

	// *Timer satisfies both Stringer and Resettable
	var r Resettable = &t
	r.Reset()
	fmt.Println(t.String())

	// Timer does NOT satisfy Resettable:
	// var r2 Resettable = t  // COMPILE ERROR
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
elapsed: 42s
elapsed: 0s
```

## Common Mistakes

### Expecting Value Types to Satisfy Pointer-Receiver Interfaces

The compiler error message is:

```
cannot use t (variable of type Timer) as Resettable value in variable declaration:
    Timer does not implement Resettable (method Reset has pointer receiver)
```

Remember: `T`'s method set has only value receivers. `*T`'s method set has both.

### Confusing Method Calls with Interface Satisfaction

Direct method calls auto-take addresses (`t.Reset()` works even though `t` is not a pointer). Interface satisfaction does NOT -- the value stored inside the interface cannot be addressed.

## Verify What You Learned

1. Define an interface `Formatter` with method `Format() string` (value receiver)
2. Define an interface `Parser` with method `Parse(s string)` (pointer receiver)
3. Verify that a value satisfies `Formatter` but not `Parser`
4. Verify that a pointer satisfies both

## What's Next

Continue to [08 - Embedding for Composition](../08-embedding-for-composition/08-embedding-for-composition.md) to explore how embedding achieves composition over inheritance.

## Summary

- Method set of `T`: only value receiver methods
- Method set of `*T`: both value and pointer receiver methods
- Interface satisfaction uses method sets -- a value cannot satisfy an interface requiring pointer receiver methods
- Direct method calls auto-take addresses on addressable values (variables, struct fields)
- Map values are NOT addressable -- use `map[K]*V` for pointer receiver methods
- This rule exists because interface values store copies, not references to the original

## Reference

- [Go Spec: Method sets](https://go.dev/ref/spec#Method_sets)
- [Go FAQ: Methods on values or pointers](https://go.dev/doc/faq#methods_on_values_or_pointers)
- [Go Spec: Address operators](https://go.dev/ref/spec#Address_operators)

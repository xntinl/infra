# 2. Struct Tags and JSON Encoding

<!--
difficulty: basic
concepts: [struct-tags, json-marshal, json-unmarshal, field-naming, omitempty]
tools: [go]
estimated_time: 20m
bloom_level: remember
prerequisites: [struct-declaration-and-initialization]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercise 01 (Struct Declaration and Initialization)
- Basic understanding of JSON format

## Learning Objectives

After completing this exercise, you will be able to:

- **Recall** the syntax for struct tags in Go
- **Identify** common JSON tag options like `omitempty` and `-`
- **Explain** how `encoding/json` uses struct tags to map between Go fields and JSON keys

## Why Struct Tags and JSON Encoding

Almost every Go program that communicates over HTTP, reads configuration files, or stores data needs to convert between Go structs and JSON. Struct tags are the mechanism that controls this conversion. They are raw string literals attached to struct fields that the `encoding/json` package reads at runtime using reflection.

Understanding struct tags prevents common bugs like silently missing fields in JSON output (unexported fields), unexpected key names, and bloated JSON with empty values. Mastering `json:"name,omitempty"` and related options is foundational for building APIs and working with external services.

## Step 1 -- Basic JSON Marshaling

Create a new project:

```bash
mkdir -p ~/go-exercises/struct-tags
cd ~/go-exercises/struct-tags
go mod init struct-tags
```

Create `main.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"log"
)

type User struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Age   int    `json:"age"`
}

func main() {
	u := User{Name: "Alice", Email: "alice@example.com", Age: 30}

	data, err := json.Marshal(u)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(data))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```json
{"name":"alice@example.com","email":"alice@example.com","age":30}
```

Wait -- that output uses the tag names (`name`, `email`, `age`) instead of the Go field names (`Name`, `Email`, `Age`). That is exactly what struct tags do.

Corrected expected output:

```
{"name":"Alice","email":"alice@example.com","age":30}
```

## Step 2 -- Unmarshaling JSON into Structs

Add unmarshaling to `main.go`:

```go
func main() {
	// Marshal
	u := User{Name: "Alice", Email: "alice@example.com", Age: 30}
	data, err := json.Marshal(u)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Marshaled:", string(data))

	// Unmarshal
	jsonStr := `{"name":"Bob","email":"bob@example.com","age":25}`
	var u2 User
	err = json.Unmarshal([]byte(jsonStr), &u2)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Unmarshaled: %+v\n", u2)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Marshaled: {"name":"Alice","email":"alice@example.com","age":30}
Unmarshaled: {Name:Bob Email:bob@example.com Age:25}
```

## Step 3 -- The `omitempty` Option

Fields with `omitempty` are excluded from JSON output when they hold their zero value:

```go
type Config struct {
	Host    string `json:"host"`
	Port    int    `json:"port,omitempty"`
	Debug   bool   `json:"debug,omitempty"`
	Timeout int    `json:"timeout,omitempty"`
}

func main() {
	c := Config{Host: "localhost", Port: 8080}
	data, _ := json.MarshalIndent(c, "", "  ")
	fmt.Println(string(data))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```json
{
  "host": "localhost",
  "port": 8080
}
```

`Debug` (false) and `Timeout` (0) are omitted because they are zero values and tagged `omitempty`.

## Step 4 -- Ignoring Fields with `-`

Use `json:"-"` to exclude a field from JSON entirely:

```go
type Session struct {
	UserID    int    `json:"user_id"`
	Token     string `json:"-"`
	ExpiresAt string `json:"expires_at"`
}

func main() {
	s := Session{UserID: 42, Token: "secret-token-abc", ExpiresAt: "2024-12-31"}
	data, _ := json.MarshalIndent(s, "", "  ")
	fmt.Println(string(data))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```json
{
  "user_id": 42,
  "expires_at": "2024-12-31"
}
```

The `Token` field is completely absent from the output.

## Step 5 -- Unexported Fields Are Invisible to JSON

```go
type internal struct {
	Public  string `json:"public"`
	private string `json:"private"` // unexported
}

func main() {
	v := internal{Public: "visible", private: "hidden"}
	data, _ := json.Marshal(v)
	fmt.Println(string(data))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
{"public":"visible"}
```

The `private` field is unexported (lowercase), so `encoding/json` cannot access it via reflection. The tag is ignored.

## Step 6 -- Nested Structs and JSON

```go
type Address struct {
	Street string `json:"street"`
	City   string `json:"city"`
}

type Person struct {
	Name    string  `json:"name"`
	Address Address `json:"address"`
}

func main() {
	p := Person{
		Name: "Carol",
		Address: Address{
			Street: "456 Oak Ave",
			City:   "Denver",
		},
	}
	data, _ := json.MarshalIndent(p, "", "  ")
	fmt.Println(string(data))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```json
{
  "name": "Carol",
  "address": {
    "street": "456 Oak Ave",
    "city": "Denver"
  }
}
```

## Common Mistakes

### Forgetting Backtick Syntax for Tags

**Wrong:**

```go
type User struct {
	Name string "json:\"name\""  // double quotes instead of backticks
}
```

**Fix:** Struct tags must use raw string literals (backticks): `` `json:"name"` ``

### Misspelling the Tag Key

**Wrong:**

```go
type User struct {
	Name string `JSON:"name"` // uppercase JSON
}
```

**What happens:** The `encoding/json` package looks for the key `json`, not `JSON`. The tag is silently ignored and the field uses its Go name.

**Fix:** Use lowercase `json` as the tag key.

### Space After the Colon

**Wrong:**

```go
type User struct {
	Name string `json: "name"` // space after colon
}
```

**What happens:** The tag is malformed. `go vet` catches this, but the program still compiles.

**Fix:** No space between the colon and the quoted value: `json:"name"`.

## Verify What You Learned

Run `go vet` on your final program to check for malformed struct tags:

```bash
go vet ./...
```

Expected: no output (no issues found).

## What's Next

Continue to [03 - Methods: Value vs Pointer Receivers](../03-methods-value-vs-pointer-receivers/03-methods-value-vs-pointer-receivers.md) to learn how to attach behavior to structs.

## Summary

- Struct tags are backtick-delimited strings attached to struct fields
- `json:"field_name"` controls the JSON key name for a field
- `omitempty` omits zero-value fields from JSON output
- `json:"-"` excludes a field from JSON entirely
- Unexported fields are invisible to `encoding/json` regardless of tags
- `json.Marshal` converts structs to JSON; `json.Unmarshal` converts JSON to structs
- Always run `go vet` to catch malformed struct tags

## Reference

- [encoding/json package](https://pkg.go.dev/encoding/json)
- [JSON and Go (blog post)](https://go.dev/blog/json)
- [Go Spec: Struct tags](https://go.dev/ref/spec#Struct_types)

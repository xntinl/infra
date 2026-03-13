# 2. Inspecting Struct Fields and Tags

<!--
difficulty: intermediate
concepts: [struct-fields, struct-tags, numfield, field, tag-get, reflect-structfield]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [reflect-typeof-valueof, structs-and-methods, struct-tags-and-json-encoding]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of `reflect.TypeOf` and `reflect.ValueOf`
- Familiarity with struct tags (e.g., `json:"name"`)

## Learning Objectives

After completing this exercise, you will be able to:

- **Iterate** over struct fields using `NumField()` and `Field(i)`
- **Read** struct tag values using `Tag.Get()`
- **Build** a utility that generates documentation from struct tags

## Why Inspecting Struct Fields and Tags

Struct tags are metadata strings attached to struct fields. The `encoding/json`, `encoding/xml`, and database packages all use struct tags to control serialization and mapping. Understanding how to read tags with reflection lets you build your own tag-driven tools: validators, mappers, documentation generators, and configuration binders.

## Step 1 -- Read Field Information

```bash
mkdir -p ~/go-exercises/struct-tags && cd ~/go-exercises/struct-tags
go mod init struct-tags
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"reflect"
)

type User struct {
	ID        int    `json:"id" db:"user_id" validate:"required"`
	FirstName string `json:"first_name" db:"first_name" validate:"required,min=2"`
	LastName  string `json:"last_name" db:"last_name" validate:"required,min=2"`
	Email     string `json:"email" db:"email" validate:"required,email"`
	Age       int    `json:"age,omitempty" db:"age" validate:"gte=0,lte=150"`
	Active    bool   `json:"active" db:"is_active"`
}

func inspectStruct(x interface{}) {
	t := reflect.TypeOf(x)

	// If x is a pointer, get the element type.
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		fmt.Println("not a struct")
		return
	}

	fmt.Printf("Struct: %s (%d fields)\n\n", t.Name(), t.NumField())

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fmt.Printf("Field %d: %s\n", i, field.Name)
		fmt.Printf("  Type:      %v\n", field.Type)
		fmt.Printf("  Kind:      %v\n", field.Type.Kind())
		fmt.Printf("  Exported:  %v\n", field.IsExported())
		fmt.Printf("  json tag:  %q\n", field.Tag.Get("json"))
		fmt.Printf("  db tag:    %q\n", field.Tag.Get("db"))
		fmt.Printf("  validate:  %q\n", field.Tag.Get("validate"))
		fmt.Println()
	}
}

func main() {
	inspectStruct(User{})
}
```

```bash
go run main.go
```

### Intermediate Verification

Each field is printed with its name, type, kind, export status, and tag values. `Tag.Get("json")` returns the full tag value for the `json` key (e.g., `"age,omitempty"`).

## Step 2 -- Parse Tag Options

Tag values like `json:"age,omitempty"` contain both a name and options. Parse them:

```go
package main

import (
	"fmt"
	"reflect"
	"strings"
)

type TagInfo struct {
	Name    string
	Options []string
}

func parseTag(tag string) TagInfo {
	parts := strings.Split(tag, ",")
	if len(parts) == 0 {
		return TagInfo{}
	}
	return TagInfo{
		Name:    parts[0],
		Options: parts[1:],
	}
}

func hasOption(info TagInfo, option string) bool {
	for _, opt := range info.Options {
		if opt == option {
			return true
		}
	}
	return false
}

type Product struct {
	ID    int     `json:"id"`
	Name  string  `json:"name" validate:"required"`
	Price float64 `json:"price,omitempty" validate:"required,gt=0"`
	SKU   string  `json:"-"` // skip in JSON
}

func main() {
	t := reflect.TypeOf(Product{})
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		jsonTag := parseTag(field.Tag.Get("json"))

		fmt.Printf("%s -> JSON name: %q", field.Name, jsonTag.Name)
		if hasOption(jsonTag, "omitempty") {
			fmt.Print(" [omitempty]")
		}
		if jsonTag.Name == "-" {
			fmt.Print(" [skipped]")
		}
		fmt.Println()
	}
}
```

### Intermediate Verification

Output shows each field's JSON name, with `[omitempty]` or `[skipped]` annotations. `Price` has `[omitempty]`, `SKU` has `[skipped]`.

## Step 3 -- Build a Schema Generator

Create a utility that generates API documentation from struct tags:

```go
func generateSchema(x interface{}) string {
	t := reflect.TypeOf(x)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n\n", t.Name()))
	sb.WriteString("| Field | Type | JSON | Required | Validation |\n")
	sb.WriteString("|-------|------|------|----------|------------|\n")

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		jsonTag := parseTag(field.Tag.Get("json"))
		validate := field.Tag.Get("validate")

		if jsonTag.Name == "-" {
			continue
		}

		jsonName := jsonTag.Name
		if jsonName == "" {
			jsonName = field.Name
		}

		required := strings.Contains(validate, "required")
		reqStr := ""
		if required {
			reqStr = "yes"
		}

		sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s |\n",
			field.Name, field.Type, jsonName, reqStr, validate))
	}

	return sb.String()
}
```

### Intermediate Verification

Calling `generateSchema(Product{})` produces a markdown table listing each field with its type, JSON name, required status, and validation rules.

## Common Mistakes

| Mistake | Why It Fails |
|---------|-------------|
| Forgetting to handle pointer types | `reflect.TypeOf(&s).Kind()` is `ptr`, not `struct` |
| Using `Tag.Get` with wrong key | Returns empty string, no error |
| Assuming tag exists on every field | Always check for empty string |
| Not handling unexported fields | `Field(i).IsExported()` returns false for lowercase fields |

## Verify What You Learned

1. How do you get the struct tag for a specific key from a `reflect.StructField`?
2. What does `Tag.Get("json")` return if the field has no json tag?
3. How do you iterate over all fields of a struct using reflection?
4. What is the difference between `reflect.Type.Field(i)` and `reflect.Value.Field(i)`?

## What's Next

Now that you can read struct metadata, the next exercise covers calling methods dynamically using reflection.

## Summary

`reflect.Type.NumField()` and `Field(i)` enumerate struct fields. Each `reflect.StructField` provides the field name, type, export status, and tag string. `Tag.Get(key)` extracts the value for a specific tag key. Parse tag values by splitting on commas to separate the name from options. This is the foundation for building validators, serializers, and schema generators.

## Reference

- [reflect.StructField](https://pkg.go.dev/reflect#StructField)
- [reflect.StructTag](https://pkg.go.dev/reflect#StructTag)
- [JSON struct tags](https://pkg.go.dev/encoding/json#Marshal)

# 5. Building a Struct Validator

<!--
difficulty: advanced
concepts: [tag-based-validation, struct-tags, reflection, validation-engine, custom-rules]
tools: [go]
estimated_time: 40m
bloom_level: analyze
prerequisites: [inspecting-struct-fields-tags, setting-values-with-reflect, custom-error-types]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of struct tag inspection with reflection
- Familiarity with custom error types

## Learning Objectives

After completing this exercise, you will be able to:

- **Build** a tag-driven validation engine using reflection
- **Implement** common validation rules: required, min/max length, range, pattern
- **Collect** multiple validation errors into a structured result
- **Design** an extensible rule system that supports custom validators

## Why Build a Struct Validator

Libraries like `go-playground/validator` use reflection and struct tags to validate data. Building one from scratch teaches you how reflection, tag parsing, and type switches work together. Even if you use a library in production, understanding the internals helps you debug validation issues and write custom rules.

## The Problem

Build a `validate` tag parser and validation engine that supports: `required`, `min`, `max`, `oneof`, and `email` rules. Validate structs and return all errors, not just the first one.

## Requirements

1. Parse `validate` struct tags with comma-separated rules
2. Implement at least 5 validation rules
3. Return a `ValidationErrors` type containing all field errors
4. Handle nested structs
5. Skip unexported and untagged fields

## Step 1 -- Define the API

```bash
mkdir -p ~/go-exercises/struct-validator && cd ~/go-exercises/struct-validator
go mod init struct-validator
```

Create `validator.go`:

```go
package main

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

type FieldError struct {
	Field   string
	Rule    string
	Message string
}

func (e FieldError) Error() string {
	return fmt.Sprintf("field %q failed %q: %s", e.Field, e.Rule, e.Message)
}

type ValidationErrors []FieldError

func (ve ValidationErrors) Error() string {
	var sb strings.Builder
	for i, e := range ve {
		if i > 0 {
			sb.WriteString("; ")
		}
		sb.WriteString(e.Error())
	}
	return sb.String()
}

func (ve ValidationErrors) HasErrors() bool {
	return len(ve) > 0
}

var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

func Validate(s interface{}) error {
	v := reflect.ValueOf(s)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("expected struct, got %v", v.Kind())
	}

	var errs ValidationErrors
	validateStruct(v, "", &errs)

	if errs.HasErrors() {
		return errs
	}
	return nil
}

func validateStruct(v reflect.Value, prefix string, errs *ValidationErrors) {
	t := v.Type()

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}

		tag := field.Tag.Get("validate")
		if tag == "" || tag == "-" {
			continue
		}

		fieldName := field.Name
		if prefix != "" {
			fieldName = prefix + "." + fieldName
		}

		fieldVal := v.Field(i)

		// Handle nested structs
		if fieldVal.Kind() == reflect.Struct && tag == "dive" {
			validateStruct(fieldVal, fieldName, errs)
			continue
		}

		rules := strings.Split(tag, ",")
		for _, rule := range rules {
			if err := applyRule(fieldName, fieldVal, rule); err != nil {
				*errs = append(*errs, *err)
			}
		}
	}
}

func applyRule(fieldName string, v reflect.Value, rule string) *FieldError {
	parts := strings.SplitN(rule, "=", 2)
	ruleName := parts[0]
	ruleParam := ""
	if len(parts) == 2 {
		ruleParam = parts[1]
	}

	switch ruleName {
	case "required":
		return validateRequired(fieldName, v)
	case "min":
		return validateMin(fieldName, v, ruleParam)
	case "max":
		return validateMax(fieldName, v, ruleParam)
	case "oneof":
		return validateOneOf(fieldName, v, ruleParam)
	case "email":
		return validateEmail(fieldName, v)
	default:
		return &FieldError{Field: fieldName, Rule: ruleName, Message: "unknown rule"}
	}
}

func validateRequired(name string, v reflect.Value) *FieldError {
	if v.IsZero() {
		return &FieldError{Field: name, Rule: "required", Message: "is required"}
	}
	return nil
}

func validateMin(name string, v reflect.Value, param string) *FieldError {
	n, _ := strconv.Atoi(param)
	switch v.Kind() {
	case reflect.String:
		if v.Len() < n {
			return &FieldError{Field: name, Rule: "min",
				Message: fmt.Sprintf("length must be >= %d", n)}
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if v.Int() < int64(n) {
			return &FieldError{Field: name, Rule: "min",
				Message: fmt.Sprintf("must be >= %d", n)}
		}
	case reflect.Slice, reflect.Array, reflect.Map:
		if v.Len() < n {
			return &FieldError{Field: name, Rule: "min",
				Message: fmt.Sprintf("length must be >= %d", n)}
		}
	}
	return nil
}

func validateMax(name string, v reflect.Value, param string) *FieldError {
	n, _ := strconv.Atoi(param)
	switch v.Kind() {
	case reflect.String:
		if v.Len() > n {
			return &FieldError{Field: name, Rule: "max",
				Message: fmt.Sprintf("length must be <= %d", n)}
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if v.Int() > int64(n) {
			return &FieldError{Field: name, Rule: "max",
				Message: fmt.Sprintf("must be <= %d", n)}
		}
	}
	return nil
}

func validateOneOf(name string, v reflect.Value, param string) *FieldError {
	options := strings.Fields(param)
	val := fmt.Sprintf("%v", v.Interface())
	for _, opt := range options {
		if val == opt {
			return nil
		}
	}
	return &FieldError{Field: name, Rule: "oneof",
		Message: fmt.Sprintf("must be one of [%s]", strings.Join(options, ", "))}
}

func validateEmail(name string, v reflect.Value) *FieldError {
	if v.Kind() != reflect.String {
		return &FieldError{Field: name, Rule: "email", Message: "must be a string"}
	}
	if !emailRegex.MatchString(v.String()) {
		return &FieldError{Field: name, Rule: "email", Message: "invalid email format"}
	}
	return nil
}
```

## Step 2 -- Use the Validator

Create `main.go`:

```go
package main

import "fmt"

type Address struct {
	Street string `validate:"required,min=5"`
	City   string `validate:"required"`
	Zip    string `validate:"required,min=5,max=10"`
}

type User struct {
	Name    string  `validate:"required,min=2,max=50"`
	Email   string  `validate:"required,email"`
	Age     int     `validate:"required,min=18,max=120"`
	Role    string  `validate:"required,oneof=admin user guest"`
	Address Address `validate:"dive"`
}

func main() {
	// Valid user
	valid := User{
		Name:  "Alice",
		Email: "alice@example.com",
		Age:   30,
		Role:  "admin",
		Address: Address{
			Street: "123 Main St",
			City:   "Springfield",
			Zip:    "62701",
		},
	}

	if err := Validate(valid); err != nil {
		fmt.Println("Validation failed:", err)
	} else {
		fmt.Println("Valid user: OK")
	}

	// Invalid user
	invalid := User{
		Name:  "A",           // too short
		Email: "not-an-email",
		Age:   10,            // under 18
		Role:  "superadmin",  // not in oneof
		Address: Address{
			Street: "123",    // too short
			City:   "",       // required
			Zip:    "1",      // too short
		},
	}

	if err := Validate(invalid); err != nil {
		if ve, ok := err.(ValidationErrors); ok {
			fmt.Printf("\nValidation errors (%d):\n", len(ve))
			for _, e := range ve {
				fmt.Printf("  - %s\n", e)
			}
		}
	}
}
```

```bash
go run validator.go main.go
```

## Hints

- `v.IsZero()` handles the `required` rule for all types
- Use `strings.SplitN(rule, "=", 2)` to separate rule name from parameter
- For `oneof`, `strings.Fields` splits the options by whitespace
- Nested structs use the `dive` tag to trigger recursive validation
- Consider adding a `validate:"omitempty"` rule that skips other rules when the value is zero

## Verification

- Valid struct passes with no errors
- Invalid struct returns multiple `FieldError` entries
- Nested struct fields are validated with dotted field names (e.g., `Address.City`)
- Unknown rules produce a clear error message
- The validator handles strings, ints, and nested structs

## What's Next

With a working validator, the next exercise explores `reflect.DeepEqual` and how to implement custom comparison logic.

## Summary

A tag-based struct validator uses `reflect.Type` to read `validate` tags and `reflect.Value` to inspect field values. Rules are parsed from comma-separated tag values and applied per-field. Multiple errors are collected into a `ValidationErrors` slice. Nested structs are handled recursively with a `dive` tag. This pattern is the foundation of libraries like `go-playground/validator`.

## Reference

- [reflect package](https://pkg.go.dev/reflect)
- [go-playground/validator](https://github.com/go-playground/validator)
- [Go struct tags](https://go.dev/wiki/Well-known-struct-tags)

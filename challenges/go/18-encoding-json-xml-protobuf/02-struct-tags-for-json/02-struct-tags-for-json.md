# Exercise 02: Struct Tags for JSON

**Difficulty:** Basic | **Estimated Time:** 20 minutes | **Section:** 18 - Encoding

## Overview

Go struct tags let you control how fields map to JSON keys. Without tags, JSON keys match the Go field names exactly (uppercase). Tags give you lowercase keys, the ability to omit empty values, skip fields entirely, and handle embedded structs -- all the control a real API needs.

## Prerequisites

- Exercise 01 (JSON marshal/unmarshal)
- Struct syntax

## Concepts

### Basic Tag Syntax

Struct tags are raw string literals placed after the field type:

```go
type User struct {
    Name  string `json:"name"`
    Email string `json:"email"`
    Age   int    `json:"age"`
}
```

Now `json.Marshal` produces `{"name":"Alice","email":"...","age":30}` instead of uppercase keys.

### omitempty

The `omitempty` option excludes a field from JSON when it holds its zero value:

```go
type User struct {
    Name  string `json:"name"`
    Email string `json:"email,omitempty"`
    Age   int    `json:"age,omitempty"`
}

u := User{Name: "Alice"}
// {"name":"Alice"} -- email and age are omitted
```

Zero values by type: `""` for strings, `0` for numbers, `false` for bools, `nil` for pointers/slices/maps.

### Skipping Fields

Use `json:"-"` to exclude a field from all JSON operations:

```go
type User struct {
    Name     string `json:"name"`
    Password string `json:"-"`
}
```

If you literally need a JSON key called `-`, use `json:"-,"` (note the comma).

### Pointers and omitempty

Pointers let you distinguish "absent" from "zero":

```go
type Config struct {
    Retries *int `json:"retries,omitempty"`
}

zero := 0
c1 := Config{Retries: &zero}  // {"retries":0} -- present, value is 0
c2 := Config{Retries: nil}    // {}            -- omitted
```

### Embedded Structs

Embedded structs are flattened by default:

```go
type Address struct {
    City    string `json:"city"`
    Country string `json:"country"`
}

type Person struct {
    Name string `json:"name"`
    Address        // embedded -- fields promoted
}

// {"name":"Alice","city":"London","country":"UK"}
```

To nest instead of flatten, give the field a name:

```go
type Person struct {
    Name    string  `json:"name"`
    Address Address `json:"address"`
}
// {"name":"Alice","address":{"city":"London","country":"UK"}}
```

### string Option

Force a numeric field to be encoded as a JSON string:

```go
type Item struct {
    ID    int64   `json:"id,string"`
    Price float64 `json:"price,string"`
}
// {"id":"42","price":"9.99"}
```

Useful when JavaScript cannot safely handle 64-bit integers.

## Task

Write a program that:

1. Defines an `APIResponse` struct with these fields and tags:
   - `Status` -- JSON key `"status"`
   - `Code` -- JSON key `"code"`
   - `Message` -- JSON key `"message"`, omit when empty
   - `Data` -- JSON key `"data"`, omit when empty (use `interface{}`)
   - `InternalTrace` -- never included in JSON
   - `RequestID` -- JSON key `"request_id"`, encoded as string

2. Creates two responses:
   - A success response with all fields populated (including `InternalTrace`)
   - An error response with only `Status`, `Code`, and `RequestID`

3. Marshals both with indentation and prints them

4. Unmarshals this JSON into the struct and prints it:
   ```json
   {"status":"ok","code":200,"data":{"items":["a","b"]},"request_id":"99"}
   ```

## Step-by-Step

### Step 1: Define the struct

```go
package main

import (
    "encoding/json"
    "fmt"
    "log"
)

type APIResponse struct {
    Status        string      `json:"status"`
    Code          int         `json:"code"`
    Message       string      `json:"message,omitempty"`
    Data          interface{} `json:"data,omitempty"`
    InternalTrace string      `json:"-"`
    RequestID     int64       `json:"request_id,string"`
}
```

### Step 2: Create and marshal responses

```go
func main() {
    success := APIResponse{
        Status:        "ok",
        Code:          200,
        Message:       "User created",
        Data:          map[string]string{"id": "abc123"},
        InternalTrace: "trace-xyz-internal",
        RequestID:     42,
    }

    errResp := APIResponse{
        Status:    "error",
        Code:      404,
        RequestID: 43,
    }

    for _, resp := range []APIResponse{success, errResp} {
        data, err := json.MarshalIndent(resp, "", "  ")
        if err != nil {
            log.Fatal(err)
        }
        fmt.Println(string(data))
        fmt.Println()
    }
```

### Step 3: Unmarshal external JSON

```go
    input := `{"status":"ok","code":200,"data":{"items":["a","b"]},"request_id":"99"}`

    var parsed APIResponse
    if err := json.Unmarshal([]byte(input), &parsed); err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Parsed: %+v\n", parsed)
}
```

## Expected Output

```
{
  "status": "ok",
  "code": 200,
  "message": "User created",
  "data": {
    "id": "abc123"
  },
  "request_id": "42"
}

{
  "status": "error",
  "code": 404,
  "request_id": "43"
}

Parsed: {Status:ok Code:200 Message: Data:map[items:[a b]] InternalTrace: RequestID:99}
```

Notice: `InternalTrace` does not appear in JSON output. `Message` and `Data` are omitted from the error response.

## Bonus Challenge

Create a struct with an embedded `time.Time` field. What JSON format does it default to? Try adding both `omitempty` and `string` to the same field tag and observe what happens.

## Key Takeaways

- `json:"key_name"` controls the JSON key
- `omitempty` suppresses zero-valued fields
- `json:"-"` excludes a field entirely
- Pointers + `omitempty` distinguish "absent" from "zero"
- Embedded structs flatten unless given an explicit field name and tag
- The `string` option wraps numeric values in JSON strings

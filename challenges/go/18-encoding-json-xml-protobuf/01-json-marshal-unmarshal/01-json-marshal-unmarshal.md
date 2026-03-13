# Exercise 01: JSON Marshal and Unmarshal

**Difficulty:** Basic | **Estimated Time:** 15 minutes | **Section:** 18 - Encoding

## Overview

JSON (JavaScript Object Notation) is the lingua franca of web APIs. Go's `encoding/json` package provides two core operations: **marshaling** (Go value to JSON bytes) and **unmarshaling** (JSON bytes to Go value). This exercise walks you through both directions.

## Prerequisites

- Structs and basic types
- Slices and maps
- Byte slices and strings

## Concepts

### Marshaling: Go to JSON

`json.Marshal` converts a Go value into a JSON-encoded byte slice:

```go
import "encoding/json"

type User struct {
    Name  string
    Email string
    Age   int
}

u := User{Name: "Alice", Email: "alice@example.com", Age: 30}
data, err := json.Marshal(u)
if err != nil {
    log.Fatal(err)
}
fmt.Println(string(data))
// {"Name":"Alice","Email":"alice@example.com","Age":30}
```

Only **exported** fields (starting with uppercase) are included in the JSON output.

### Unmarshaling: JSON to Go

`json.Unmarshal` parses JSON bytes and stores the result in a Go value:

```go
jsonStr := `{"Name":"Bob","Email":"bob@example.com","Age":25}`

var u User
err := json.Unmarshal([]byte(jsonStr), &u)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("%+v\n", u)
// {Name:Bob Email:bob@example.com Age:25}
```

You must pass a **pointer** to `Unmarshal` so it can modify the target value.

### Pretty Printing

`json.MarshalIndent` produces human-readable output:

```go
data, err := json.MarshalIndent(u, "", "  ")
```

The second argument is a prefix for each line; the third is the indentation string.

### Maps and Slices

You can marshal and unmarshal maps and slices directly:

```go
m := map[string]interface{}{
    "name": "Charlie",
    "scores": []int{95, 87, 92},
}
data, _ := json.Marshal(m)

var result map[string]interface{}
json.Unmarshal(data, &result)
```

## Task

Write a program that:

1. Defines a `Book` struct with fields: `Title` (string), `Author` (string), `Pages` (int), `Published` (bool)
2. Creates a slice of three `Book` values
3. Marshals the slice to JSON with indentation (two spaces)
4. Prints the JSON string
5. Unmarshals the JSON back into a new slice of `Book`
6. Prints each book on its own line using `fmt.Printf("%+v\n", book)`

## Step-by-Step

### Step 1: Define the struct

```go
package main

import (
    "encoding/json"
    "fmt"
    "log"
)

type Book struct {
    Title     string
    Author    string
    Pages     int
    Published bool
}
```

### Step 2: Create the data

```go
func main() {
    books := []Book{
        {Title: "The Go Programming Language", Author: "Donovan & Kernighan", Pages: 380, Published: true},
        {Title: "Concurrency in Go", Author: "Katherine Cox-Buday", Pages: 238, Published: true},
        {Title: "Untitled Draft", Author: "Unknown", Pages: 0, Published: false},
    }
```

### Step 3: Marshal to JSON

```go
    data, err := json.MarshalIndent(books, "", "  ")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(string(data))
```

### Step 4: Unmarshal back

```go
    var decoded []Book
    if err := json.Unmarshal(data, &decoded); err != nil {
        log.Fatal(err)
    }

    fmt.Println("\nDecoded books:")
    for _, b := range decoded {
        fmt.Printf("  %+v\n", b)
    }
}
```

## Expected Output

```
[
  {
    "Title": "The Go Programming Language",
    "Author": "Donovan & Kernighan",
    "Pages": 380,
    "Published": true
  },
  {
    "Title": "Concurrency in Go",
    "Author": "Katherine Cox-Buday",
    "Pages": 238,
    "Published": true
  },
  {
    "Title": "Untitled Draft",
    "Author": "Unknown",
    "Pages": 0,
    "Published": false
  }
]

Decoded books:
  {Title:The Go Programming Language Author:Donovan & Kernighan Pages:380 Published:true}
  {Title:Concurrency in Go Author:Katherine Cox-Buday Pages:238 Published:true}
  {Title:Untitled Draft Author:Unknown Pages:0 Published:false}
```

## Bonus Challenge

Try marshaling a struct that contains unexported fields. What happens? Try unmarshaling JSON that has extra fields not in your struct. What happens?

## Key Takeaways

- `json.Marshal` encodes Go values to JSON bytes; `json.Unmarshal` decodes
- Only exported struct fields participate in encoding
- Always pass a pointer to `Unmarshal`
- `MarshalIndent` is useful for debugging but adds size overhead in production
- Unknown JSON fields are silently ignored during unmarshaling by default

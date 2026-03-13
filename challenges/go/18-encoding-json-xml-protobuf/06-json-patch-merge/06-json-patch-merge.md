# Exercise 06: JSON Patch and Merge

**Difficulty:** Intermediate | **Estimated Time:** 30 minutes | **Section:** 18 - Encoding

## Overview

APIs commonly need to update parts of a JSON document without replacing the whole thing. Two standards exist for this: **JSON Merge Patch** (RFC 7396) -- simple key-level merge, and **JSON Patch** (RFC 6902) -- an array of targeted operations. In this exercise you implement both from scratch using Go's `encoding/json` and `map[string]interface{}`.

## Prerequisites

- Exercises 01-02 (JSON basics)
- Maps and type assertions
- Recursion basics

## Background

### JSON Merge Patch (RFC 7396)

A merge patch is a JSON document that describes changes. Rules:
- Present keys overwrite the original
- A key set to `null` removes it from the original
- Nested objects merge recursively

```
Original:  {"name":"Alice","age":30,"address":{"city":"London"}}
Patch:     {"age":31,"address":{"city":"Paris"},"email":"a@b.com"}
Result:    {"name":"Alice","age":31,"address":{"city":"Paris"},"email":"a@b.com"}
```

### JSON Patch (RFC 6902)

A JSON Patch is an array of operation objects. Each has `op`, `path`, and optionally `value`:

```json
[
  {"op":"replace","path":"/age","value":31},
  {"op":"add","path":"/email","value":"a@b.com"},
  {"op":"remove","path":"/nickname"}
]
```

Operations: `add`, `remove`, `replace`, `move`, `copy`, `test`.

## Task

Implement both patching strategies without external libraries:

### Part 1: JSON Merge Patch

Write a function:

```go
func MergePatch(original, patch map[string]interface{}) map[string]interface{}
```

Rules:
- If a patch value is `nil`, delete that key from the original
- If a patch value is a `map[string]interface{}` and the original has a map at the same key, recurse
- Otherwise, set the key to the patch value
- Return the modified original

Test with:

```json
Original: {"name":"Server-1","config":{"cpu":4,"memory":8,"tags":["prod","us-east"]},"status":"running"}
Patch:    {"config":{"memory":16,"gpu":1},"status":null,"owner":"ops-team"}
```

Expected result: name unchanged, config.memory updated to 16, config.gpu added, config.cpu preserved, status removed, owner added.

### Part 2: JSON Patch (subset)

Write a function:

```go
func ApplyPatch(doc map[string]interface{}, ops []PatchOp) (map[string]interface{}, error)
```

Where `PatchOp` has `Op`, `Path`, and `Value` (interface{}). Implement `add`, `remove`, and `replace` for top-level keys only (path like `"/key"`). Return an error for unsupported operations or missing keys on `remove`/`replace`.

Test with:

```json
Document: {"name":"App","version":"1.0","debug":true}
Operations: [
  {"op":"replace","path":"/version","value":"2.0"},
  {"op":"add","path":"/author","value":"team"},
  {"op":"remove","path":"/debug"}
]
```

### Part 3: Diff

Write a function that takes two JSON objects and produces a merge patch that transforms one into the other:

```go
func CreateMergePatch(original, modified map[string]interface{}) map[string]interface{}
```

Keys in original but not modified should produce `nil` in the patch. Keys in modified but not original or with different values should be included.

## Hints

- Unmarshal JSON into `map[string]interface{}` for flexible manipulation.
- In Go, when JSON is unmarshaled into `interface{}`, `null` becomes `nil`.
- Use type assertion `v, ok := val.(map[string]interface{})` to check for nested objects.
- For the patch path, just strip the leading `/` and use it as a map key (no nested path support needed).
- Deep-copy the original before mutating if you want to preserve the input.
- Use `reflect.DeepEqual` to compare values in `CreateMergePatch`.

## Verification

Print the result of each operation as indented JSON. The merge patch result should show:

```json
{
  "config": {
    "cpu": 4,
    "gpu": 1,
    "memory": 16,
    "tags": ["prod", "us-east"]
  },
  "name": "Server-1",
  "owner": "ops-team"
}
```

Note: `status` is gone, `config.memory` is 16, `config.gpu` is added.

## Key Takeaways

- JSON Merge Patch is simple but cannot set a value to `null` (it means "delete")
- JSON Patch is more expressive with explicit operations but more complex to implement
- `map[string]interface{}` is the escape hatch for dynamic JSON in Go
- Recursive merge is the core algorithm for merge patch
- Generating diffs (Part 3) is the inverse operation and useful for audit logs

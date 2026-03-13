# Exercise 08: Protocol Buffers

**Difficulty:** Advanced | **Estimated Time:** 35 minutes | **Section:** 18 - Encoding

## Overview

Protocol Buffers (protobuf) is Google's language-neutral, binary serialization format. It is smaller, faster, and more strongly typed than JSON. You define your data structures in `.proto` files, generate Go code with `protoc`, and use the generated types for serialization. This exercise takes you through the full workflow.

## Prerequisites

- JSON marshal/unmarshal experience (Exercises 01-03)
- Installing CLI tools (`go install`, `brew`, etc.)

## Problem

Build a contact book system using Protocol Buffers:

1. Define a `.proto` file with these message types:

   - `PhoneNumber` with `number` (string) and `type` (enum: MOBILE, HOME, WORK)
   - `Contact` with `id` (int32), `name` (string), `email` (string), `phones` (repeated PhoneNumber), `created_at` (google.protobuf.Timestamp)
   - `ContactBook` with `owner` (string) and `contacts` (repeated Contact)

2. Generate Go code from the proto definition.

3. Write a Go program that:
   - Creates a `ContactBook` with at least 3 contacts
   - Serializes it to protobuf binary format (`proto.Marshal`)
   - Deserializes it back (`proto.Unmarshal`)
   - Also serializes to JSON using `protojson` for comparison
   - Prints the size of the protobuf binary vs the JSON string

## Setup Requirements

Install the protobuf compiler and Go plugin:

```bash
# Install protoc (macOS)
brew install protobuf

# Install Go plugins
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest

# Ensure $GOPATH/bin is in PATH
```

## Proto File Structure

```
project/
  proto/
    contacts.proto
  gen/
    contacts/
      contacts.pb.go    (generated)
  main.go
  go.mod
```

Proto file skeleton:

```protobuf
syntax = "proto3";
package contacts;
option go_package = "gen/contacts";

import "google/protobuf/timestamp.proto";

enum PhoneType {
  MOBILE = 0;
  HOME = 1;
  WORK = 2;
}

// Define your messages here
```

Generate with:

```bash
protoc --go_out=. --go_opt=paths=source_relative proto/contacts.proto
```

## Hints

- The `proto` package: `google.golang.org/protobuf/proto` for Marshal/Unmarshal.
- The `protojson` package: `google.golang.org/protobuf/encoding/protojson` for JSON.
- Use `timestamppb.Now()` to create a `google.protobuf.Timestamp`.
- Proto3 fields are all optional by default (zero values are not serialized).
- The generated Go struct uses pointer-like access but with value types and getter methods.
- Compare sizes: `len(protoBytes)` vs `len(jsonBytes)` -- protobuf is typically 30-70% smaller.

## Verification Criteria

- The `.proto` file compiles without errors
- Go code uses the generated types (not hand-written structs)
- Round-trip: marshal then unmarshal produces identical data
- Output shows both protobuf binary size and JSON size
- Example: "Protobuf: 182 bytes, JSON: 547 bytes"

## Stretch Goals

- Add a `SearchContacts` function that filters by name substring on the deserialized data
- Write protobuf bytes to a file and read them back in a separate run
- Use `proto.Equal` to verify round-trip equality

## Key Takeaways

- Protobuf provides schema-driven, backward-compatible binary encoding
- The workflow is: `.proto` file, `protoc` compile, use generated Go types
- Protobuf is significantly smaller than JSON for the same data
- `protojson` bridges protobuf types and JSON when you need both
- Proto3 uses zero-value defaults; there is no "required" keyword

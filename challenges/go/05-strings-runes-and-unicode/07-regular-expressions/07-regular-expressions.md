# 7. Regular Expressions

<!--
difficulty: intermediate
concepts: [regexp-package, compile, match, find, replace, submatch, named-groups]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [string-basics, strings-package]
-->

## Prerequisites

- Go 1.22+ installed
- Familiarity with basic regex syntax (character classes, quantifiers, anchors)
- Understanding of Go strings and byte slices

## Learning Objectives

After completing this exercise, you will be able to:

- **Use** `regexp.Compile` and `regexp.MustCompile` to create compiled patterns
- **Apply** `Find`, `FindAll`, `FindSubmatch`, and `ReplaceAll` methods
- **Distinguish** between `string` and `[]byte` variants of regexp methods

## Why Regular Expressions

Regular expressions extract structured data from unstructured text: parsing log files, validating input formats, extracting URLs, and transforming text patterns. Go's `regexp` package uses the RE2 engine, which guarantees linear-time matching -- no catastrophic backtracking. This makes it safe for processing untrusted input, but it also means some Perl-compatible features (lookahead, backreferences) are not available.

## Step 1 -- Compiling and Matching

```bash
mkdir -p ~/go-exercises/regexp-basics
cd ~/go-exercises/regexp-basics
go mod init regexp-basics
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"regexp"
)

func main() {
	// MustCompile panics if the pattern is invalid (use for constants)
	emailPattern := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

	emails := []string{
		"user@example.com",
		"invalid-email",
		"alice+tag@company.co.uk",
		"@missing-local.com",
	}

	for _, email := range emails {
		match := emailPattern.MatchString(email)
		fmt.Printf("%-30s valid=%t\n", email, match)
	}
}
```

`MustCompile` is appropriate for package-level pattern constants. Use `regexp.Compile` when the pattern comes from user input and you need to handle errors.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
user@example.com               valid=true
invalid-email                  valid=false
alice+tag@company.co.uk        valid=true
@missing-local.com             valid=false
```

## Step 2 -- Finding Matches

```go
package main

import (
	"fmt"
	"regexp"
)

func main() {
	text := "Server started on port 8080. Backup on port 9090. Debug on port 6060."

	portPattern := regexp.MustCompile(`port (\d+)`)

	// FindString returns the first match
	first := portPattern.FindString(text)
	fmt.Println("First match:", first)

	// FindAllString returns all matches
	all := portPattern.FindAllString(text, -1) // -1 means all
	fmt.Println("All matches:", all)

	// FindAllString with limit
	limited := portPattern.FindAllString(text, 2)
	fmt.Println("First two:", limited)

	// FindStringIndex returns the byte offsets
	loc := portPattern.FindStringIndex(text)
	fmt.Printf("First match at [%d:%d] = %q\n", loc[0], loc[1], text[loc[0]:loc[1]])
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
First match: port 8080
All matches: [port 8080 port 9090 port 6060]
First two: [port 8080 port 9090]
First match at [22:31] = "port 8080"
```

## Step 3 -- Submatches (Capture Groups)

```go
package main

import (
	"fmt"
	"regexp"
)

func main() {
	logPattern := regexp.MustCompile(`(\d{4}-\d{2}-\d{2}) (\w+): (.+)`)

	log := "2024-03-15 ERROR: connection timeout to database"

	// FindStringSubmatch returns [full_match, group1, group2, ...]
	matches := logPattern.FindStringSubmatch(log)
	if matches != nil {
		fmt.Println("Full match:", matches[0])
		fmt.Println("Date:      ", matches[1])
		fmt.Println("Level:     ", matches[2])
		fmt.Println("Message:   ", matches[3])
	}

	fmt.Println()

	// Named groups
	namedPattern := regexp.MustCompile(
		`(?P<date>\d{4}-\d{2}-\d{2}) (?P<level>\w+): (?P<msg>.+)`)

	matches = namedPattern.FindStringSubmatch(log)
	for i, name := range namedPattern.SubexpNames() {
		if i == 0 || name == "" {
			continue
		}
		fmt.Printf("%-8s = %s\n", name, matches[i])
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Full match: 2024-03-15 ERROR: connection timeout to database
Date:       2024-03-15
Level:      ERROR
Message:    connection timeout to database

date     = 2024-03-15
level    = ERROR
msg      = connection timeout to database
```

## Step 4 -- Replacement

```go
package main

import (
	"fmt"
	"regexp"
	"strings"
)

func main() {
	// Simple replacement
	re := regexp.MustCompile(`\d+`)
	result := re.ReplaceAllString("item1 item2 item3", "X")
	fmt.Println(result)

	// Replacement with capture group reference
	reDate := regexp.MustCompile(`(\d{2})/(\d{2})/(\d{4})`)
	isoDate := reDate.ReplaceAllString("Date: 03/15/2024", "$3-$1-$2")
	fmt.Println(isoDate)

	// ReplaceAllStringFunc for dynamic replacements
	reWord := regexp.MustCompile(`\b\w+\b`)
	upper := reWord.ReplaceAllStringFunc("hello world", strings.ToUpper)
	fmt.Println(upper)

	// Literal replacement (no group expansion)
	reDollar := regexp.MustCompile(`price`)
	literal := reDollar.ReplaceAllLiteralString("The price is price", "$100")
	fmt.Println(literal)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
itemX itemX itemX
Date: 2024-03-15
HELLO WORLD
The $100 is $100
```

## Step 5 -- Splitting and Practical Patterns

```go
package main

import (
	"fmt"
	"regexp"
)

func main() {
	// Split on one or more whitespace characters
	re := regexp.MustCompile(`\s+`)
	fields := re.Split("  hello   world  go  ", -1)
	fmt.Printf("Split: %q\n", fields)

	// Extract all URLs from text
	urlPattern := regexp.MustCompile(`https?://[^\s]+`)
	text := "Visit https://go.dev and http://example.com/path?q=1 for more."
	urls := urlPattern.FindAllString(text, -1)
	for _, u := range urls {
		fmt.Println("URL:", u)
	}

	fmt.Println()

	// Validate multiple inputs
	ipPattern := regexp.MustCompile(`^(\d{1,3}\.){3}\d{1,3}$`)
	ips := []string{"192.168.1.1", "10.0.0.1", "999.999.999.999", "abc"}
	for _, ip := range ips {
		fmt.Printf("%-20s matches=%t\n", ip, ipPattern.MatchString(ip))
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Split: ["" "hello" "world" "go" ""]
URL: https://go.dev
URL: http://example.com/path?q=1

192.168.1.1          matches=true
10.0.0.1             matches=true
999.999.999.999      matches=true
abc                  matches=false
```

## Common Mistakes

### Using Compile Instead of MustCompile for Constants

**Wrong:**

```go
var re, _ = regexp.Compile(`\d+`) // ignoring error for a constant pattern
```

**What happens:** If the pattern has a typo, `re` is `nil` and later calls panic.

**Fix:** Use `MustCompile` for patterns known at compile time. It panics immediately with a clear message.

### Forgetting RE2 Limitations

**Wrong:**

```go
regexp.MustCompile(`(?<=@)\w+`) // lookbehind -- not supported
```

**What happens:** `MustCompile` panics because RE2 does not support lookahead or lookbehind.

**Fix:** Restructure the pattern using capture groups: `@(\w+)` and access `matches[1]`.

### Not Anchoring Validation Patterns

**Wrong:**

```go
re := regexp.MustCompile(`\d{3}-\d{4}`)
re.MatchString("abc123-4567xyz") // true -- partial match!
```

**Fix:** Add anchors: `^\d{3}-\d{4}$`.

## Verify What You Learned

1. Write a regex that extracts all hashtags (`#word`) from a tweet
2. Use `FindAllStringSubmatch` to parse key-value pairs from `"name=alice age=30 city=portland"`
3. Write a function that censors phone numbers in text by replacing digits with `*`

## What's Next

Continue to [08 - Unicode Normalization and Collation](../08-unicode-normalization-and-collation/08-unicode-normalization-and-collation.md) to learn how different byte sequences can represent the same character.

## Summary

- `regexp.MustCompile` for constant patterns; `regexp.Compile` for user-supplied patterns
- `MatchString` checks if a pattern matches; `FindString` returns the match
- `FindAllString` returns all matches; pass `-1` for unlimited results
- `FindStringSubmatch` returns capture groups as a string slice
- Named groups use `(?P<name>...)` syntax; access names via `SubexpNames()`
- `ReplaceAllString` uses `$1` for group references; `ReplaceAllLiteralString` does not
- Go uses RE2: guaranteed linear time, but no lookahead/lookbehind

## Reference

- [regexp package](https://pkg.go.dev/regexp)
- [regexp/syntax](https://pkg.go.dev/regexp/syntax) -- supported RE2 syntax
- [RE2 syntax reference](https://github.com/google/re2/wiki/Syntax)

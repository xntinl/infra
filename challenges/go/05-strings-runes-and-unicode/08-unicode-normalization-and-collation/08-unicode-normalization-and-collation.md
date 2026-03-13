# 8. Unicode Normalization and Collation

<!--
difficulty: advanced
concepts: [unicode-normalization, nfc, nfd, combining-characters, collation, golang-x-text]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [runes-and-unicode-code-points, string-iteration-bytes-vs-runes, strings-package]
-->

## Prerequisites

- Go 1.22+ installed
- Solid understanding of runes and UTF-8 encoding
- Familiarity with `go get` for external packages

## The Problem

The string `"café"` can be encoded two ways in Unicode: as a single code point `é` (U+00E9), or as the letter `e` (U+0065) followed by a combining acute accent (U+0301). Both look identical on screen, but `==` comparison returns `false` because the byte sequences differ. This causes bugs in string comparison, map lookups, database queries, and search functionality.

Your task: understand Unicode normalization forms (NFC, NFD), detect and convert between them, and use collation for locale-aware sorting.

## Requirements

1. Demonstrate that two visually identical strings can have different byte representations
2. Use `golang.org/x/text/unicode/norm` to normalize strings to NFC and NFD forms
3. Show that normalized strings compare equal with `==`
4. Use `golang.org/x/text/collate` to sort strings in locale-aware order
5. Build a function that safely compares strings regardless of normalization form

## Hints

<details>
<summary>Hint 1: Creating composed vs decomposed strings</summary>

```go
composed := "caf\u00e9"           // é as single code point (NFC)
decomposed := "cafe\u0301"        // e + combining accent (NFD)
fmt.Println(composed == decomposed) // false!
fmt.Println(composed)              // café
fmt.Println(decomposed)            // café (looks the same)
```
</details>

<details>
<summary>Hint 2: Using the norm package</summary>

```go
import "golang.org/x/text/unicode/norm"

s1 := "caf\u00e9"
s2 := "cafe\u0301"

// Normalize both to NFC
n1 := norm.NFC.String(s1)
n2 := norm.NFC.String(s2)
fmt.Println(n1 == n2) // true
```

Install with: `go get golang.org/x/text`
</details>

<details>
<summary>Hint 3: Collation for locale-aware sorting</summary>

```go
import (
    "golang.org/x/text/collate"
    "golang.org/x/text/language"
)

col := collate.New(language.German)
words := []string{"Öl", "Apfel", "Über", "Birne"}
col.SortStrings(words)
```

Different languages sort accented characters differently. German treats `ö` as `o`; Swedish sorts `ö` after `z`.
</details>

<details>
<summary>Hint 4: Checking normalization form</summary>

```go
// Check if a string is already in NFC form
if norm.NFC.IsNormalString(s) {
    fmt.Println("Already NFC")
} else {
    s = norm.NFC.String(s)
}
```
</details>

## Verification

Your program should demonstrate:

1. Two strings that look identical but differ in byte representation
2. Byte-level inspection showing the different encodings
3. Normalization making them compare equal
4. Locale-aware sorting producing different orders for different languages
5. A safe comparison function that normalizes before comparing

Check your understanding:
- When does normalization matter in real applications?
- Why is NFC the recommended normalization form for interchange?
- How does collation differ from simple byte comparison?

## What's Next

Continue to [09 - Strings Builder Performance](../09-strings-builder-performance/09-strings-builder-performance.md) to benchmark different string concatenation strategies.

## Summary

- Unicode allows multiple byte sequences for the same visual character
- NFC (composed): `é` = single code point U+00E9
- NFD (decomposed): `é` = `e` (U+0065) + combining accent (U+0301)
- Use `golang.org/x/text/unicode/norm` to normalize before comparison or storage
- Collation (`golang.org/x/text/collate`) sorts strings according to locale rules
- Always normalize user input at system boundaries (API ingress, form submission)

## Reference

- [golang.org/x/text/unicode/norm](https://pkg.go.dev/golang.org/x/text/unicode/norm)
- [golang.org/x/text/collate](https://pkg.go.dev/golang.org/x/text/collate)
- [Unicode Normalization FAQ](https://unicode.org/faq/normalization.html)
- [Unicode Technical Report #15: Normalization Forms](https://unicode.org/reports/tr15/)

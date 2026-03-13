# 10. Building a Text Processing Pipeline

<!--
difficulty: advanced
concepts: [text-processing, pipeline, io-reader, scanner, transform, unicode-normalization, strings-builder]
tools: [go]
estimated_time: 40m
bloom_level: create
prerequisites: [string-basics, byte-slices-vs-strings, runes-and-unicode-code-points, strings-package, regular-expressions, unicode-normalization-and-collation, strings-builder-performance]
-->

## Prerequisites

- Go 1.22+ installed
- Completed all previous exercises in this section (1-9)
- Familiarity with `io.Reader`, `bufio.Scanner`, and `strings.Builder`

## The Problem

Real-world text processing rarely involves a single operation. You receive messy input -- mixed encodings, inconsistent whitespace, HTML entities, irregular casing -- and must produce clean, normalized output. Each cleaning step is simple on its own, but composing them correctly and efficiently requires careful design.

Your task: build a text processing pipeline where each stage is a composable transformation function. The pipeline reads input from an `io.Reader`, applies a chain of transformations, and writes clean output using `strings.Builder`.

## Requirements

1. Define a `Transform` type: `type Transform func(string) string`
2. Build a `Pipeline` that chains multiple `Transform` functions
3. Implement at least six transforms:
   - **NormalizeUnicode**: NFC normalization using `golang.org/x/text/unicode/norm`
   - **CollapseWhitespace**: replace runs of whitespace with a single space, trim edges
   - **StripHTML**: remove HTML tags using regex
   - **DecodeEntities**: convert `&amp;`, `&lt;`, `&gt;`, `&#39;`, `&quot;` to their characters
   - **NormalizeCase**: convert to lowercase (Unicode-aware using `strings.ToLower`)
   - **RemoveControlChars**: strip characters in categories Cc and Cf except newline and tab
4. The pipeline must read line-by-line from an `io.Reader` using `bufio.Scanner`
5. Collect output efficiently with `strings.Builder`
6. Process a sample input demonstrating all transforms applied in sequence

## Hints

<details>
<summary>Hint 1: Transform type and chaining</summary>

```go
type Transform func(string) string

type Pipeline struct {
    transforms []Transform
}

func NewPipeline(transforms ...Transform) *Pipeline {
    return &Pipeline{transforms: transforms}
}

func (p *Pipeline) Apply(input string) string {
    result := input
    for _, t := range p.transforms {
        result = t(result)
    }
    return result
}
```
</details>

<details>
<summary>Hint 2: Processing from io.Reader</summary>

```go
func (p *Pipeline) Process(r io.Reader) string {
    scanner := bufio.NewScanner(r)
    var sb strings.Builder
    first := true
    for scanner.Scan() {
        if !first {
            sb.WriteString("\n")
        }
        sb.WriteString(p.Apply(scanner.Text()))
        first = false
    }
    return sb.String()
}
```
</details>

<details>
<summary>Hint 3: StripHTML and DecodeEntities</summary>

```go
var htmlTagRe = regexp.MustCompile(`<[^>]*>`)

func StripHTML(s string) string {
    return htmlTagRe.ReplaceAllString(s, "")
}

var entities = map[string]string{
    "&amp;": "&", "&lt;": "<", "&gt;": ">",
    "&quot;": `"`, "&#39;": "'",
}

func DecodeEntities(s string) string {
    for entity, char := range entities {
        s = strings.ReplaceAll(s, entity, char)
    }
    return s
}
```
</details>

<details>
<summary>Hint 4: RemoveControlChars</summary>

```go
func RemoveControlChars(s string) string {
    var sb strings.Builder
    sb.Grow(len(s))
    for _, r := range s {
        if r == '\n' || r == '\t' || !unicode.IsControl(r) {
            sb.WriteRune(r)
        }
    }
    return sb.String()
}
```
</details>

## Verification

Feed the pipeline input like:

```
<p>Caf&eacute; &amp; R&eacute;sum&eacute;</p>
<b>HELLO</b>   WORLD   &lt;test&gt;
```

Expected output after all transforms:

```
cafe & resume
hello world <test>
```

Check your understanding:
- Does the order of transforms matter? What breaks if you strip HTML after decoding entities?
- How would you add a transform that operates on `[]byte` instead of `string`?
- How could you make the pipeline concurrent with a stage per goroutine?

## What's Next

You have completed the Strings, Runes, and Unicode section. Continue to [Section 06 - Collections: Arrays, Slices, and Maps](../../06-collections-arrays-slices-and-maps/01-arrays-fixed-size-value-semantics/01-arrays-fixed-size-value-semantics.md).

## Summary

- A `Transform` function type `func(string) string` is composable and testable in isolation
- Pipelines chain transforms sequentially; order matters (strip HTML before decoding entities)
- `bufio.Scanner` reads line-by-line from any `io.Reader`, decoupling the pipeline from the input source
- `strings.Builder` collects output efficiently without O(n^2) copying
- Unicode normalization, whitespace collapsing, and HTML stripping are common real-world text cleaning stages
- This pattern generalizes to any data transformation pipeline in Go

## Reference

- [bufio.Scanner](https://pkg.go.dev/bufio#Scanner)
- [strings.Builder](https://pkg.go.dev/strings#Builder)
- [golang.org/x/text/unicode/norm](https://pkg.go.dev/golang.org/x/text/unicode/norm)
- [regexp package](https://pkg.go.dev/regexp)
- [unicode package](https://pkg.go.dev/unicode)

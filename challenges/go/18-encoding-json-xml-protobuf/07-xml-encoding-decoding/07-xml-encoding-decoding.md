# Exercise 07: XML Encoding and Decoding

**Difficulty:** Intermediate | **Estimated Time:** 25 minutes | **Section:** 18 - Encoding

## Overview

XML remains common in enterprise systems, RSS feeds, SOAP services, and configuration files. Go's `encoding/xml` package mirrors the `encoding/json` API but adds XML-specific features: attributes, namespaces, character data, and nested element control through struct tags.

## Prerequisites

- Exercises 01-02 (JSON encoding patterns)
- Struct tags

## Key Differences from JSON

| Feature | JSON tag | XML tag |
|---------|----------|---------|
| Field name | `json:"name"` | `xml:"name"` |
| Attribute | N/A | `xml:"name,attr"` |
| Character data | N/A | `xml:",chardata"` |
| Inner XML | N/A | `xml:",innerxml"` |
| Omit empty | `json:",omitempty"` | `xml:",omitempty"` |
| Comment | N/A | `xml:",comment"` |
| Root element | N/A | `XMLName xml.Name` |

## Task

Build an RSS feed parser and generator:

### Part 1: Define RSS structs

Model a simplified RSS 2.0 feed:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Go Blog</title>
    <link>https://go.dev/blog</link>
    <description>The Go Programming Language Blog</description>
    <item>
      <title>Go 1.23 Released</title>
      <link>https://go.dev/blog/go1.23</link>
      <description>Announcing Go 1.23</description>
      <pubDate>Tue, 13 Aug 2025 00:00:00 UTC</pubDate>
      <guid isPermaLink="true">https://go.dev/blog/go1.23</guid>
    </item>
    <item>
      <title>Structured Logging</title>
      <link>https://go.dev/blog/slog</link>
      <description>Introducing slog</description>
      <pubDate>Mon, 20 Jan 2025 00:00:00 UTC</pubDate>
      <guid isPermaLink="false">slog-post-2025</guid>
    </item>
  </channel>
</rss>
```

You need structs for: `RSS` (root with version attribute), `Channel`, `Item`, and `GUID` (with `isPermaLink` attribute and character data for the value).

### Part 2: Unmarshal the XML

Parse the XML above into your structs. Print the feed title and each item's title and publication date.

### Part 3: Generate XML

Create a new feed programmatically with at least two items. Marshal it to XML with `xml.MarshalIndent`. Prepend the XML header using `xml.Header`.

### Part 4: Attributes and namespaces

Add an `atom:link` element to the channel with a namespace:

```xml
<atom:link href="https://go.dev/blog/feed.xml" rel="self" type="application/rss+xml"
           xmlns:atom="http://www.w3.org/2005/Atom"/>
```

Model this as a struct with `XMLName xml.Name` specifying the namespace.

## Hints

- `XMLName xml.Name \`xml:"rss"\`` controls the root element name.
- For the `version` attribute: `Version string \`xml:"version,attr"\``.
- `GUID` needs both `xml:",chardata"` for the text content and `xml:"isPermaLink,attr"` for the attribute.
- `xml.Header` is the constant `<?xml version="1.0" encoding="UTF-8"?>` with a newline.
- For namespaced elements, set `XMLName: xml.Name{Space: "http://www.w3.org/2005/Atom", Local: "link"}`.
- `xml.MarshalIndent` works just like `json.MarshalIndent`.

## Verification

After unmarshaling, print:

```
Feed: Go Blog
  [1] Go 1.23 Released (Tue, 13 Aug 2025 00:00:00 UTC)
  [2] Structured Logging (Mon, 20 Jan 2025 00:00:00 UTC)
```

The generated XML should be valid and re-parseable by your own code.

## Key Takeaways

- `encoding/xml` mirrors `encoding/json` but adds `attr`, `chardata`, `innerxml`, and namespace support
- `XMLName xml.Name` controls the element name and namespace of a struct
- Attributes use the `attr` tag option
- `xml:",chardata"` captures the text content of an element
- Always prepend `xml.Header` when generating standalone XML documents

# 15. WebAssembly with Rust

**Difficulty**: Avanzado

## Prerequisites

- Cargo workspace and crate structure
- Traits and generics
- Serde serialization/deserialization
- Basic familiarity with JavaScript and npm
- Understanding of memory ownership in Rust

## Introduction

WebAssembly (WASM) is a binary instruction format that runs in browsers and
standalone runtimes at near-native speed. Rust is one of the best-supported
languages for WASM because of its lack of a garbage collector, small runtime,
and fine-grained control over memory layout.

This exercise covers the full workflow: compiling Rust to WASM, bridging the
Rust/JS boundary with wasm-bindgen, managing memory across the two worlds,
optimizing binary size, and making production-ready architectural decisions.

WASM is not a silver bullet. The interop boundary has real costs, and not every
workload benefits from moving to WASM. Understanding when and where to use it
is as important as knowing how.

## Tooling Setup

### wasm-pack

wasm-pack is the standard build tool for Rust-to-WASM projects. It handles
compilation, wasm-bindgen glue generation, and npm package creation.

```bash
# Install wasm-pack
curl https://rustwasm.github.io/wasm-pack/installer/init.sh -sSf | sh

# Or via cargo
cargo install wasm-pack
```

### wasm-opt (via binaryen)

wasm-opt applies WASM-specific optimizations that LLVM does not cover.

```bash
# macOS
brew install binaryen

# Ubuntu/Debian
sudo apt install binaryen

# Or download from https://github.com/WebAssembly/binaryen/releases
```

### Project Structure

```bash
cargo new --lib wasm-demo
cd wasm-demo
```

```toml
# Cargo.toml
[package]
name = "wasm-demo"
version = "0.1.0"
edition = "2021"

[lib]
crate-type = ["cdylib", "rlib"]

[dependencies]
wasm-bindgen = "0.2"
serde = { version = "1", features = ["derive"] }
serde-wasm-bindgen = "0.6"
js-sys = "0.3"
web-sys = { version = "0.3", features = ["console", "Window", "Document",
    "HtmlElement", "Element", "Performance"] }

[dev-dependencies]
wasm-bindgen-test = "0.3"

[profile.release]
opt-level = "z"       # optimize for size
lto = true
codegen-units = 1
strip = true
```

Key points about `crate-type`:
- `cdylib`: produces a dynamic library suitable for WASM (required).
- `rlib`: allows the crate to also be used as a normal Rust library (useful for
  testing business logic without WASM).

## Problem Statement

Build a Rust-powered Markdown-to-HTML converter that runs in the browser via WASM.
The converter must:

1. Accept a Markdown string from JavaScript.
2. Parse and convert it to HTML in Rust.
3. Return the HTML string to JavaScript.
4. Expose a streaming API that processes chunks without copying the entire document.
5. Maintain internal state (parsed document tree) across calls.

This exercises the key WASM challenges: string passing across the boundary,
persistent state, memory management, and API design for interop.

### Naive Implementation

```rust
// src/lib.rs
use wasm_bindgen::prelude::*;

#[wasm_bindgen]
pub fn markdown_to_html(input: &str) -> String {
    let mut html = String::new();
    for line in input.lines() {
        if let Some(heading) = line.strip_prefix("# ") {
            html.push_str(&format!("<h1>{}</h1>\n", heading));
        } else if let Some(heading) = line.strip_prefix("## ") {
            html.push_str(&format!("<h2>{}</h2>\n", heading));
        } else if let Some(heading) = line.strip_prefix("### ") {
            html.push_str(&format!("<h3>{}</h3>\n", heading));
        } else if let Some(item) = line.strip_prefix("- ") {
            html.push_str(&format!("<li>{}</li>\n", item));
        } else if line.starts_with("```") {
            html.push_str("<pre><code>");
        } else if line.is_empty() {
            html.push_str("<br/>\n");
        } else {
            html.push_str(&format!("<p>{}</p>\n", line));
        }
    }
    html
}
```

Build and test:

```bash
wasm-pack build --target web
```

This produces a `pkg/` directory containing:
- `wasm_demo_bg.wasm` -- the compiled WASM binary
- `wasm_demo.js` -- JavaScript glue code
- `wasm_demo.d.ts` -- TypeScript type declarations
- `package.json` -- ready for npm publish

### JavaScript Consumer

```html
<!-- index.html -->
<!DOCTYPE html>
<html>
<head><title>WASM Markdown</title></head>
<body>
  <textarea id="input" rows="10" cols="60"># Hello WASM
This is a paragraph.

- Item one
- Item two</textarea>
  <div id="output"></div>

  <script type="module">
    import init, { markdown_to_html } from './pkg/wasm_demo.js';

    async function main() {
      await init();

      const input = document.getElementById('input');
      const output = document.getElementById('output');

      input.addEventListener('input', () => {
        output.innerHTML = markdown_to_html(input.value);
      });

      // Trigger initial render
      output.innerHTML = markdown_to_html(input.value);
    }

    main();
  </script>
</body>
</html>
```

Serve locally:

```bash
# Any static file server works
python3 -m http.server 8080
# Open http://localhost:8080
```

## The Memory Model: Rust vs JavaScript

Understanding memory across the WASM boundary is critical. Here is how it works:

```
+---------------------------+      +------------------------+
|   JavaScript Heap         |      |   WASM Linear Memory   |
|                           |      |                        |
|  JS String "hello"        |      |  Rust Vec<u8>          |
|  JS Object { ... }        |      |  Rust String           |
|  ArrayBuffer (view into --+------+-> raw bytes            |
|    WASM memory)           |      |                        |
+---------------------------+      +------------------------+
```

Key rules:

1. **Strings are copied** across the boundary. When you pass a JS string to Rust,
   wasm-bindgen encodes it as UTF-8 into WASM linear memory. When Rust returns a
   String, it is copied back to the JS heap as a JS string. There is no zero-copy
   string sharing.

2. **Typed arrays can share memory**. A `Uint8Array` can be a view into WASM linear
   memory, avoiding copies. But the view is invalidated if WASM memory grows (which
   happens when Rust allocates).

3. **Rust owns WASM memory**. JavaScript cannot directly free Rust allocations.
   If you pass a struct to JS, you must provide an explicit `free()` method or
   use wasm-bindgen's drop-glue mechanism.

4. **No garbage collection for WASM objects**. If JS holds a reference to a Rust
   struct (via wasm-bindgen), that struct is never freed until JS calls `.free()`.
   Memory leaks are easy to create.

## Hints

### Hint 1: Stateful Converter with `#[wasm_bindgen]` Struct

Instead of a free function that re-parses every time, expose a struct that holds
parsed state:

```rust
#[wasm_bindgen]
pub struct MarkdownConverter {
    // internal parsed representation
    buffer: String,
    line_count: usize,
}
```

Think about how JS will create, use, and free this struct.

### Hint 2: Passing Complex Data with Serde

For structured data (e.g., a table of contents), use `serde-wasm-bindgen` instead
of manually converting to `JsValue`:

```rust
use serde::Serialize;

#[derive(Serialize)]
pub struct TocEntry {
    level: u8,
    text: String,
    id: String,
}
```

Think about when to serialize in Rust vs letting JS traverse the data.

### Hint 3: Avoiding Copies with Views

For large binary data (images, buffers), returning a `Vec<u8>` copies the data.
Instead, expose a pointer and length, and let JS create a view:

```rust
#[wasm_bindgen]
pub fn get_buffer_ptr(converter: &MarkdownConverter) -> *const u8 { ... }

#[wasm_bindgen]
pub fn get_buffer_len(converter: &MarkdownConverter) -> usize { ... }
```

Think about what invalidates these pointers.

### Hint 4: Binary Size

The default release build is larger than necessary. Think about:
- Which features of `web-sys` you actually need
- Whether `console_error_panic_hook` is worth the bytes
- What `wasm-opt` can strip

### Hint 5: Testing Without a Browser

`wasm-bindgen-test` runs in Node.js or headless Chrome. Think about how to
structure code so core logic is testable as plain Rust without WASM.

---

## Solution Approach

### Step 1: Stateful Converter

```rust
use wasm_bindgen::prelude::*;
use serde::Serialize;

#[derive(Serialize, Clone)]
pub struct TocEntry {
    pub level: u8,
    pub text: String,
    pub id: String,
}

#[wasm_bindgen]
pub struct MarkdownConverter {
    source: String,
    toc: Vec<TocEntry>,
    html_cache: Option<String>,
}

#[wasm_bindgen]
impl MarkdownConverter {
    #[wasm_bindgen(constructor)]
    pub fn new() -> Self {
        // Install better panic messages in debug builds
        #[cfg(debug_assertions)]
        console_error_panic_hook::set_once();

        Self {
            source: String::new(),
            toc: Vec::new(),
            html_cache: None,
        }
    }

    /// Set the full markdown source. Invalidates the cache.
    pub fn set_source(&mut self, markdown: &str) {
        self.source = markdown.to_string();
        self.html_cache = None;
        self.toc.clear();
    }

    /// Append to the existing source (streaming use case).
    pub fn append(&mut self, chunk: &str) {
        self.source.push_str(chunk);
        self.html_cache = None;
        // TOC is only rebuilt on full render
    }

    /// Render to HTML. Caches the result until source changes.
    pub fn render(&mut self) -> String {
        if let Some(ref cached) = self.html_cache {
            return cached.clone();
        }

        let html = self.parse_and_render();
        self.html_cache = Some(html.clone());
        html
    }

    /// Return the table of contents as a JS value (via serde).
    pub fn table_of_contents(&self) -> JsValue {
        serde_wasm_bindgen::to_value(&self.toc).unwrap_or(JsValue::NULL)
    }

    /// Explicitly free memory. Call from JS when done.
    /// wasm-bindgen generates this automatically via Drop, but being
    /// explicit makes the API clearer.
    pub fn dispose(self) {
        // Drop runs here, freeing all owned memory in WASM.
    }
}
```

### Step 2: Core Parsing Logic (No WASM Dependencies)

Separate the parsing logic so it can be tested as pure Rust:

```rust
// src/parser.rs -- no wasm_bindgen imports
use crate::TocEntry;

pub fn parse_markdown(source: &str) -> (String, Vec<TocEntry>) {
    let mut html = String::with_capacity(source.len());
    let mut toc = Vec::new();
    let mut in_code_block = false;
    let mut in_list = false;
    let mut heading_counter: u32 = 0;

    for line in source.lines() {
        // Code blocks
        if line.starts_with("```") {
            if in_code_block {
                html.push_str("</code></pre>\n");
                in_code_block = false;
            } else {
                if in_list {
                    html.push_str("</ul>\n");
                    in_list = false;
                }
                let lang = line.trim_start_matches('`').trim();
                if lang.is_empty() {
                    html.push_str("<pre><code>");
                } else {
                    html.push_str(&format!("<pre><code class=\"language-{}\">", lang));
                }
                in_code_block = true;
            }
            continue;
        }

        if in_code_block {
            // Escape HTML inside code blocks
            let escaped = line
                .replace('&', "&amp;")
                .replace('<', "&lt;")
                .replace('>', "&gt;");
            html.push_str(&escaped);
            html.push('\n');
            continue;
        }

        // Headings
        if let Some((level, text)) = parse_heading(line) {
            if in_list {
                html.push_str("</ul>\n");
                in_list = false;
            }
            heading_counter += 1;
            let id = slug(text);
            toc.push(TocEntry {
                level,
                text: text.to_string(),
                id: id.clone(),
            });
            html.push_str(&format!(
                "<h{level} id=\"{id}\">{text}</h{level}>\n",
                level = level,
                id = id,
                text = text,
            ));
            continue;
        }

        // List items
        if let Some(item) = line.strip_prefix("- ") {
            if !in_list {
                html.push_str("<ul>\n");
                in_list = true;
            }
            html.push_str(&format!("<li>{}</li>\n", item));
            continue;
        }

        // Close list if previous line was a list item
        if in_list {
            html.push_str("</ul>\n");
            in_list = false;
        }

        // Empty lines
        if line.trim().is_empty() {
            html.push_str("<br/>\n");
            continue;
        }

        // Bold and italic
        let processed = process_inline(line);
        html.push_str(&format!("<p>{}</p>\n", processed));
    }

    // Close any open blocks
    if in_code_block {
        html.push_str("</code></pre>\n");
    }
    if in_list {
        html.push_str("</ul>\n");
    }

    (html, toc)
}

fn parse_heading(line: &str) -> Option<(u8, &str)> {
    let level = line.bytes().take_while(|&b| b == b'#').count();
    if level == 0 || level > 6 {
        return None;
    }
    let rest = &line[level..];
    if !rest.starts_with(' ') {
        return None;
    }
    Some((level as u8, rest.trim()))
}

fn slug(text: &str) -> String {
    text.to_lowercase()
        .chars()
        .map(|c| if c.is_alphanumeric() { c } else { '-' })
        .collect::<String>()
        .trim_matches('-')
        .to_string()
}

fn process_inline(text: &str) -> String {
    // Simple bold (**text**) and italic (*text*) processing.
    // A production parser would use a proper state machine.
    let mut result = String::with_capacity(text.len());
    let chars: Vec<char> = text.chars().collect();
    let mut i = 0;

    while i < chars.len() {
        if i + 1 < chars.len() && chars[i] == '*' && chars[i + 1] == '*' {
            // Bold
            if let Some(end) = find_closing(&chars, i + 2, "**") {
                result.push_str("<strong>");
                result.extend(&chars[i + 2..end]);
                result.push_str("</strong>");
                i = end + 2;
                continue;
            }
        }
        if chars[i] == '*' {
            // Italic
            if let Some(end) = find_closing_char(&chars, i + 1, '*') {
                result.push_str("<em>");
                result.extend(&chars[i + 1..end]);
                result.push_str("</em>");
                i = end + 1;
                continue;
            }
        }
        result.push(chars[i]);
        i += 1;
    }
    result
}

fn find_closing(chars: &[char], start: usize, pattern: &str) -> Option<usize> {
    let pat: Vec<char> = pattern.chars().collect();
    for i in start..chars.len() - pat.len() + 1 {
        if chars[i..i + pat.len()] == pat[..] {
            return Some(i);
        }
    }
    None
}

fn find_closing_char(chars: &[char], start: usize, ch: char) -> Option<usize> {
    for i in start..chars.len() {
        if chars[i] == ch {
            return Some(i);
        }
    }
    None
}
```

Wire it into the WASM struct:

```rust
// In MarkdownConverter impl
impl MarkdownConverter {
    fn parse_and_render(&mut self) -> String {
        let (html, toc) = parser::parse_markdown(&self.source);
        self.toc = toc;
        html
    }
}
```

### Step 3: Zero-Copy Buffer Access

For large binary outputs (e.g., a rendered PDF or image), avoid copying through
the JS/WASM boundary:

```rust
#[wasm_bindgen]
pub struct WasmBuffer {
    data: Vec<u8>,
}

#[wasm_bindgen]
impl WasmBuffer {
    /// Returns a pointer into WASM linear memory.
    /// JS can create a Uint8Array view from this.
    pub fn ptr(&self) -> *const u8 {
        self.data.as_ptr()
    }

    pub fn len(&self) -> usize {
        self.data.len()
    }

    /// Allocate a buffer from the JS side.
    pub fn alloc(size: usize) -> WasmBuffer {
        WasmBuffer {
            data: vec![0u8; size],
        }
    }

    /// Write data from JS into the buffer (JS calls this with offset + bytes).
    pub fn write_byte(&mut self, offset: usize, value: u8) {
        if offset < self.data.len() {
            self.data[offset] = value;
        }
    }

    pub fn dispose(self) {
        drop(self);
    }
}
```

JavaScript usage:

```javascript
import init, { WasmBuffer } from './pkg/wasm_demo.js';

async function main() {
    const wasm = await init();

    const buf = WasmBuffer.alloc(1024);
    const ptr = buf.ptr();
    const len = buf.len();

    // Create a view into WASM memory -- zero copy
    const view = new Uint8Array(wasm.memory.buffer, ptr, len);

    // Read data directly from WASM memory
    console.log(view[0], view[1]);

    // IMPORTANT: this view is invalidated if Rust allocates more memory
    // (which causes memory.buffer to be detached and replaced).
    // Always re-create the view after any call that might allocate.

    buf.free();  // Release WASM memory
}
```

**Critical caveat**: When WASM linear memory grows (any Rust allocation can
trigger this), all existing `Uint8Array` views into `wasm.memory.buffer` become
invalid. Always re-create views after calling into WASM.

### Step 4: Testing

#### Pure Rust Tests (no WASM required)

```rust
#[cfg(test)]
mod tests {
    use super::parser::parse_markdown;

    #[test]
    fn test_heading_parsing() {
        let (html, toc) = parse_markdown("# Hello World");
        assert!(html.contains("<h1"));
        assert!(html.contains("Hello World"));
        assert_eq!(toc.len(), 1);
        assert_eq!(toc[0].level, 1);
    }

    #[test]
    fn test_code_block_escaping() {
        let input = "```\n<script>alert('xss')</script>\n```";
        let (html, _) = parse_markdown(input);
        assert!(html.contains("&lt;script&gt;"));
        assert!(!html.contains("<script>"));
    }

    #[test]
    fn test_nested_list() {
        let input = "- one\n- two\n- three";
        let (html, _) = parse_markdown(input);
        assert!(html.contains("<ul>"));
        assert_eq!(html.matches("<li>").count(), 3);
    }

    #[test]
    fn test_inline_bold() {
        let (html, _) = parse_markdown("This is **bold** text.");
        assert!(html.contains("<strong>bold</strong>"));
    }

    #[test]
    fn test_empty_input() {
        let (html, toc) = parse_markdown("");
        assert!(html.is_empty() || html.trim().is_empty());
        assert!(toc.is_empty());
    }
}
```

Run without any WASM tooling:

```bash
cargo test
```

#### WASM-Specific Tests

```rust
// tests/wasm.rs
use wasm_bindgen_test::*;
wasm_bindgen_test_configure!(run_in_browser);

use wasm_demo::MarkdownConverter;

#[wasm_bindgen_test]
fn test_converter_lifecycle() {
    let mut converter = MarkdownConverter::new();
    converter.set_source("# Test\nParagraph.");
    let html = converter.render();

    assert!(html.contains("<h1"));
    assert!(html.contains("Paragraph"));

    // Second call should return cached result
    let html2 = converter.render();
    assert_eq!(html, html2);

    converter.dispose();
}

#[wasm_bindgen_test]
fn test_streaming_append() {
    let mut converter = MarkdownConverter::new();
    converter.set_source("# Part 1\n");
    converter.append("## Part 2\n");
    let html = converter.render();

    assert!(html.contains("<h1"));
    assert!(html.contains("<h2"));
}

#[wasm_bindgen_test]
fn test_toc_returns_js_value() {
    let mut converter = MarkdownConverter::new();
    converter.set_source("# First\n## Second\n### Third");
    converter.render();  // triggers TOC build
    let toc = converter.table_of_contents();

    // toc is a JsValue; in a real test you'd check the JS array
    assert!(!toc.is_null());
}
```

Run:

```bash
wasm-pack test --headless --chrome
# or
wasm-pack test --node
```

### Step 5: Size Optimization

The default WASM binary is often 200-800 KB. For web delivery, every byte counts.

#### Cargo.toml Profile

```toml
[profile.release]
opt-level = "z"       # optimize for binary size (not speed)
lto = true            # link-time optimization across all crates
codegen-units = 1     # slower compile, better optimization
strip = true          # strip debug symbols
panic = "abort"       # no unwinding machinery
```

#### wasm-opt Post-Processing

```bash
# Build first
wasm-pack build --release --target web

# Then optimize further
wasm-opt -Oz -o pkg/wasm_demo_bg_opt.wasm pkg/wasm_demo_bg.wasm

# Compare sizes
ls -lh pkg/wasm_demo_bg.wasm
ls -lh pkg/wasm_demo_bg_opt.wasm
```

Typical reductions:

| Stage                         | Size   |
|-------------------------------|--------|
| Debug build                   | ~2 MB  |
| Release build                 | ~180 KB|
| Release + opt-level="z" + LTO| ~90 KB |
| After wasm-opt -Oz            | ~70 KB |
| After gzip                    | ~25 KB |

#### Feature-Gate web-sys

Every web-sys feature you enable adds to the binary. Only enable what you use:

```toml
# Bad: pulls in everything
web-sys = { version = "0.3", features = ["Window", "Document", "Element",
    "HtmlElement", "Performance", "Navigator", "Gpu", "AudioContext",
    "WebGlRenderingContext"] }

# Good: minimal surface
web-sys = { version = "0.3", features = ["console"] }
```

#### Audit Dependencies

```bash
cargo tree --target wasm32-unknown-unknown

# Check which crates contribute the most to binary size:
# Build with --emit=llvm-ir, or use twiggy:
cargo install twiggy
twiggy top pkg/wasm_demo_bg.wasm
twiggy dominators pkg/wasm_demo_bg.wasm
```

`twiggy` shows exactly which functions and data sections consume the most space.

## Trade-Off Analysis

| Decision | Option A | Option B | Guidance |
|----------|----------|----------|----------|
| String passing | Copy across boundary (safe, simple) | Shared buffer + pointer (zero-copy, unsafe view lifetime) | Copy for strings < 100KB; shared buffer for large binary data |
| State management | Free functions (stateless) | `#[wasm_bindgen]` struct (stateful) | Stateful when JS needs to maintain a session or cache parsed state |
| Serialization | `serde-wasm-bindgen` (rich types) | Manual `JsValue` construction | Serde for complex types; manual for single primitives |
| Error handling | Return `Result<T, JsValue>` | Panic (becomes JS exception) | Result for expected errors; panic only for bugs |
| Size vs speed | `opt-level = "z"` | `opt-level = 3` | "z" for web delivery; "3" for compute-heavy WASM in Node/edge |
| Testing | Pure Rust unit tests + wasm-bindgen-test | Only wasm-bindgen-test | Pure Rust for logic; wasm-bindgen-test for interop |
| Build target | `--target web` (ES modules) | `--target bundler` (webpack/vite) | `web` for standalone; `bundler` for apps using a bundler |
| Threading | Single-threaded (default) | `wasm-bindgen-rayon` (SharedArrayBuffer) | Single unless CPU-bound; threading requires COOP/COEP headers |

### When WASM Is Worth It

- **CPU-intensive computation**: image processing, cryptography, parsing, simulation.
- **Reusing existing Rust libraries**: no need to rewrite in JS.
- **Consistent behavior**: same code runs server-side and client-side.
- **Security-sensitive code**: harder to tamper with than plain JS.

### When WASM Is Not Worth It

- **DOM manipulation**: JS is faster because there is no interop overhead.
- **Small utility functions**: the cost of loading the WASM module exceeds the benefit.
- **I/O-bound workloads**: WASM does not speed up network requests or file reads.
- **Simple string processing**: the copy overhead across the boundary may negate gains.

## Production Patterns

### Error Handling Across the Boundary

```rust
use wasm_bindgen::prelude::*;

#[wasm_bindgen]
pub fn parse_config(json: &str) -> Result<JsValue, JsValue> {
    let config: serde_json::Value = serde_json::from_str(json)
        .map_err(|e| JsValue::from_str(&format!("Parse error: {}", e)))?;

    serde_wasm_bindgen::to_value(&config)
        .map_err(|e| JsValue::from_str(&format!("Conversion error: {}", e)))
}
```

On the JS side:

```javascript
try {
    const config = parse_config('{"key": "value"}');
    console.log(config);
} catch (e) {
    // e is the JsValue string from Rust
    console.error("WASM error:", e);
}
```

### Async WASM (with wasm-bindgen-futures)

```toml
[dependencies]
wasm-bindgen-futures = "0.4"
```

```rust
use wasm_bindgen::prelude::*;
use wasm_bindgen_futures::JsFuture;
use web_sys::{Request, RequestInit, Response};

#[wasm_bindgen]
pub async fn fetch_data(url: &str) -> Result<JsValue, JsValue> {
    let mut opts = RequestInit::new();
    opts.method("GET");

    let request = Request::new_with_str_and_init(url, &opts)?;
    let window = web_sys::window().unwrap();
    let resp_value = JsFuture::from(window.fetch_with_request(&request)).await?;
    let resp: Response = resp_value.dyn_into()?;
    let json = JsFuture::from(resp.json()?).await?;

    Ok(json)
}
```

### Logging from WASM

```rust
use web_sys::console;

#[wasm_bindgen]
pub fn process(data: &str) -> String {
    console::log_1(&format!("Processing {} bytes", data.len()).into());

    // Or use the log crate with console_log backend:
    // log::info!("Processing {} bytes", data.len());

    data.to_uppercase()
}
```

### Memory-Conscious Streaming

For processing large files without loading everything into WASM memory:

```rust
#[wasm_bindgen]
pub struct StreamProcessor {
    state: ProcessingState,
    chunk_buffer: Vec<u8>,
}

#[wasm_bindgen]
impl StreamProcessor {
    #[wasm_bindgen(constructor)]
    pub fn new() -> Self {
        Self {
            state: ProcessingState::default(),
            chunk_buffer: Vec::with_capacity(64 * 1024), // 64 KB chunks
        }
    }

    /// Process a chunk. Returns partial results as a JS array.
    pub fn push_chunk(&mut self, chunk: &[u8]) -> Result<JsValue, JsValue> {
        self.chunk_buffer.extend_from_slice(chunk);

        // Process complete records from the buffer
        let results = self.state.process(&mut self.chunk_buffer);

        serde_wasm_bindgen::to_value(&results)
            .map_err(|e| JsValue::from_str(&e.to_string()))
    }

    /// Flush remaining data and finalize.
    pub fn finish(&mut self) -> Result<JsValue, JsValue> {
        let results = self.state.finalize(&mut self.chunk_buffer);
        serde_wasm_bindgen::to_value(&results)
            .map_err(|e| JsValue::from_str(&e.to_string()))
    }
}

// Internal state -- not exposed to JS
#[derive(Default)]
struct ProcessingState {
    records_processed: usize,
}

impl ProcessingState {
    fn process(&mut self, buffer: &mut Vec<u8>) -> Vec<String> {
        let mut results = Vec::new();
        // Process complete lines/records from buffer
        while let Some(pos) = buffer.iter().position(|&b| b == b'\n') {
            let line = String::from_utf8_lossy(&buffer[..pos]).to_string();
            results.push(line);
            buffer.drain(..=pos);
            self.records_processed += 1;
        }
        results
    }

    fn finalize(&mut self, buffer: &mut Vec<u8>) -> Vec<String> {
        let mut results = Vec::new();
        if !buffer.is_empty() {
            results.push(String::from_utf8_lossy(buffer).to_string());
            buffer.clear();
            self.records_processed += 1;
        }
        results
    }
}
```

## Common Mistakes

1. **Copying data across the WASM boundary in a render loop.** Use direct memory
   access (`ptr()` + `Uint8Array` view) instead of returning `Vec<u8>` each frame.

2. **Using `format!` heavily in WASM.** The formatting machinery adds significant
   binary size. For size-critical applications, build strings manually with
   `push_str`.

3. **Forgetting `crate-type = ["cdylib"]`.** Without it, wasm-pack cannot generate
   a WASM module. Keep `"rlib"` alongside it so `cargo test` continues to work.

4. **Not setting `panic = "abort"` for release.** Unwinding support adds 5-10 KB
   to the WASM binary for no benefit (browsers cannot catch WASM panics via
   unwinding).

5. **Holding stale `Uint8Array` views.** After any call into WASM that might
   allocate, the linear memory buffer can be replaced. All existing views become
   detached. Re-create views after every WASM call.

6. **Leaking WASM objects in JS.** Every `#[wasm_bindgen]` struct allocated from
   JS must eventually have `.free()` called. There is no garbage collector for
   WASM heap memory.

## Verification

```bash
# 1. Install tooling
cargo install wasm-pack
rustup target add wasm32-unknown-unknown

# 2. Build the project
wasm-pack build --release --target web

# 3. Run pure Rust tests (no WASM tooling needed)
cargo test

# 4. Run WASM-specific tests
wasm-pack test --node
# or with a headless browser:
wasm-pack test --headless --chrome

# 5. Check binary size
ls -lh pkg/*.wasm

# 6. Optimize the binary
wasm-opt -Oz -o pkg/optimized.wasm pkg/wasm_demo_bg.wasm
ls -lh pkg/optimized.wasm

# 7. Analyze what contributes to binary size
cargo install twiggy
twiggy top pkg/wasm_demo_bg.wasm | head -20

# 8. Serve and test in browser
python3 -m http.server 8080
# Open http://localhost:8080 and test the converter

# 9. Verify TypeScript types were generated
cat pkg/wasm_demo.d.ts
```

## What You Learned

- **wasm-pack** handles the full build pipeline from Rust source to npm-ready
  package, including wasm-bindgen glue generation and TypeScript declarations.
- **The WASM/JS memory boundary** is the central design challenge. Strings are
  always copied. Binary data can be shared via typed array views into linear memory,
  but those views are invalidated when memory grows.
- **Stateful WASM structs** exposed via `#[wasm_bindgen]` let JavaScript maintain
  sessions, caches, and streaming processors, but memory ownership must be managed
  explicitly (call `.free()` when done).
- **Separating core logic from WASM bindings** enables fast unit testing with
  plain `cargo test` while still covering interop with `wasm-bindgen-test`.
- **Binary size optimization** is a multi-layer process: `opt-level = "z"`, LTO,
  `codegen-units = 1`, minimal `web-sys` features, `wasm-opt`, and `twiggy` for
  analysis. Each layer removes a different category of bloat.
- **serde-wasm-bindgen** bridges structured data across the boundary without manual
  `JsValue` construction, but adds to binary size -- use it for complex types and
  manual conversion for simple ones.
- **Not everything belongs in WASM**. DOM manipulation, I/O, and simple logic are
  better left in JavaScript. WASM shines for CPU-intensive, memory-controlled
  computation where Rust's performance and safety guarantees matter.
- **The trade-off mindset** is essential: every decision (copy vs share, size vs
  speed, stateful vs stateless) has a cost. Profiling and measuring -- not
  guessing -- determines the right choice for your workload.

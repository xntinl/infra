<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 40h
-->

# Go Language Server (LSP)

## The Challenge

Build a Language Server Protocol (LSP) implementation for Go that provides IDE features -- code completion, go-to-definition, find references, hover documentation, diagnostics, rename refactoring, and code actions -- by parsing and analyzing Go source code. The Language Server Protocol standardizes the communication between code editors and language-specific analysis servers, enabling a single implementation to work with VS Code, Neovim, Emacs, and any LSP-compatible editor. Your server must parse Go source code using the `go/ast`, `go/parser`, `go/types`, and `go/token` standard library packages, perform type checking to resolve identifiers, and respond to LSP requests over JSON-RPC 2.0 transported via stdin/stdout. This is a massive project that touches parsing, type systems, protocol design, and incremental computation.

## Requirements

1. Implement the LSP transport layer: read and write JSON-RPC 2.0 messages over stdin/stdout with the LSP `Content-Length` header framing, supporting both requests (with ID, expecting a response), notifications (without ID), and responses (matching request ID).
2. Implement the `initialize` handshake: respond to the client's `initialize` request with server capabilities declaring support for completion, hover, definition, references, rename, diagnostics, and document synchronization (full and incremental).
3. Implement document synchronization: handle `textDocument/didOpen`, `textDocument/didChange` (both full and incremental content changes), and `textDocument/didClose` notifications, maintaining an in-memory copy of all open files.
4. Implement `textDocument/completion`: at a given cursor position, analyze the AST and type information to provide context-aware completions -- struct field names after a dot, package-level identifiers after a package name, local variables in scope, keywords in the appropriate context, and function signatures with parameter snippets.
5. Implement `textDocument/definition`: resolve the identifier under the cursor to its declaration location (file and position) by using `go/types.Info.Uses` and `go/types.Info.Defs` maps, handling cross-file and cross-package navigation.
6. Implement `textDocument/hover`: display the type signature and documentation comment for the identifier under the cursor, extracted from the AST and type information.
7. Implement `textDocument/publishDiagnostics`: after every file change, re-parse and type-check the file, converting parse errors and type errors into LSP diagnostic objects with severity, range, and message, and pushing them to the client.
8. Implement `textDocument/references`: find all references to the identifier under the cursor across all files in the workspace by walking the AST of every file and checking `go/types.Info.Uses` for identifiers that resolve to the same object.
9. Implement `textDocument/rename`: rename the identifier under the cursor and all its references across the workspace, returning a `WorkspaceEdit` with text edits for every affected file; validate that the new name is a valid Go identifier and does not conflict with existing names in scope.

## Hints

- Use `go/parser.ParseFile` with `parser.ParseComments` to get both the AST and doc comments; use `go/types.Config.Check` to type-check parsed files and populate the `types.Info` struct.
- The `types.Info` struct contains: `Defs` (identifier -> object for declarations), `Uses` (identifier -> object for references), `Types` (expression -> type), and `Scopes` (node -> scope).
- For cross-package analysis, use `golang.org/x/tools/go/packages` to load the full dependency graph, or implement a simpler workspace-level analysis that only handles files in the current directory.
- JSON-RPC 2.0 messages: `{"jsonrpc":"2.0","id":1,"method":"textDocument/completion","params":{...}}` for requests; responses include `{"jsonrpc":"2.0","id":1,"result":{...}}`.
- The `Content-Length` header: `Content-Length: 123\r\n\r\n{...json...}`.
- For incremental updates, maintain the file content as a `[]byte` and apply edits by replacing the specified range (line/character offsets converted to byte offsets).
- Test with VS Code by creating a `.vscode/settings.json` that points to your LSP binary, or use the `vim.lsp` Neovim API.
- Start with a minimal implementation (diagnostics + hover) and add features incrementally.

## Success Criteria

1. The LSP server starts, completes the `initialize` handshake with an editor, and does not crash on any standard lifecycle event.
2. Diagnostics appear in the editor within 1 second of a file change, correctly highlighting parse errors and type errors at the right positions.
3. Hover on a function name shows its signature and doc comment.
4. Go-to-definition on a function call navigates to the function's declaration, including cross-file navigation within the same package.
5. Code completion after a dot on a struct variable shows the struct's field names and methods.
6. Find references on a function name returns all call sites across the workspace.
7. Rename correctly updates the identifier and all references, producing valid Go code.
8. The server handles files with thousands of lines without noticeable lag (type-checking completes within 500 ms for a typical package).
9. The server works correctly with at least one real LSP client (VS Code or Neovim).

## Research Resources

- Language Server Protocol specification -- https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/
- JSON-RPC 2.0 specification -- https://www.jsonrpc.org/specification
- Go `go/ast`, `go/parser`, `go/types`, `go/token` package documentation
- gopls (official Go LSP server) source code -- https://github.com/golang/tools/tree/master/gopls -- reference implementation
- `golang.org/x/tools/go/packages` for workspace loading
- "Language Server Protocol and Implementation" -- LSP tutorial
- VS Code LSP client extension guide -- https://code.visualstudio.com/api/language-extensions/language-server-extension-guide

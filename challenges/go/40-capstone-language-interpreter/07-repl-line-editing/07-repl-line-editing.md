<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 2h
-->

# REPL with Line Editing

A Read-Eval-Print Loop is the interactive face of your language -- the environment where users experiment, debug, and explore. A bare-bones REPL that uses `fmt.Scanln` is functional but painful. Your task is to build a production-quality REPL with GNU Readline-style line editing, persistent history, multi-line input detection, tab completion, syntax highlighting, pretty-printed output with colors, special REPL commands, and session state management. The REPL must feel polished enough that a user would genuinely want to use it for interactive programming.

## Requirements

1. Implement line editing with keyboard navigation: left/right arrow keys to move the cursor within the current line, Home/End to jump to beginning/end of line, Ctrl+A/Ctrl+E as alternatives, Ctrl+W to delete word backward, Ctrl+U to delete to beginning of line, Ctrl+K to delete to end of line, Ctrl+L to clear screen, and Ctrl+D on empty line to exit. Read raw terminal input by putting the terminal into raw mode using `golang.org/x/term` or `syscall.RawMode`, and process escape sequences for arrow keys and special keys.

2. Implement command history: up/down arrow keys navigate through previously entered lines. Persist history to `~/.monkey_history` (or a configurable path) across sessions, loading on startup and appending on each new entry. Implement `Ctrl+R` for reverse incremental search through history: as the user types a search string, show the most recent matching history entry, with repeated `Ctrl+R` cycling through older matches. Limit history to a configurable maximum (default 10,000 entries).

3. Implement multi-line input detection: when the user enters an incomplete expression (unclosed parenthesis, bracket, brace, or an unterminated string), display a continuation prompt (`...`) and continue reading lines until the expression is complete. Track bracket/brace/paren depth using a simple counter -- do not require full parsing for continuation detection, as that would be too expensive for every keystroke. An explicit empty line after a continuation prompt cancels the multi-line input.

4. Implement tab completion for: built-in function names, user-defined variable names currently in scope, keywords (let, fn, if, else, return, while, for, etc.), and file paths when the cursor is inside a string argument to `readFile` or `writeFile`. Display multiple completions in a columnar format below the prompt if there are more than one match. A single match completes inline. Implement this as a pluggable `Completer` interface so new completion sources can be added.

5. Implement syntax highlighting of the input line: as the user types, colorize keywords (bold blue), string literals (green), numeric literals (yellow), operators (cyan), comments (gray), identifiers that match defined variables (white), and unknown identifiers (red, as a hint they may be undefined). Use ANSI escape codes for colors. The highlighting must update in real-time as the user types, without flickering. Implement this by re-rendering the entire line after each keystroke with the cursor repositioned correctly.

6. Implement REPL-specific commands prefixed with a colon: `:help` displays available commands and keyboard shortcuts, `:history [n]` displays the last N history entries, `:clear` clears all variables from the current environment, `:load <file>` reads and evaluates a source file in the current environment, `:save <file>` saves the current session's input history to a file, `:ast <expr>` parses the expression and displays the AST tree, `:tokens <expr>` tokenizes the expression and displays all tokens, `:time <expr>` evaluates the expression and displays the execution time, `:type <expr>` evaluates and displays the result's type.

7. Implement pretty-printed output: results are displayed with syntax-aware formatting. Integers and floats in their natural representation. Strings in double quotes with escape sequences displayed. Arrays formatted as `[1, 2, 3]` with line-wrapping for long arrays and indented multi-line formatting for nested arrays. Hashes formatted as `{"key": value, ...}` with sorted keys. Functions displayed as `fn(params) { ... }` with the parameter names. Null displayed in a distinct color. Errors displayed in red with the full stack trace indented.

8. Write tests covering: line editing operations (simulate keystrokes and verify buffer state), history navigation (add entries, go up/down, verify correct entry displayed), multi-line detection (unclosed brace triggers continuation, closed brace completes), tab completion (verify correct completions for partial identifiers), REPL command parsing and execution (`:help`, `:ast`, `:load`), pretty-printing of all object types, session persistence (write history, create new REPL, verify history loaded), and an integration test that scripts a complete REPL session by simulating terminal input/output.

## Hints

- For raw terminal input, `golang.org/x/term.MakeRaw()` puts the terminal in raw mode, returning the old state for restoration. Read bytes one at a time from `os.Stdin`. Escape sequences start with `\x1b[` followed by a character code (A=up, B=down, C=right, D=left).
- For real-time syntax highlighting, after each keystroke: tokenize the current input line, map each token to its color, rebuild the display string with ANSI color codes, move the cursor to the beginning of the line (`\r`), write the colored string, then reposition the cursor to its logical position.
- Multi-line detection with bracket counting: maintain counters for `(`, `[`, `{` depth. On newline, if any counter is positive, continue to the next line. Reset on empty continuation line.
- For tab completion, maintain a `[]string` of all identifiers currently bound in the environment, plus all built-in names, plus all keywords. Use `strings.HasPrefix` to filter completions.
- To avoid flickering during re-rendering, clear the line with `\r\x1b[K` (carriage return + clear to end of line), write the new content, then set cursor position with `\x1b[{n}G` (move cursor to column n).
- For testing terminal I/O, create a `Terminal` interface that wraps raw I/O, and provide a mock implementation that uses `bytes.Buffer` for input/output. This allows scripting test sessions without a real terminal.

## Success Criteria

1. Arrow keys, Home/End, and Ctrl shortcuts correctly manipulate the input line without visual artifacts.
2. History persists across REPL sessions: close and reopen the REPL, press up arrow, and see the last entry from the previous session.
3. Multi-line input correctly detects unclosed brackets and continues until the expression is complete.
4. Tab completion shows correct candidates for partially typed identifiers and completes inline for unique matches.
5. Syntax highlighting colors keywords, strings, numbers, and operators distinctly without flickering during typing.
6. All REPL commands work correctly: `:load` evaluates a file, `:ast` shows the tree, `:time` shows execution duration.
7. Pretty-printed output is readable and correctly formatted for nested data structures.
8. The REPL handles rapid input, paste operations, and edge cases (empty input, very long lines, binary data) without crashing.

## Research Resources

- [golang.org/x/term Package](https://pkg.go.dev/golang.org/x/term)
- [ANSI Escape Codes Reference](https://gist.github.com/fnky/458719343aabd01cfb17a3a4f7296797)
- [Build Your Own Command Line (terminal raw mode)](https://viewsourcecode.org/snaptoken/kilo/)
- [GNU Readline Documentation](https://tiswww.case.edu/php/chet/readline/readline.html)
- [Writing An Interpreter In Go - REPL Chapter (Thorsten Ball)](https://interpreterbook.com/)
- [go-prompt - Interactive Prompt Library](https://github.com/c-bata/go-prompt)

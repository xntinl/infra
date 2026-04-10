<!--
type: reference
difficulty: advanced
section: [05-compilers-and-runtimes]
concepts: [lexer, tokenizer, pratt-parser, top-down-operator-precedence, peg-grammars, error-recovery, recursive-descent]
languages: [go, rust]
estimated_reading_time: 60-75 min
bloom_level: analyze
prerequisites: [state-machines, recursive-data-structures, go-interfaces, rust-enums]
papers: [Pratt-1973-Top-Down-Operator-Precedence, Ford-2004-PEG-Grammars]
industry_use: [go-compiler, rustc, clang, tree-sitter, pest]
language_contrast: medium
-->

# Lexing and Parsing

> The parser is the contract between your language's syntax and the rest of the compiler — get it wrong and every diagnostic, IDE feature, and optimization downstream pays the price.

## Mental Model

A lexer is a state machine over a byte stream. It has one job: group bytes into tokens with a type and a source span. It knows nothing about grammar, operator precedence, or meaning. The reason compilers separate lexing from parsing is not historical accident — it is a clean separation of concerns that makes both components simpler and faster. The lexer can be a tight inner loop over raw bytes; the parser operates on a structured token stream.

The parser's job is to impose tree structure on the token stream. The naive approach — recursive descent — works perfectly for statement-level grammar where precedence is explicit in the grammar rules. But for expressions with multiple levels of operator precedence, recursive descent leads to a grammar with one function per precedence level. Pratt parsing (top-down operator precedence) solves this with a single dispatch table indexed by token type, where each token registers its own left-binding power and how to parse itself in prefix or infix position. The result is a parser where adding a new operator means adding one entry to a table, not restructuring the grammar.

PEG (Parsing Expression Grammar) grammars take a different approach: they are deterministic by construction. Where traditional context-free grammars can be ambiguous (requiring GLR parsing), PEG grammars use ordered choice (`/`) that always picks the first matching alternative. This eliminates ambiguity but means error messages become harder to generate — a failed PEG match backtracks silently. Understanding these tradeoffs matters when choosing a parsing strategy: hand-rolled Pratt for production compilers (where error quality is paramount), PEG for configuration languages or protocols (where the grammar is simple and correctness is primary).

Production compilers — Go, rustc, Clang — use hand-rolled recursive descent parsers, not parser generators. The reasons are instructive: error recovery (inserting/skipping tokens to continue parsing after an error requires fine-grained control), IDE integration (incremental re-parsing of changed regions), and diagnostics (pointing to the right source span and suggesting fixes). Parser generators optimize for grammar expressiveness; production parsers optimize for user experience.

## Core Concepts

### Lexer as a State Machine

A lexer maintains a position in the input and a current state. The state determines how to interpret the next byte. For most languages, the state is simple: `Default`, `InString`, `InLineComment`, `InBlockComment`. The output is a stream of tokens, each carrying its type, its text (or a pointer into the source buffer), and its byte span for diagnostics.

The critical property is that lexing is linear — O(n) in the input size with no backtracking. This matters because the lexer runs on every keystroke in an IDE. Token types are a finite set; in a hand-rolled lexer they are typically an integer enum.

### Pratt Parsing (Top-Down Operator Precedence)

Pratt's key insight: instead of encoding precedence in the grammar, associate a **binding power** with each token. A higher binding power means the token "binds tighter." The parser's main function, `parse_expression(min_bp)`, consumes tokens as long as the next token's binding power exceeds `min_bp`.

For a token like `+` with binding power 10:
- Left operand: parsed by whatever called `parse_expression`
- Right operand: parsed by `parse_expression(10)` — which stops when it sees a token with bp ≤ 10
- This gives left-associativity. For right-associativity (like `^`), use `parse_expression(bp - 1)`

The elegance: unary prefix operators get a null denotation (`nud`) — what to do when the token appears with no left operand. Binary operators get a left denotation (`led`) — what to do when the token appears with a left operand already parsed.

### PEG Grammars

A PEG grammar is a set of rules where each rule is a parsing expression:
- `e1 e2` — sequence: match e1 then e2
- `e1 / e2` — ordered choice: try e1, if it fails try e2
- `e*`, `e+`, `e?` — repetition
- `!e` — negative lookahead: succeed if e fails, consuming nothing

The ordered choice is what makes PEGs deterministic: `a / ab` always matches `a` even when the input is `ab`. This is the opposite of CFG `|` which is symmetric. PEGs can be compiled to packrat parsers (with memoization) that run in O(n) time, trading memory for speed.

## Implementation: Go

```go
package main

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Token types for a mini expression language.
// Expressions: integers, +, -, *, /, unary minus, parentheses.
type TokenKind int

const (
	TkEOF TokenKind = iota
	TkInt
	TkPlus
	TkMinus
	TkStar
	TkSlash
	TkCaret // right-associative exponentiation
	TkLParen
	TkRParen
)

func (k TokenKind) String() string {
	return [...]string{"EOF", "INT", "+", "-", "*", "/", "^", "(", ")"}[k]
}

type Token struct {
	Kind TokenKind
	Text string
	Pos  int // byte offset in source
}

// --- Lexer ---

type Lexer struct {
	src []byte
	pos int
}

func NewLexer(src string) *Lexer {
	return &Lexer{src: []byte(src)}
}

func (l *Lexer) skipWhitespace() {
	for l.pos < len(l.src) && unicode.IsSpace(rune(l.src[l.pos])) {
		l.pos++
	}
}

func (l *Lexer) Next() Token {
	l.skipWhitespace()
	if l.pos >= len(l.src) {
		return Token{Kind: TkEOF, Pos: l.pos}
	}

	start := l.pos
	ch := l.src[l.pos]

	// Single-character tokens: fast path via lookup table.
	single := map[byte]TokenKind{
		'+': TkPlus, '-': TkMinus, '*': TkStar,
		'/': TkSlash, '^': TkCaret, '(': TkLParen, ')': TkRParen,
	}
	if kind, ok := single[ch]; ok {
		l.pos++
		return Token{Kind: kind, Text: string(ch), Pos: start}
	}

	// Integer literal: consume digits.
	if ch >= '0' && ch <= '9' {
		for l.pos < len(l.src) && l.src[l.pos] >= '0' && l.src[l.pos] <= '9' {
			l.pos++
		}
		return Token{Kind: TkInt, Text: string(l.src[start:l.pos]), Pos: start}
	}

	panic(fmt.Sprintf("unexpected character %q at position %d", ch, l.pos))
}

func (l *Lexer) Peek() Token {
	saved := l.pos
	tok := l.Next()
	l.pos = saved
	return tok
}

// --- Pratt Parser ---
// Each token type has a binding power and optional parse functions.
// nud: "null denotation" — how to parse this token with no left operand (prefix position)
// led: "left denotation" — how to parse this token with a left operand (infix position)

type Node interface {
	String() string
	Eval() int
}

type IntNode struct{ Value int }

func (n *IntNode) String() string { return strconv.Itoa(n.Value) }
func (n *IntNode) Eval() int      { return n.Value }

type BinopNode struct {
	Op    string
	Left  Node
	Right Node
}

func (n *BinopNode) String() string {
	return fmt.Sprintf("(%s %s %s)", n.Op, n.Left, n.Right)
}
func (n *BinopNode) Eval() int {
	l, r := n.Left.Eval(), n.Right.Eval()
	switch n.Op {
	case "+":
		return l + r
	case "-":
		return l - r
	case "*":
		return l * r
	case "/":
		if r == 0 {
			panic("division by zero")
		}
		return l / r
	case "^":
		result := 1
		for i := 0; i < r; i++ {
			result *= l
		}
		return result
	}
	panic("unknown op " + n.Op)
}

type UnaryNode struct {
	Op      string
	Operand Node
}

func (n *UnaryNode) String() string { return fmt.Sprintf("(%s %s)", n.Op, n.Operand) }
func (n *UnaryNode) Eval() int {
	v := n.Operand.Eval()
	if n.Op == "-" {
		return -v
	}
	return v
}

// bindingPower returns left and right binding powers for infix operators.
// Left > right for left-associative; left < right for right-associative.
func bindingPower(kind TokenKind) (left, right int) {
	switch kind {
	case TkPlus, TkMinus:
		return 10, 11 // left-associative at precedence 10
	case TkStar, TkSlash:
		return 20, 21 // left-associative at precedence 20
	case TkCaret:
		return 30, 30 // right-associative: right bp == left bp means right side binds equally
		// To make right-associative, we do: right operand parsed with bp-1 equivalent.
		// Here we use (30, 29) convention: right side gets lower min_bp so it "grabs" more.
	}
	return 0, 0
}

type Parser struct {
	lexer   *Lexer
	current Token
}

func NewParser(src string) *Parser {
	p := &Parser{lexer: NewLexer(src)}
	p.current = p.lexer.Next()
	return p
}

func (p *Parser) consume(expected TokenKind) Token {
	tok := p.current
	if tok.Kind != expected {
		panic(fmt.Sprintf("expected %s, got %s at pos %d", expected, tok.Kind, tok.Pos))
	}
	p.current = p.lexer.Next()
	return tok
}

func (p *Parser) advance() Token {
	tok := p.current
	p.current = p.lexer.Next()
	return tok
}

// parseExpression is the heart of the Pratt parser.
// minBP: the minimum binding power the next infix operator must have to be consumed.
func (p *Parser) parseExpression(minBP int) Node {
	// Parse the prefix (nud) part: integer literal, unary minus, or parenthesized expr.
	var left Node
	tok := p.advance()
	switch tok.Kind {
	case TkInt:
		v, _ := strconv.Atoi(tok.Text)
		left = &IntNode{Value: v}
	case TkMinus:
		// Unary minus: right operand binds very tightly (bp=100)
		operand := p.parseExpression(100)
		left = &UnaryNode{Op: "-", Operand: operand}
	case TkPlus:
		// Unary plus: no-op but valid
		operand := p.parseExpression(100)
		left = &UnaryNode{Op: "+", Operand: operand}
	case TkLParen:
		// Grouped expression: parse inside with fresh min_bp, consume closing paren.
		left = p.parseExpression(0)
		p.consume(TkRParen)
	default:
		panic(fmt.Sprintf("unexpected token %s in prefix position at pos %d", tok.Kind, tok.Pos))
	}

	// Parse infix (led) operators as long as their left binding power exceeds minBP.
	for {
		op := p.current
		leftBP, rightBP := bindingPower(op.Kind)
		if leftBP <= minBP {
			// This operator doesn't bind tightly enough: stop.
			break
		}
		p.advance() // consume the operator
		right := p.parseExpression(rightBP)
		left = &BinopNode{Op: op.Text, Left: left, Right: right}
	}

	return left
}

func Parse(src string) Node {
	p := NewParser(src)
	node := p.parseExpression(0)
	if p.current.Kind != TkEOF {
		panic(fmt.Sprintf("unexpected token %s after expression", p.current.Kind))
	}
	return node
}

func main() {
	examples := []string{
		"1 + 2 * (3 - 4)",    // = 1 + 2 * (-1) = -1
		"2 ^ 3 ^ 2",          // right-assoc: 2^(3^2) = 2^9 = 512, not (2^3)^2 = 64
		"-5 + 3",              // unary: (-5) + 3 = -2
		"(1 + 2) * (3 + 4)",  // = 3 * 7 = 21
		"10 / 2 / 5",         // left-assoc: (10/2)/5 = 1
	}

	for _, src := range examples {
		node := Parse(src)
		fmt.Printf("%-30s => AST: %-35s => %d\n", src, node.String(), node.Eval())
	}
}
```

### Go-specific considerations

**Escape analysis and the Lexer**: The `Lexer` struct is allocated with `NewLexer` — Go's escape analysis will determine whether it lives on the heap or stack. In hot parsing loops, heap allocation matters. You can verify with `go build -gcflags='-m -m' ./...` — look for `moved to heap` in the output.

**Interface dispatch for AST nodes**: `Node` is an interface; every `IntNode`, `BinopNode`, `UnaryNode` satisfies it. Each call to `node.Eval()` or `node.String()` goes through the interface dispatch table (itab lookup + indirect call). For deep AST trees this adds up. Production compilers often use tagged unions (discriminated structs) to eliminate dispatch overhead — see the `syntax` package in `go/src/cmd/compile`.

**The Go compiler's own parser**: Located at `src/cmd/compile/internal/syntax/parser.go`. It is a hand-rolled recursive descent parser with explicit error recovery — when parsing fails, it inserts synthetic tokens and continues to report multiple errors per compilation. Study `p.errorAt` and the `p.want` recovery helper.

**`go/parser` vs `cmd/compile/internal/syntax`**: The `go/parser` package in the standard library produces a CST-like AST (keeping comments, positions). The compiler's internal parser is optimized for speed and produces a leaner AST. They are not the same.

## Implementation: Rust

```rust
// Hand-rolled Pratt parser in Rust.
// Demonstrates: enum-based tokens, iterator-based lexer, recursive descent with binding powers.

#[derive(Debug, Clone, PartialEq)]
enum TokenKind {
    Int(i64),
    Plus,
    Minus,
    Star,
    Slash,
    Caret,
    LParen,
    RParen,
    Eof,
}

#[derive(Debug, Clone)]
struct Token {
    kind: TokenKind,
    pos: usize,
}

// --- Lexer ---

struct Lexer<'a> {
    src: &'a [u8],
    pos: usize,
}

impl<'a> Lexer<'a> {
    fn new(src: &'a str) -> Self {
        Lexer { src: src.as_bytes(), pos: 0 }
    }

    fn skip_whitespace(&mut self) {
        while self.pos < self.src.len() && self.src[self.pos].is_ascii_whitespace() {
            self.pos += 1;
        }
    }

    fn next_token(&mut self) -> Token {
        self.skip_whitespace();
        if self.pos >= self.src.len() {
            return Token { kind: TokenKind::Eof, pos: self.pos };
        }

        let start = self.pos;
        let ch = self.src[self.pos];

        // Single-character tokens
        let kind = match ch {
            b'+' => { self.pos += 1; TokenKind::Plus }
            b'-' => { self.pos += 1; TokenKind::Minus }
            b'*' => { self.pos += 1; TokenKind::Star }
            b'/' => { self.pos += 1; TokenKind::Slash }
            b'^' => { self.pos += 1; TokenKind::Caret }
            b'(' => { self.pos += 1; TokenKind::LParen }
            b')' => { self.pos += 1; TokenKind::RParen }
            b'0'..=b'9' => {
                while self.pos < self.src.len() && self.src[self.pos].is_ascii_digit() {
                    self.pos += 1;
                }
                let text = std::str::from_utf8(&self.src[start..self.pos]).unwrap();
                TokenKind::Int(text.parse().unwrap())
            }
            _ => panic!("unexpected character '{}' at position {}", ch as char, self.pos),
        };

        Token { kind, pos: start }
    }
}

// Collect all tokens upfront — simpler for a Pratt parser.
fn tokenize(src: &str) -> Vec<Token> {
    let mut lexer = Lexer::new(src);
    let mut tokens = Vec::new();
    loop {
        let tok = lexer.next_token();
        let is_eof = tok.kind == TokenKind::Eof;
        tokens.push(tok);
        if is_eof { break; }
    }
    tokens
}

// --- AST ---

#[derive(Debug)]
enum Expr {
    Int(i64),
    Unary { op: char, operand: Box<Expr> },
    Binary { op: char, left: Box<Expr>, right: Box<Expr> },
}

impl Expr {
    fn eval(&self) -> i64 {
        match self {
            Expr::Int(n) => *n,
            Expr::Unary { op, operand } => match op {
                '-' => -operand.eval(),
                '+' => operand.eval(),
                _ => panic!("unknown unary op"),
            },
            Expr::Binary { op, left, right } => {
                let (l, r) = (left.eval(), right.eval());
                match op {
                    '+' => l + r,
                    '-' => l - r,
                    '*' => l * r,
                    '/' => l / r,
                    '^' => l.pow(r as u32),
                    _ => panic!("unknown binary op"),
                }
            }
        }
    }

    fn display(&self) -> String {
        match self {
            Expr::Int(n) => n.to_string(),
            Expr::Unary { op, operand } => format!("({} {})", op, operand.display()),
            Expr::Binary { op, left, right } => {
                format!("({} {} {})", op, left.display(), right.display())
            }
        }
    }
}

// --- Pratt Parser ---

struct Parser {
    tokens: Vec<Token>,
    pos: usize,
}

impl Parser {
    fn new(tokens: Vec<Token>) -> Self {
        Parser { tokens, pos: 0 }
    }

    fn peek(&self) -> &Token {
        &self.tokens[self.pos]
    }

    fn advance(&mut self) -> &Token {
        let tok = &self.tokens[self.pos];
        if self.pos < self.tokens.len() - 1 {
            self.pos += 1;
        }
        tok
    }

    // Returns (left_bp, right_bp) for infix operators.
    // left_bp is checked against the caller's min_bp.
    // right_bp is passed to the recursive call (right operand).
    // Right-associativity: left_bp > right_bp (right side "gets" more).
    fn infix_bp(kind: &TokenKind) -> Option<(u8, u8)> {
        match kind {
            TokenKind::Plus | TokenKind::Minus => Some((10, 11)),
            TokenKind::Star | TokenKind::Slash => Some((20, 21)),
            // ^ is right-associative: left_bp(30) > right_bp(29)
            // So `2^3^2` parses as `2^(3^2)`: after parsing `3`, we check
            // whether `^` (left_bp=30) > current min_bp(29). It is, so we continue.
            TokenKind::Caret => Some((30, 29)),
            _ => None,
        }
    }

    fn parse_expression(&mut self, min_bp: u8) -> Expr {
        // Prefix (nud): parse the initial atom.
        let tok = self.advance().clone();
        let mut left = match &tok.kind {
            TokenKind::Int(n) => Expr::Int(*n),
            TokenKind::Minus => {
                let operand = self.parse_expression(100);
                Expr::Unary { op: '-', operand: Box::new(operand) }
            }
            TokenKind::Plus => {
                let operand = self.parse_expression(100);
                Expr::Unary { op: '+', operand: Box::new(operand) }
            }
            TokenKind::LParen => {
                let inner = self.parse_expression(0);
                // Consume ')'
                assert_eq!(self.advance().kind, TokenKind::RParen, "expected ')'");
                inner
            }
            other => panic!("unexpected token {:?} in prefix position", other),
        };

        // Infix (led): consume operators while their left_bp > min_bp.
        loop {
            let (left_bp, right_bp) = match Self::infix_bp(&self.peek().kind) {
                Some(bp) => bp,
                None => break,
            };

            if left_bp <= min_bp {
                break;
            }

            let op_char = match self.advance().kind {
                TokenKind::Plus => '+',
                TokenKind::Minus => '-',
                TokenKind::Star => '*',
                TokenKind::Slash => '/',
                TokenKind::Caret => '^',
                _ => panic!("infix_bp returned Some but advance gave non-operator"),
            };

            let right = self.parse_expression(right_bp);
            left = Expr::Binary {
                op: op_char,
                left: Box::new(left),
                right: Box::new(right),
            };
        }

        left
    }
}

fn parse(src: &str) -> Expr {
    let tokens = tokenize(src);
    let mut parser = Parser::new(tokens);
    parser.parse_expression(0)
}

fn main() {
    let examples = [
        ("1 + 2 * (3 - 4)", -1_i64),
        ("2 ^ 3 ^ 2", 512),  // right-assoc: 2^9
        ("-5 + 3", -2),
        ("(1 + 2) * (3 + 4)", 21),
        ("10 / 2 / 5", 1),
    ];

    for (src, expected) in &examples {
        let expr = parse(src);
        let result = expr.eval();
        let ast_str = expr.display();
        println!("{:<30} => AST: {:<35} => {}", src, ast_str, result);
        assert_eq!(result, *expected, "wrong result for '{}'", src);
    }
    println!("All assertions passed.");
}
```

### Rust-specific considerations

**`Box<Expr>` and recursive enums**: Rust enums must have a known size at compile time. A recursive enum (`Expr` containing `Expr`) would be infinite size. `Box<Expr>` gives us a pointer — fixed size (8 bytes on 64-bit). This is the Rust idiom for tree nodes. The box allocation happens per node, which is fine for an AST but would be a bottleneck for a hot-path scanner. Arena allocation (via `bumpalo`) eliminates this overhead.

**`clone()` on tokens**: The `advance()` function borrows `self` but we need to return a `Token` while still mutating `self.pos`. The simplest fix is `clone()` on the token. In a production compiler you'd either use token indices (avoids cloning) or split the token stream into a separate data structure.

**Comparing with `nom`**: `nom` is a combinator library for parsing. Instead of writing `parse_expression`, you'd compose parsers: `alt((parse_int, parse_paren, parse_unary))`. `nom` shines for binary formats (network protocols, file headers) where the grammar is regular. For expression parsing with custom operator precedence, Pratt is cleaner because `nom` does not have a built-in Pratt combinator — you'd layer it manually anyway.

**`rustc`'s own parser**: In `compiler/rustc_parse/src/parser/expr.rs`. Look for `parse_expr_prec` — rustc uses a precedence-based approach very similar to Pratt. The key difference is that rustc's parser must handle `<` and `>` as both comparison operators and generic delimiters — a known pain point requiring backtracking in specific contexts.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|----|----|
| Token representation | Struct with int kind | Enum variant with data (e.g., `Int(i64)`) |
| AST node type | Interface (`Node`) | Enum (`Expr`) with `match` |
| Pattern matching | Type switch (`switch v := n.(type)`) | Exhaustive `match` — compiler enforces all cases |
| Memory model | GC heap allocation per node | `Box<T>` per node, or arena |
| Error handling | `panic` (adequate for demos) | `Result<T, ParseError>` in production |
| Recursive types | Pointer fields implicit | `Box<Expr>` required — size must be known |
| Parser generator alternative | `goyacc`, `pigeon` (PEG) | `pest`, `nom`, `lalrpop` |

## Production War Stories

**Go's `cmd/compile` parser rewrite (Go 1.6 → 1.7)**: The compiler parser was rewritten from using `go/parser` (which allocates an AST with full source positions for every node) to a custom internal parser that is significantly faster and allocates less. The old parser was a bottleneck during `go build` on large packages. The key lesson: the public-facing `go/parser` API and the compiler's internal parser have different performance requirements, and conflating them costs you.

**tree-sitter's incremental parsing**: tree-sitter (used by Neovim, Helix, GitHub) uses an error-tolerant parser that can re-parse only the changed region of a file. It is based on GLR (Generalized LR) parsing, not Pratt or recursive descent. When you type a character, tree-sitter replaces the affected subtree without re-parsing the entire file. This is why `go/parser` is not suitable for IDE use at scale — it re-parses entire files.

**ANTLR and the "grammar works, performance does not" trap**: Many teams start with ANTLR for DSLs. The grammar is clean, the parser generates in seconds. Then the grammar hits production data with pathological inputs, and ANTLR's ALL(*) parsing strategy starts exhibiting O(n²) or worse behavior on certain ambiguous grammar rules. The fix is always the same: profile, find the ambiguous rule, restrict it. A hand-rolled parser would have made the ambiguity obvious in the implementation.

**rustc's error recovery**: rustc's parser has extensive error recovery logic. When it encounters unexpected tokens, it tries to give useful suggestions ("did you mean...?"). This code is some of the most complex in the parser — handling the delta between what was seen and what was expected is essentially a small edit-distance computation over grammar rules. The lesson: if you need good error messages, budget 30–50% of your parser development time for error recovery alone.

## Complexity Analysis

| Operation | Time | Space |
|-----------|------|-------|
| Lexing | O(n) in source length | O(1) state, O(k) for token buffer |
| Pratt parsing | O(n) in token count | O(d) stack where d = nesting depth |
| PEG without memoization | O(2^n) worst case | O(1) |
| PEG packrat (with memoization) | O(n) | O(n × rules) |
| ANTLR ALL(*) | O(n) typical, O(n²) pathological | O(n) |

## Common Pitfalls

**1. Conflating lexing and parsing.** Embedding grammar decisions in the lexer ("this `<` is a generic delimiter, not a comparison") leads to context-sensitive lexers that are hard to reason about. The clean separation: the lexer returns `<` unconditionally; the parser uses context to decide its role.

**2. Using parser generators for error recovery.** ANTLR and yacc/bison have error recovery mechanisms, but they are coarse (`error` rules that consume tokens until a synchronization point). When your users expect rustc-quality diagnostics, you need full control over what happens after a parse failure.

**3. Off-by-one in binding powers.** In Pratt parsing, the difference between left-associative and right-associative is a single integer. `(10, 11)` is left-associative; `(10, 10)` is right-associative. Getting this wrong silently misparses `a - b - c` as `a - (b - c)`.

**4. Not preserving source spans.** Early in a compiler project it is tempting to discard position information to simplify the AST. Once you need error messages, you retrofit spans everywhere — this is a painful refactor. Store `(start, end)` byte offsets from the start.

**5. PEG grammars and "greedy" `*`/`+`**: In a PEG, `e+` is greedy — it consumes as many matches as possible. A grammar like `a+ a` will never match because the first `a+` consumes all `a`s, leaving nothing for the second `a`. This is a common source of surprise when translating from CFG grammars.

## Exercises

**Exercise 1** (30 min): Add support for floating-point literals (`1.5`, `.3`) to the Go lexer. Handle the edge case where `.` appears without a leading digit. Run `go build` to verify it compiles.

**Exercise 2** (2–4h): Extend the Pratt parser to support comparison operators (`<`, `>`, `==`, `!=`) and logical operators (`&&`, `||`) with correct relative precedence (comparisons bind tighter than logical, but looser than arithmetic). Add an `Env` map that allows identifiers (variable names) to be looked up. Implement `let x = 5; x + 3` as a simple statement language on top.

**Exercise 3** (4–8h): Implement a PEG parser for the same expression language without a library. Use a recursive struct of function pointers (Go) or trait objects (Rust). Add memoization (packrat) and measure the speedup on deeply nested inputs like `((((((1+2))))))`. Compare memory usage vs the Pratt parser.

**Exercise 4** (8–15h): Implement error recovery in the Pratt parser. After an unexpected token, attempt to synchronize by skipping to the next `)` or end of input, reporting the error but continuing to parse the rest. Test with malformed inputs like `1 + * 2` and `(1 + 2`. The goal: report all parse errors in a single pass, not just the first one. Compare your approach to how `go/parser` does it (read `src/go/parser/parser.go`).

## Further Reading

### Foundational Papers
- **Pratt, 1973** — "Top Down Operator Precedence" (ACM SIGPLAN symposium on Principles of programming languages). The original paper. Surprisingly readable.
- **Ford, 2004** — "Parsing Expression Grammars: A Recognition-Based Syntactic Foundation." Establishes the theoretical basis for PEGs and packrat parsing.
- **Earley, 1970** — "An Efficient Context-Free Parsing Algorithm." The algorithm behind modern generalized parsers (ANTLR's LL(*) is a restricted form).

### Books
- **Crafting Interpreters** — Robert Nystrom. Free online. Chapters 4–6 cover scanning, parsing, and evaluation. The best practical introduction.
- **Engineering a Compiler** (Cooper & Torczon, 3rd ed.) — Chapters 2–3. Formal treatment of lexing (regular expressions → DFA) and parsing (LL, LR grammars).
- **Dragon Book** (Compilers: Principles, Techniques, and Tools, Aho et al.) — Chapters 3–4. The reference. Dense but complete.

### Production Code to Read
- `go/src/cmd/compile/internal/syntax/parser.go` — Go compiler's hand-rolled recursive descent parser
- `compiler/rustc_parse/src/parser/expr.rs` — rustc's expression parser
- `https://github.com/nickel-lang/nickel/tree/master/core/src/parser` — Nickel uses LALRPOP (LR parser generator for Rust); contrast with hand-rolled approach
- `https://github.com/tree-sitter/tree-sitter` — The incremental GLR parser used by code editors

### Talks
- **"Pratt Parsers: Expression Parsing Made Easy"** — Bob Nystrom's blog post (journal.stuffwithstuff.com). The best practical explanation of Pratt parsing.
- **"How GCC and LLVM generate x86 code"** — CppCon 2018. Starts at the IR level but the parsing context is useful.

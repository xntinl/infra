# 23. Recursive Descent Parser + AST Builder -- Solution

## Architecture Overview

Both solutions implement four layers:

1. **Lexer** -- tokenizes source with position tracking
2. **Parser** -- recursive descent with panic-mode error recovery, produces AST
3. **AST** -- typed nodes with `Span` for source locations
4. **Visitors** -- trait/interface with per-node methods; pretty-printer and node counter

### Language Grammar (EBNF)

```
program     = declaration* EOF
declaration = fun_decl | var_decl | statement
fun_decl    = "fn" IDENT "(" params? ")" block
var_decl    = "let" IDENT ("=" expression)? ";"
statement   = expr_stmt | print_stmt | if_stmt | while_stmt | return_stmt | block
expr_stmt   = expression ";"
print_stmt  = "print" expression ";"
if_stmt     = "if" expression block ("else" (if_stmt | block))?
while_stmt  = "while" expression block
return_stmt = "return" expression? ";"
block       = "{" declaration* "}"
expression  = assignment
assignment  = IDENT "=" assignment | logic_or
logic_or    = logic_and ("or" logic_and)*
logic_and   = equality ("and" equality)*
equality    = comparison (("==" | "!=") comparison)*
comparison  = term (("<" | ">" | "<=" | ">=") term)*
term        = factor (("+" | "-") factor)*
factor      = unary (("*" | "/") unary)*
unary       = ("not" | "-") unary | call
call        = primary ("(" arguments? ")")*
primary     = NUMBER | STRING | "true" | "false" | IDENT | "(" expression ")"
params      = IDENT ("," IDENT)*
arguments   = expression ("," expression)*
```

## Complete Solution (Go)

### ast.go

```go
package lang

type Span struct {
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
}

type Node interface {
	GetSpan() Span
	Accept(v Visitor)
}

type Program struct {
	Span  Span
	Decls []Node
}

func (p *Program) GetSpan() Span    { return p.Span }
func (p *Program) Accept(v Visitor) { v.VisitProgram(p) }

type VarDecl struct {
	Span  Span
	Name  string
	Value Expr
}

func (d *VarDecl) GetSpan() Span    { return d.Span }
func (d *VarDecl) Accept(v Visitor) { v.VisitVarDecl(d) }

type FunDecl struct {
	Span   Span
	Name   string
	Params []string
	Body   *Block
}

func (d *FunDecl) GetSpan() Span    { return d.Span }
func (d *FunDecl) Accept(v Visitor) { v.VisitFunDecl(d) }

type Block struct {
	Span  Span
	Stmts []Node
}

func (b *Block) GetSpan() Span    { return b.Span }
func (b *Block) Accept(v Visitor) { v.VisitBlock(b) }

type PrintStmt struct {
	Span Span
	Expr Expr
}

func (s *PrintStmt) GetSpan() Span    { return s.Span }
func (s *PrintStmt) Accept(v Visitor) { v.VisitPrintStmt(s) }

type IfStmt struct {
	Span      Span
	Condition Expr
	Then      *Block
	Else      Node // *Block or *IfStmt or nil
}

func (s *IfStmt) GetSpan() Span    { return s.Span }
func (s *IfStmt) Accept(v Visitor) { v.VisitIfStmt(s) }

type WhileStmt struct {
	Span      Span
	Condition Expr
	Body      *Block
}

func (s *WhileStmt) GetSpan() Span    { return s.Span }
func (s *WhileStmt) Accept(v Visitor) { v.VisitWhileStmt(s) }

type ReturnStmt struct {
	Span  Span
	Value Expr
}

func (s *ReturnStmt) GetSpan() Span    { return s.Span }
func (s *ReturnStmt) Accept(v Visitor) { v.VisitReturnStmt(s) }

type ExprStmt struct {
	Span Span
	Expr Expr
}

func (s *ExprStmt) GetSpan() Span    { return s.Span }
func (s *ExprStmt) Accept(v Visitor) { v.VisitExprStmt(s) }

// Expressions

type Expr interface {
	Node
	exprNode()
}

type NumberLit struct {
	Span  Span
	Value float64
}

func (e *NumberLit) GetSpan() Span    { return e.Span }
func (e *NumberLit) Accept(v Visitor) { v.VisitNumberLit(e) }
func (e *NumberLit) exprNode()        {}

type StringLit struct {
	Span  Span
	Value string
}

func (e *StringLit) GetSpan() Span    { return e.Span }
func (e *StringLit) Accept(v Visitor) { v.VisitStringLit(e) }
func (e *StringLit) exprNode()        {}

type BoolLit struct {
	Span  Span
	Value bool
}

func (e *BoolLit) GetSpan() Span    { return e.Span }
func (e *BoolLit) Accept(v Visitor) { v.VisitBoolLit(e) }
func (e *BoolLit) exprNode()        {}

type Identifier struct {
	Span Span
	Name string
}

func (e *Identifier) GetSpan() Span    { return e.Span }
func (e *Identifier) Accept(v Visitor) { v.VisitIdentifier(e) }
func (e *Identifier) exprNode()        {}

type BinaryExpr struct {
	Span  Span
	Left  Expr
	Op    string
	Right Expr
}

func (e *BinaryExpr) GetSpan() Span    { return e.Span }
func (e *BinaryExpr) Accept(v Visitor) { v.VisitBinaryExpr(e) }
func (e *BinaryExpr) exprNode()        {}

type UnaryExpr struct {
	Span Span
	Op   string
	Expr Expr
}

func (e *UnaryExpr) GetSpan() Span    { return e.Span }
func (e *UnaryExpr) Accept(v Visitor) { v.VisitUnaryExpr(e) }
func (e *UnaryExpr) exprNode()        {}

type AssignExpr struct {
	Span  Span
	Name  string
	Value Expr
}

func (e *AssignExpr) GetSpan() Span    { return e.Span }
func (e *AssignExpr) Accept(v Visitor) { v.VisitAssignExpr(e) }
func (e *AssignExpr) exprNode()        {}

type CallExpr struct {
	Span   Span
	Callee Expr
	Args   []Expr
}

func (e *CallExpr) GetSpan() Span    { return e.Span }
func (e *CallExpr) Accept(v Visitor) { v.VisitCallExpr(e) }
func (e *CallExpr) exprNode()        {}
```

### visitor.go

```go
package lang

import (
	"fmt"
	"strings"
)

type Visitor interface {
	VisitProgram(n *Program)
	VisitVarDecl(n *VarDecl)
	VisitFunDecl(n *FunDecl)
	VisitBlock(n *Block)
	VisitPrintStmt(n *PrintStmt)
	VisitIfStmt(n *IfStmt)
	VisitWhileStmt(n *WhileStmt)
	VisitReturnStmt(n *ReturnStmt)
	VisitExprStmt(n *ExprStmt)
	VisitNumberLit(n *NumberLit)
	VisitStringLit(n *StringLit)
	VisitBoolLit(n *BoolLit)
	VisitIdentifier(n *Identifier)
	VisitBinaryExpr(n *BinaryExpr)
	VisitUnaryExpr(n *UnaryExpr)
	VisitAssignExpr(n *AssignExpr)
	VisitCallExpr(n *CallExpr)
}

// PrettyPrinter visitor

type PrettyPrinter struct {
	buf    strings.Builder
	indent int
}

func NewPrettyPrinter() *PrettyPrinter {
	return &PrettyPrinter{}
}

func (p *PrettyPrinter) String() string {
	return p.buf.String()
}

func (p *PrettyPrinter) writeIndent() {
	for i := 0; i < p.indent; i++ {
		p.buf.WriteString("    ")
	}
}

func (p *PrettyPrinter) VisitProgram(n *Program) {
	for _, d := range n.Decls {
		d.Accept(p)
		p.buf.WriteString("\n")
	}
}

func (p *PrettyPrinter) VisitVarDecl(n *VarDecl) {
	p.writeIndent()
	p.buf.WriteString("let ")
	p.buf.WriteString(n.Name)
	if n.Value != nil {
		p.buf.WriteString(" = ")
		n.Value.Accept(p)
	}
	p.buf.WriteString(";")
}

func (p *PrettyPrinter) VisitFunDecl(n *FunDecl) {
	p.writeIndent()
	p.buf.WriteString("fn ")
	p.buf.WriteString(n.Name)
	p.buf.WriteString("(")
	p.buf.WriteString(strings.Join(n.Params, ", "))
	p.buf.WriteString(") ")
	n.Body.Accept(p)
}

func (p *PrettyPrinter) VisitBlock(n *Block) {
	p.buf.WriteString("{\n")
	p.indent++
	for _, s := range n.Stmts {
		s.Accept(p)
		p.buf.WriteString("\n")
	}
	p.indent--
	p.writeIndent()
	p.buf.WriteString("}")
}

func (p *PrettyPrinter) VisitPrintStmt(n *PrintStmt) {
	p.writeIndent()
	p.buf.WriteString("print ")
	n.Expr.Accept(p)
	p.buf.WriteString(";")
}

func (p *PrettyPrinter) VisitIfStmt(n *IfStmt) {
	p.writeIndent()
	p.buf.WriteString("if ")
	n.Condition.Accept(p)
	p.buf.WriteString(" ")
	n.Then.Accept(p)
	if n.Else != nil {
		p.buf.WriteString(" else ")
		switch el := n.Else.(type) {
		case *IfStmt:
			// reset indent since VisitIfStmt adds its own
			el.Accept(p)
		case *Block:
			el.Accept(p)
		}
	}
}

func (p *PrettyPrinter) VisitWhileStmt(n *WhileStmt) {
	p.writeIndent()
	p.buf.WriteString("while ")
	n.Condition.Accept(p)
	p.buf.WriteString(" ")
	n.Body.Accept(p)
}

func (p *PrettyPrinter) VisitReturnStmt(n *ReturnStmt) {
	p.writeIndent()
	p.buf.WriteString("return")
	if n.Value != nil {
		p.buf.WriteString(" ")
		n.Value.Accept(p)
	}
	p.buf.WriteString(";")
}

func (p *PrettyPrinter) VisitExprStmt(n *ExprStmt) {
	p.writeIndent()
	n.Expr.Accept(p)
	p.buf.WriteString(";")
}

func (p *PrettyPrinter) VisitNumberLit(n *NumberLit) {
	if n.Value == float64(int64(n.Value)) {
		p.buf.WriteString(fmt.Sprintf("%d", int64(n.Value)))
	} else {
		p.buf.WriteString(fmt.Sprintf("%g", n.Value))
	}
}

func (p *PrettyPrinter) VisitStringLit(n *StringLit) {
	p.buf.WriteString(fmt.Sprintf("%q", n.Value))
}

func (p *PrettyPrinter) VisitBoolLit(n *BoolLit) {
	if n.Value {
		p.buf.WriteString("true")
	} else {
		p.buf.WriteString("false")
	}
}

func (p *PrettyPrinter) VisitIdentifier(n *Identifier) {
	p.buf.WriteString(n.Name)
}

func (p *PrettyPrinter) VisitBinaryExpr(n *BinaryExpr) {
	p.buf.WriteString("(")
	n.Left.Accept(p)
	p.buf.WriteString(" ")
	p.buf.WriteString(n.Op)
	p.buf.WriteString(" ")
	n.Right.Accept(p)
	p.buf.WriteString(")")
}

func (p *PrettyPrinter) VisitUnaryExpr(n *UnaryExpr) {
	p.buf.WriteString(n.Op)
	n.Expr.Accept(p)
}

func (p *PrettyPrinter) VisitAssignExpr(n *AssignExpr) {
	p.buf.WriteString(n.Name)
	p.buf.WriteString(" = ")
	n.Value.Accept(p)
}

func (p *PrettyPrinter) VisitCallExpr(n *CallExpr) {
	n.Callee.Accept(p)
	p.buf.WriteString("(")
	for i, arg := range n.Args {
		if i > 0 {
			p.buf.WriteString(", ")
		}
		arg.Accept(p)
	}
	p.buf.WriteString(")")
}

// NodeCounter visitor

type NodeCounter struct {
	Counts map[string]int
}

func NewNodeCounter() *NodeCounter {
	return &NodeCounter{Counts: make(map[string]int)}
}

func (c *NodeCounter) Total() int {
	total := 0
	for _, v := range c.Counts {
		total += v
	}
	return total
}

func (c *NodeCounter) inc(name string)              { c.Counts[name]++ }
func (c *NodeCounter) VisitProgram(n *Program)      { c.inc("Program"); for _, d := range n.Decls { d.Accept(c) } }
func (c *NodeCounter) VisitVarDecl(n *VarDecl)      { c.inc("VarDecl"); if n.Value != nil { n.Value.Accept(c) } }
func (c *NodeCounter) VisitFunDecl(n *FunDecl)      { c.inc("FunDecl"); n.Body.Accept(c) }
func (c *NodeCounter) VisitBlock(n *Block)           { c.inc("Block"); for _, s := range n.Stmts { s.Accept(c) } }
func (c *NodeCounter) VisitPrintStmt(n *PrintStmt)   { c.inc("PrintStmt"); n.Expr.Accept(c) }
func (c *NodeCounter) VisitIfStmt(n *IfStmt)         { c.inc("IfStmt"); n.Condition.Accept(c); n.Then.Accept(c); if n.Else != nil { n.Else.Accept(c) } }
func (c *NodeCounter) VisitWhileStmt(n *WhileStmt)   { c.inc("WhileStmt"); n.Condition.Accept(c); n.Body.Accept(c) }
func (c *NodeCounter) VisitReturnStmt(n *ReturnStmt) { c.inc("ReturnStmt"); if n.Value != nil { n.Value.Accept(c) } }
func (c *NodeCounter) VisitExprStmt(n *ExprStmt)     { c.inc("ExprStmt"); n.Expr.Accept(c) }
func (c *NodeCounter) VisitNumberLit(n *NumberLit)   { c.inc("NumberLit") }
func (c *NodeCounter) VisitStringLit(n *StringLit)   { c.inc("StringLit") }
func (c *NodeCounter) VisitBoolLit(n *BoolLit)       { c.inc("BoolLit") }
func (c *NodeCounter) VisitIdentifier(n *Identifier) { c.inc("Identifier") }
func (c *NodeCounter) VisitBinaryExpr(n *BinaryExpr) { c.inc("BinaryExpr"); n.Left.Accept(c); n.Right.Accept(c) }
func (c *NodeCounter) VisitUnaryExpr(n *UnaryExpr)   { c.inc("UnaryExpr"); n.Expr.Accept(c) }
func (c *NodeCounter) VisitAssignExpr(n *AssignExpr) { c.inc("AssignExpr"); n.Value.Accept(c) }
func (c *NodeCounter) VisitCallExpr(n *CallExpr)     { c.inc("CallExpr"); n.Callee.Accept(c); for _, a := range n.Args { a.Accept(c) } }
```

### lexer.go

```go
package lang

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

type TokenKind int

const (
	TokEOF TokenKind = iota
	TokNumber
	TokString
	TokIdent
	TokPlus
	TokMinus
	TokStar
	TokSlash
	TokEq
	TokEqEq
	TokBangEq
	TokLt
	TokGt
	TokLtEq
	TokGtEq
	TokLParen
	TokRParen
	TokLBrace
	TokRBrace
	TokComma
	TokSemicolon
	// Keywords
	KwLet
	KwFn
	KwIf
	KwElse
	KwWhile
	KwReturn
	KwPrint
	KwTrue
	KwFalse
	KwAnd
	KwOr
	KwNot
)

type LexToken struct {
	Kind  TokenKind
	Value string
	Line  int
	Col   int
}

type Lexer struct {
	input []rune
	pos   int
	line  int
	col   int
}

func NewLexer(input string) *Lexer {
	return &Lexer{input: []rune(input), pos: 0, line: 1, col: 1}
}

func (l *Lexer) Tokenize() ([]LexToken, error) {
	var tokens []LexToken
	for {
		l.skipWhitespace()
		if l.pos >= len(l.input) {
			tokens = append(tokens, LexToken{Kind: TokEOF, Line: l.line, Col: l.col})
			return tokens, nil
		}
		tok, err := l.nextToken()
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, tok)
	}
}

func (l *Lexer) peek() rune {
	if l.pos >= len(l.input) {
		return 0
	}
	return l.input[l.pos]
}

func (l *Lexer) advance() rune {
	ch := l.input[l.pos]
	l.pos++
	if ch == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return ch
}

func (l *Lexer) skipWhitespace() {
	for l.pos < len(l.input) && unicode.IsSpace(l.input[l.pos]) {
		l.advance()
	}
}

var kwMap = map[string]TokenKind{
	"let": KwLet, "fn": KwFn, "if": KwIf, "else": KwElse,
	"while": KwWhile, "return": KwReturn, "print": KwPrint,
	"true": KwTrue, "false": KwFalse, "and": KwAnd, "or": KwOr, "not": KwNot,
}

func (l *Lexer) nextToken() (LexToken, error) {
	line, col := l.line, l.col
	ch := l.peek()

	switch {
	case ch == '+': l.advance(); return LexToken{TokPlus, "+", line, col}, nil
	case ch == '-': l.advance(); return LexToken{TokMinus, "-", line, col}, nil
	case ch == '*': l.advance(); return LexToken{TokStar, "*", line, col}, nil
	case ch == '/': l.advance(); return LexToken{TokSlash, "/", line, col}, nil
	case ch == '(': l.advance(); return LexToken{TokLParen, "(", line, col}, nil
	case ch == ')': l.advance(); return LexToken{TokRParen, ")", line, col}, nil
	case ch == '{': l.advance(); return LexToken{TokLBrace, "{", line, col}, nil
	case ch == '}': l.advance(); return LexToken{TokRBrace, "}", line, col}, nil
	case ch == ',': l.advance(); return LexToken{TokComma, ",", line, col}, nil
	case ch == ';': l.advance(); return LexToken{TokSemicolon, ";", line, col}, nil
	case ch == '=':
		l.advance()
		if l.peek() == '=' { l.advance(); return LexToken{TokEqEq, "==", line, col}, nil }
		return LexToken{TokEq, "=", line, col}, nil
	case ch == '!':
		l.advance()
		if l.peek() == '=' { l.advance(); return LexToken{TokBangEq, "!=", line, col}, nil }
		return LexToken{}, fmt.Errorf("unexpected '!' at %d:%d", line, col)
	case ch == '<':
		l.advance()
		if l.peek() == '=' { l.advance(); return LexToken{TokLtEq, "<=", line, col}, nil }
		return LexToken{TokLt, "<", line, col}, nil
	case ch == '>':
		l.advance()
		if l.peek() == '=' { l.advance(); return LexToken{TokGtEq, ">=", line, col}, nil }
		return LexToken{TokGt, ">", line, col}, nil
	case ch == '"':
		return l.lexString(line, col)
	case unicode.IsDigit(ch):
		return l.lexNumber(line, col)
	case unicode.IsLetter(ch) || ch == '_':
		return l.lexIdent(line, col)
	default:
		return LexToken{}, fmt.Errorf("unexpected character '%c' at %d:%d", ch, line, col)
	}
}

func (l *Lexer) lexString(line, col int) (LexToken, error) {
	l.advance()
	var sb strings.Builder
	for l.pos < len(l.input) && l.peek() != '"' {
		ch := l.advance()
		if ch == '\\' && l.pos < len(l.input) {
			next := l.advance()
			switch next {
			case 'n': sb.WriteRune('\n')
			case 't': sb.WriteRune('\t')
			case '"': sb.WriteRune('"')
			case '\\': sb.WriteRune('\\')
			default: sb.WriteRune('\\'); sb.WriteRune(next)
			}
		} else {
			sb.WriteRune(ch)
		}
	}
	if l.pos >= len(l.input) {
		return LexToken{}, fmt.Errorf("unterminated string at %d:%d", line, col)
	}
	l.advance()
	return LexToken{TokString, sb.String(), line, col}, nil
}

func (l *Lexer) lexNumber(line, col int) (LexToken, error) {
	start := l.pos
	for l.pos < len(l.input) && unicode.IsDigit(l.peek()) { l.advance() }
	if l.pos < len(l.input) && l.peek() == '.' {
		l.advance()
		for l.pos < len(l.input) && unicode.IsDigit(l.peek()) { l.advance() }
	}
	return LexToken{TokNumber, string(l.input[start:l.pos]), line, col}, nil
}

func (l *Lexer) lexIdent(line, col int) (LexToken, error) {
	start := l.pos
	for l.pos < len(l.input) && (unicode.IsLetter(l.peek()) || unicode.IsDigit(l.peek()) || l.peek() == '_') {
		l.advance()
	}
	word := string(l.input[start:l.pos])
	if kw, ok := kwMap[word]; ok {
		return LexToken{kw, word, line, col}, nil
	}
	return LexToken{TokIdent, word, line, col}, nil
}

func parseNumber(s string) float64 {
	n, _ := strconv.ParseFloat(s, 64)
	return n
}
```

### parser.go

```go
package lang

import "fmt"

type ParseError struct {
	Message string
	Line    int
	Col     int
}

func (e ParseError) Error() string {
	return fmt.Sprintf("%s at %d:%d", e.Message, e.Line, e.Col)
}

type Parser struct {
	tokens []LexToken
	pos    int
	errors []ParseError
}

func NewParser(tokens []LexToken) *Parser {
	return &Parser{tokens: tokens, pos: 0}
}

func ParseProgram(input string) (*Program, []ParseError) {
	lexer := NewLexer(input)
	tokens, err := lexer.Tokenize()
	if err != nil {
		return nil, []ParseError{{Message: err.Error(), Line: 0, Col: 0}}
	}
	parser := NewParser(tokens)
	return parser.Parse()
}

func (p *Parser) peek() LexToken { return p.tokens[p.pos] }

func (p *Parser) advance() LexToken {
	tok := p.tokens[p.pos]
	if p.pos < len(p.tokens)-1 {
		p.pos++
	}
	return tok
}

func (p *Parser) check(kind TokenKind) bool { return p.peek().Kind == kind }

func (p *Parser) match(kinds ...TokenKind) (LexToken, bool) {
	for _, k := range kinds {
		if p.peek().Kind == k {
			return p.advance(), true
		}
	}
	return LexToken{}, false
}

func (p *Parser) expect(kind TokenKind) (LexToken, error) {
	tok := p.peek()
	if tok.Kind == kind {
		return p.advance(), nil
	}
	return tok, fmt.Errorf("expected %v, found '%s'", kind, tok.Value)
}

func (p *Parser) addError(msg string) {
	tok := p.peek()
	p.errors = append(p.errors, ParseError{
		Message: msg, Line: tok.Line, Col: tok.Col,
	})
}

func (p *Parser) synchronize() {
	p.advance()
	for !p.check(TokEOF) {
		prev := p.tokens[p.pos-1]
		if prev.Kind == TokSemicolon || prev.Kind == TokRBrace {
			return
		}
		switch p.peek().Kind {
		case KwLet, KwFn, KwIf, KwWhile, KwReturn, KwPrint:
			return
		}
		p.advance()
	}
}

func (p *Parser) Parse() (*Program, []ParseError) {
	startTok := p.peek()
	var decls []Node
	for !p.check(TokEOF) {
		decl := p.parseDeclaration()
		if decl != nil {
			decls = append(decls, decl)
		}
	}
	span := Span{StartLine: startTok.Line, StartCol: startTok.Col, EndLine: p.peek().Line, EndCol: p.peek().Col}
	return &Program{Span: span, Decls: decls}, p.errors
}

func (p *Parser) parseDeclaration() Node {
	defer func() {
		if r := recover(); r != nil {
			p.addError(fmt.Sprintf("%v", r))
			p.synchronize()
		}
	}()

	if p.check(KwFn) {
		return p.parseFunDecl()
	}
	if p.check(KwLet) {
		return p.parseVarDecl()
	}
	return p.parseStatement()
}

func (p *Parser) parseFunDecl() *FunDecl {
	start := p.advance() // fn
	name, err := p.expect(TokIdent)
	if err != nil {
		panic(fmt.Sprintf("Expected function name, found '%s'", p.peek().Value))
	}
	if _, err := p.expect(TokLParen); err != nil {
		panic(fmt.Sprintf("Expected '(' after function name '%s'", name.Value))
	}
	var params []string
	if !p.check(TokRParen) {
		for {
			param, err := p.expect(TokIdent)
			if err != nil {
				panic("Expected parameter name")
			}
			params = append(params, param.Value)
			if _, ok := p.match(TokComma); !ok {
				break
			}
		}
	}
	if _, err := p.expect(TokRParen); err != nil {
		panic("Expected ')' after parameters")
	}
	body := p.parseBlock()
	span := Span{StartLine: start.Line, StartCol: start.Col, EndLine: body.Span.EndLine, EndCol: body.Span.EndCol}
	return &FunDecl{Span: span, Name: name.Value, Params: params, Body: body}
}

func (p *Parser) parseVarDecl() *VarDecl {
	start := p.advance() // let
	name, err := p.expect(TokIdent)
	if err != nil {
		panic(fmt.Sprintf("Expected variable name after 'let'"))
	}
	var value Expr
	if _, ok := p.match(TokEq); ok {
		value = p.parseExpression()
	}
	end, err := p.expect(TokSemicolon)
	if err != nil {
		panic("Expected ';' after variable declaration")
	}
	span := Span{StartLine: start.Line, StartCol: start.Col, EndLine: end.Line, EndCol: end.Col}
	return &VarDecl{Span: span, Name: name.Value, Value: value}
}

func (p *Parser) parseStatement() Node {
	if p.check(KwPrint) {
		return p.parsePrintStmt()
	}
	if p.check(KwIf) {
		return p.parseIfStmt()
	}
	if p.check(KwWhile) {
		return p.parseWhileStmt()
	}
	if p.check(KwReturn) {
		return p.parseReturnStmt()
	}
	if p.check(TokLBrace) {
		return p.parseBlock()
	}
	return p.parseExprStmt()
}

func (p *Parser) parsePrintStmt() *PrintStmt {
	start := p.advance() // print
	expr := p.parseExpression()
	end, err := p.expect(TokSemicolon)
	if err != nil {
		panic("Expected ';' after print statement")
	}
	span := Span{StartLine: start.Line, StartCol: start.Col, EndLine: end.Line, EndCol: end.Col}
	return &PrintStmt{Span: span, Expr: expr}
}

func (p *Parser) parseIfStmt() *IfStmt {
	start := p.advance() // if
	condition := p.parseExpression()
	then := p.parseBlock()
	var elseNode Node
	if _, ok := p.match(KwElse); ok {
		if p.check(KwIf) {
			elseNode = p.parseIfStmt()
		} else {
			elseNode = p.parseBlock()
		}
	}
	endLine, endCol := then.Span.EndLine, then.Span.EndCol
	if elseNode != nil {
		endLine = elseNode.GetSpan().EndLine
		endCol = elseNode.GetSpan().EndCol
	}
	span := Span{StartLine: start.Line, StartCol: start.Col, EndLine: endLine, EndCol: endCol}
	return &IfStmt{Span: span, Condition: condition, Then: then, Else: elseNode}
}

func (p *Parser) parseWhileStmt() *WhileStmt {
	start := p.advance() // while
	condition := p.parseExpression()
	body := p.parseBlock()
	span := Span{StartLine: start.Line, StartCol: start.Col, EndLine: body.Span.EndLine, EndCol: body.Span.EndCol}
	return &WhileStmt{Span: span, Condition: condition, Body: body}
}

func (p *Parser) parseReturnStmt() *ReturnStmt {
	start := p.advance() // return
	var value Expr
	if !p.check(TokSemicolon) {
		value = p.parseExpression()
	}
	end, err := p.expect(TokSemicolon)
	if err != nil {
		panic("Expected ';' after return")
	}
	span := Span{StartLine: start.Line, StartCol: start.Col, EndLine: end.Line, EndCol: end.Col}
	return &ReturnStmt{Span: span, Value: value}
}

func (p *Parser) parseBlock() *Block {
	start, err := p.expect(TokLBrace)
	if err != nil {
		panic("Expected '{'")
	}
	var stmts []Node
	for !p.check(TokRBrace) && !p.check(TokEOF) {
		decl := p.parseDeclaration()
		if decl != nil {
			stmts = append(stmts, decl)
		}
	}
	end, err := p.expect(TokRBrace)
	if err != nil {
		panic("Expected '}'")
	}
	span := Span{StartLine: start.Line, StartCol: start.Col, EndLine: end.Line, EndCol: end.Col}
	return &Block{Span: span, Stmts: stmts}
}

func (p *Parser) parseExprStmt() *ExprStmt {
	expr := p.parseExpression()
	end, err := p.expect(TokSemicolon)
	if err != nil {
		panic("Expected ';' after expression")
	}
	span := expr.GetSpan()
	span.EndLine = end.Line
	span.EndCol = end.Col
	return &ExprStmt{Span: span, Expr: expr}
}

func (p *Parser) parseExpression() Expr {
	return p.parseAssignment()
}

func (p *Parser) parseAssignment() Expr {
	expr := p.parseOr()
	if _, ok := p.match(TokEq); ok {
		value := p.parseAssignment()
		if ident, ok := expr.(*Identifier); ok {
			span := Span{
				StartLine: ident.Span.StartLine, StartCol: ident.Span.StartCol,
				EndLine: value.GetSpan().EndLine, EndCol: value.GetSpan().EndCol,
			}
			return &AssignExpr{Span: span, Name: ident.Name, Value: value}
		}
		panic("Invalid assignment target")
	}
	return expr
}

func (p *Parser) parseOr() Expr {
	left := p.parseAnd()
	for p.check(KwOr) {
		op := p.advance()
		right := p.parseAnd()
		span := Span{StartLine: left.GetSpan().StartLine, StartCol: left.GetSpan().StartCol,
			EndLine: right.GetSpan().EndLine, EndCol: right.GetSpan().EndCol}
		left = &BinaryExpr{Span: span, Left: left, Op: op.Value, Right: right}
	}
	return left
}

func (p *Parser) parseAnd() Expr {
	left := p.parseEquality()
	for p.check(KwAnd) {
		op := p.advance()
		right := p.parseEquality()
		span := Span{StartLine: left.GetSpan().StartLine, StartCol: left.GetSpan().StartCol,
			EndLine: right.GetSpan().EndLine, EndCol: right.GetSpan().EndCol}
		left = &BinaryExpr{Span: span, Left: left, Op: op.Value, Right: right}
	}
	return left
}

func (p *Parser) parseEquality() Expr {
	left := p.parseComparison()
	for p.check(TokEqEq) || p.check(TokBangEq) {
		op := p.advance()
		right := p.parseComparison()
		span := Span{StartLine: left.GetSpan().StartLine, StartCol: left.GetSpan().StartCol,
			EndLine: right.GetSpan().EndLine, EndCol: right.GetSpan().EndCol}
		left = &BinaryExpr{Span: span, Left: left, Op: op.Value, Right: right}
	}
	return left
}

func (p *Parser) parseComparison() Expr {
	left := p.parseTerm()
	for p.check(TokLt) || p.check(TokGt) || p.check(TokLtEq) || p.check(TokGtEq) {
		op := p.advance()
		right := p.parseTerm()
		span := Span{StartLine: left.GetSpan().StartLine, StartCol: left.GetSpan().StartCol,
			EndLine: right.GetSpan().EndLine, EndCol: right.GetSpan().EndCol}
		left = &BinaryExpr{Span: span, Left: left, Op: op.Value, Right: right}
	}
	return left
}

func (p *Parser) parseTerm() Expr {
	left := p.parseFactor()
	for p.check(TokPlus) || p.check(TokMinus) {
		op := p.advance()
		right := p.parseFactor()
		span := Span{StartLine: left.GetSpan().StartLine, StartCol: left.GetSpan().StartCol,
			EndLine: right.GetSpan().EndLine, EndCol: right.GetSpan().EndCol}
		left = &BinaryExpr{Span: span, Left: left, Op: op.Value, Right: right}
	}
	return left
}

func (p *Parser) parseFactor() Expr {
	left := p.parseUnary()
	for p.check(TokStar) || p.check(TokSlash) {
		op := p.advance()
		right := p.parseUnary()
		span := Span{StartLine: left.GetSpan().StartLine, StartCol: left.GetSpan().StartCol,
			EndLine: right.GetSpan().EndLine, EndCol: right.GetSpan().EndCol}
		left = &BinaryExpr{Span: span, Left: left, Op: op.Value, Right: right}
	}
	return left
}

func (p *Parser) parseUnary() Expr {
	if p.check(TokMinus) || p.check(KwNot) {
		op := p.advance()
		expr := p.parseUnary()
		span := Span{StartLine: op.Line, StartCol: op.Col,
			EndLine: expr.GetSpan().EndLine, EndCol: expr.GetSpan().EndCol}
		return &UnaryExpr{Span: span, Op: op.Value, Expr: expr}
	}
	return p.parseCall()
}

func (p *Parser) parseCall() Expr {
	expr := p.parsePrimary()
	for p.check(TokLParen) {
		p.advance()
		var args []Expr
		if !p.check(TokRParen) {
			for {
				args = append(args, p.parseExpression())
				if _, ok := p.match(TokComma); !ok { break }
			}
		}
		end, err := p.expect(TokRParen)
		if err != nil {
			panic("Expected ')' after arguments")
		}
		span := Span{StartLine: expr.GetSpan().StartLine, StartCol: expr.GetSpan().StartCol,
			EndLine: end.Line, EndCol: end.Col}
		expr = &CallExpr{Span: span, Callee: expr, Args: args}
	}
	return expr
}

func (p *Parser) parsePrimary() Expr {
	tok := p.peek()
	switch tok.Kind {
	case TokNumber:
		p.advance()
		span := Span{StartLine: tok.Line, StartCol: tok.Col, EndLine: tok.Line, EndCol: tok.Col + len(tok.Value)}
		return &NumberLit{Span: span, Value: parseNumber(tok.Value)}
	case TokString:
		p.advance()
		span := Span{StartLine: tok.Line, StartCol: tok.Col, EndLine: tok.Line, EndCol: tok.Col + len(tok.Value) + 2}
		return &StringLit{Span: span, Value: tok.Value}
	case KwTrue:
		p.advance()
		span := Span{StartLine: tok.Line, StartCol: tok.Col, EndLine: tok.Line, EndCol: tok.Col + 4}
		return &BoolLit{Span: span, Value: true}
	case KwFalse:
		p.advance()
		span := Span{StartLine: tok.Line, StartCol: tok.Col, EndLine: tok.Line, EndCol: tok.Col + 5}
		return &BoolLit{Span: span, Value: false}
	case TokIdent:
		p.advance()
		span := Span{StartLine: tok.Line, StartCol: tok.Col, EndLine: tok.Line, EndCol: tok.Col + len(tok.Value)}
		return &Identifier{Span: span, Name: tok.Value}
	case TokLParen:
		p.advance()
		expr := p.parseExpression()
		end, err := p.expect(TokRParen)
		if err != nil {
			panic("Expected ')' after expression")
		}
		_ = end
		return expr
	default:
		panic(fmt.Sprintf("Expected expression, found '%s'", tok.Value))
	}
}
```

### parser_test.go

```go
package lang

import (
	"strings"
	"testing"
)

func TestParseVarDecl(t *testing.T) {
	prog, errs := ParseProgram("let x = 42;")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(prog.Decls) != 1 {
		t.Fatalf("expected 1 declaration, got %d", len(prog.Decls))
	}
	vd, ok := prog.Decls[0].(*VarDecl)
	if !ok {
		t.Fatal("expected VarDecl")
	}
	if vd.Name != "x" {
		t.Errorf("expected variable name 'x', got '%s'", vd.Name)
	}
}

func TestParseFunction(t *testing.T) {
	src := `fn add(a, b) { return a + b; }`
	prog, errs := ParseProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	fd, ok := prog.Decls[0].(*FunDecl)
	if !ok {
		t.Fatal("expected FunDecl")
	}
	if fd.Name != "add" {
		t.Errorf("expected 'add', got '%s'", fd.Name)
	}
	if len(fd.Params) != 2 {
		t.Errorf("expected 2 params, got %d", len(fd.Params))
	}
}

func TestParseIfElse(t *testing.T) {
	src := `if x > 0 { print x; } else { print 0; }`
	prog, errs := ParseProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	ifStmt, ok := prog.Decls[0].(*IfStmt)
	if !ok {
		t.Fatal("expected IfStmt")
	}
	if ifStmt.Else == nil {
		t.Error("expected else branch")
	}
}

func TestParseWhile(t *testing.T) {
	src := `while x < 10 { x = x + 1; }`
	prog, errs := ParseProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	_, ok := prog.Decls[0].(*WhileStmt)
	if !ok {
		t.Fatal("expected WhileStmt")
	}
}

func TestErrorRecovery(t *testing.T) {
	src := `let x = ;
let y = 5;
let z = ;`
	_, errs := ParseProgram(src)
	if len(errs) < 2 {
		t.Errorf("expected at least 2 errors, got %d", len(errs))
	}
	// Despite errors, y should still be parsed
}

func TestMultipleErrors(t *testing.T) {
	src := `let a = ;
print ;
let b = 5;
fn foo( { }
let c = 10;`
	_, errs := ParseProgram(src)
	if len(errs) < 3 {
		t.Errorf("expected at least 3 errors, got %d: %v", len(errs), errs)
	}
}

func TestSpanTracking(t *testing.T) {
	src := "let x = 42;"
	prog, _ := ParseProgram(src)
	vd := prog.Decls[0].(*VarDecl)
	if vd.Span.StartLine != 1 || vd.Span.StartCol != 1 {
		t.Errorf("expected span start 1:1, got %d:%d", vd.Span.StartLine, vd.Span.StartCol)
	}
}

func TestPrettyPrinter(t *testing.T) {
	src := `let x = 42;
print x;
fn add(a, b) {
    return a + b;
}
if x > 0 {
    print x;
} else {
    print 0;
}`
	prog, errs := ParseProgram(src)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	pp := NewPrettyPrinter()
	prog.Accept(pp)
	output := pp.String()
	if !strings.Contains(output, "let x = 42;") {
		t.Error("pretty printer output should contain 'let x = 42;'")
	}
	if !strings.Contains(output, "fn add(a, b)") {
		t.Error("pretty printer output should contain 'fn add(a, b)'")
	}
}

func TestNodeCounter(t *testing.T) {
	src := `let x = 1; let y = 2; print x + y;`
	prog, _ := ParseProgram(src)
	counter := NewNodeCounter()
	prog.Accept(counter)
	if counter.Counts["VarDecl"] != 2 {
		t.Errorf("expected 2 VarDecl, got %d", counter.Counts["VarDecl"])
	}
	if counter.Counts["PrintStmt"] != 1 {
		t.Errorf("expected 1 PrintStmt, got %d", counter.Counts["PrintStmt"])
	}
	if counter.Total() < 5 {
		t.Errorf("expected at least 5 total nodes, got %d", counter.Total())
	}
}
```

## Complete Solution (Rust)

Due to the size of the full Rust solution, the key structural differences from Go are highlighted.

### src/ast.rs

```rust
#[derive(Debug, Clone, Copy)]
pub struct Span {
    pub start_line: usize,
    pub start_col: usize,
    pub end_line: usize,
    pub end_col: usize,
}

#[derive(Debug)]
pub enum Stmt {
    VarDecl { span: Span, name: String, value: Option<Expr> },
    FunDecl { span: Span, name: String, params: Vec<String>, body: Vec<Stmt> },
    Block { span: Span, stmts: Vec<Stmt> },
    Print { span: Span, expr: Expr },
    If { span: Span, condition: Expr, then_block: Vec<Stmt>, else_block: Option<Vec<Stmt>> },
    While { span: Span, condition: Expr, body: Vec<Stmt> },
    Return { span: Span, value: Option<Expr> },
    ExprStmt { span: Span, expr: Expr },
}

#[derive(Debug)]
pub enum Expr {
    Number { span: Span, value: f64 },
    StringLit { span: Span, value: String },
    Bool { span: Span, value: bool },
    Ident { span: Span, name: String },
    Binary { span: Span, left: Box<Expr>, op: String, right: Box<Expr> },
    Unary { span: Span, op: String, expr: Box<Expr> },
    Assign { span: Span, name: String, value: Box<Expr> },
    Call { span: Span, callee: Box<Expr>, args: Vec<Expr> },
}

impl Stmt {
    pub fn span(&self) -> Span {
        match self {
            Stmt::VarDecl { span, .. } | Stmt::FunDecl { span, .. } |
            Stmt::Block { span, .. } | Stmt::Print { span, .. } |
            Stmt::If { span, .. } | Stmt::While { span, .. } |
            Stmt::Return { span, .. } | Stmt::ExprStmt { span, .. } => *span,
        }
    }
}

impl Expr {
    pub fn span(&self) -> Span {
        match self {
            Expr::Number { span, .. } | Expr::StringLit { span, .. } |
            Expr::Bool { span, .. } | Expr::Ident { span, .. } |
            Expr::Binary { span, .. } | Expr::Unary { span, .. } |
            Expr::Assign { span, .. } | Expr::Call { span, .. } => *span,
        }
    }
}
```

### src/visitor.rs

```rust
use crate::ast::*;

pub trait Visitor {
    fn visit_stmt(&mut self, stmt: &Stmt) {
        match stmt {
            Stmt::VarDecl { name, value, .. } => self.visit_var_decl(name, value.as_ref()),
            Stmt::FunDecl { name, params, body, .. } => self.visit_fun_decl(name, params, body),
            Stmt::Block { stmts, .. } => self.visit_block(stmts),
            Stmt::Print { expr, .. } => self.visit_print(expr),
            Stmt::If { condition, then_block, else_block, .. } =>
                self.visit_if(condition, then_block, else_block.as_deref()),
            Stmt::While { condition, body, .. } => self.visit_while(condition, body),
            Stmt::Return { value, .. } => self.visit_return(value.as_ref()),
            Stmt::ExprStmt { expr, .. } => self.visit_expr_stmt(expr),
        }
    }

    fn visit_var_decl(&mut self, name: &str, value: Option<&Expr>);
    fn visit_fun_decl(&mut self, name: &str, params: &[String], body: &[Stmt]);
    fn visit_block(&mut self, stmts: &[Stmt]);
    fn visit_print(&mut self, expr: &Expr);
    fn visit_if(&mut self, condition: &Expr, then_block: &[Stmt], else_block: Option<&[Stmt]>);
    fn visit_while(&mut self, condition: &Expr, body: &[Stmt]);
    fn visit_return(&mut self, value: Option<&Expr>);
    fn visit_expr_stmt(&mut self, expr: &Expr);
    fn visit_expr(&mut self, expr: &Expr);
}

pub struct PrettyPrinter {
    pub output: String,
    indent: usize,
}

impl PrettyPrinter {
    pub fn new() -> Self {
        PrettyPrinter { output: String::new(), indent: 0 }
    }

    fn write_indent(&mut self) {
        for _ in 0..self.indent {
            self.output.push_str("    ");
        }
    }

    fn print_expr(&mut self, expr: &Expr) {
        match expr {
            Expr::Number { value, .. } => {
                if *value == (*value as i64) as f64 {
                    self.output.push_str(&format!("{}", *value as i64));
                } else {
                    self.output.push_str(&format!("{}", value));
                }
            }
            Expr::StringLit { value, .. } => {
                self.output.push_str(&format!("\"{}\"", value));
            }
            Expr::Bool { value, .. } => {
                self.output.push_str(if *value { "true" } else { "false" });
            }
            Expr::Ident { name, .. } => self.output.push_str(name),
            Expr::Binary { left, op, right, .. } => {
                self.output.push('(');
                self.print_expr(left);
                self.output.push_str(&format!(" {} ", op));
                self.print_expr(right);
                self.output.push(')');
            }
            Expr::Unary { op, expr, .. } => {
                self.output.push_str(op);
                self.print_expr(expr);
            }
            Expr::Assign { name, value, .. } => {
                self.output.push_str(name);
                self.output.push_str(" = ");
                self.print_expr(value);
            }
            Expr::Call { callee, args, .. } => {
                self.print_expr(callee);
                self.output.push('(');
                for (i, arg) in args.iter().enumerate() {
                    if i > 0 { self.output.push_str(", "); }
                    self.print_expr(arg);
                }
                self.output.push(')');
            }
        }
    }
}

impl Visitor for PrettyPrinter {
    fn visit_var_decl(&mut self, name: &str, value: Option<&Expr>) {
        self.write_indent();
        self.output.push_str(&format!("let {}", name));
        if let Some(v) = value {
            self.output.push_str(" = ");
            self.print_expr(v);
        }
        self.output.push_str(";\n");
    }

    fn visit_fun_decl(&mut self, name: &str, params: &[String], body: &[Stmt]) {
        self.write_indent();
        self.output.push_str(&format!("fn {}({}) ", name, params.join(", ")));
        self.output.push_str("{\n");
        self.indent += 1;
        for stmt in body { self.visit_stmt(stmt); }
        self.indent -= 1;
        self.write_indent();
        self.output.push_str("}\n");
    }

    fn visit_block(&mut self, stmts: &[Stmt]) {
        self.write_indent();
        self.output.push_str("{\n");
        self.indent += 1;
        for stmt in stmts { self.visit_stmt(stmt); }
        self.indent -= 1;
        self.write_indent();
        self.output.push_str("}\n");
    }

    fn visit_print(&mut self, expr: &Expr) {
        self.write_indent();
        self.output.push_str("print ");
        self.print_expr(expr);
        self.output.push_str(";\n");
    }

    fn visit_if(&mut self, condition: &Expr, then_block: &[Stmt], else_block: Option<&[Stmt]>) {
        self.write_indent();
        self.output.push_str("if ");
        self.print_expr(condition);
        self.output.push_str(" {\n");
        self.indent += 1;
        for stmt in then_block { self.visit_stmt(stmt); }
        self.indent -= 1;
        self.write_indent();
        self.output.push('}');
        if let Some(els) = else_block {
            self.output.push_str(" else {\n");
            self.indent += 1;
            for stmt in els { self.visit_stmt(stmt); }
            self.indent -= 1;
            self.write_indent();
            self.output.push('}');
        }
        self.output.push('\n');
    }

    fn visit_while(&mut self, condition: &Expr, body: &[Stmt]) {
        self.write_indent();
        self.output.push_str("while ");
        self.print_expr(condition);
        self.output.push_str(" {\n");
        self.indent += 1;
        for stmt in body { self.visit_stmt(stmt); }
        self.indent -= 1;
        self.write_indent();
        self.output.push_str("}\n");
    }

    fn visit_return(&mut self, value: Option<&Expr>) {
        self.write_indent();
        self.output.push_str("return");
        if let Some(v) = value {
            self.output.push(' ');
            self.print_expr(v);
        }
        self.output.push_str(";\n");
    }

    fn visit_expr_stmt(&mut self, expr: &Expr) {
        self.write_indent();
        self.print_expr(expr);
        self.output.push_str(";\n");
    }

    fn visit_expr(&mut self, expr: &Expr) { self.print_expr(expr); }
}

pub struct NodeCounter {
    pub counts: std::collections::HashMap<String, usize>,
}

impl NodeCounter {
    pub fn new() -> Self {
        NodeCounter { counts: std::collections::HashMap::new() }
    }
    fn inc(&mut self, name: &str) { *self.counts.entry(name.to_string()).or_insert(0) += 1; }
    pub fn total(&self) -> usize { self.counts.values().sum() }

    fn count_expr(&mut self, expr: &Expr) {
        match expr {
            Expr::Number { .. } => self.inc("Number"),
            Expr::StringLit { .. } => self.inc("String"),
            Expr::Bool { .. } => self.inc("Bool"),
            Expr::Ident { .. } => self.inc("Ident"),
            Expr::Binary { left, right, .. } => {
                self.inc("Binary");
                self.count_expr(left);
                self.count_expr(right);
            }
            Expr::Unary { expr, .. } => { self.inc("Unary"); self.count_expr(expr); }
            Expr::Assign { value, .. } => { self.inc("Assign"); self.count_expr(value); }
            Expr::Call { callee, args, .. } => {
                self.inc("Call");
                self.count_expr(callee);
                for a in args { self.count_expr(a); }
            }
        }
    }
}

impl Visitor for NodeCounter {
    fn visit_var_decl(&mut self, _: &str, value: Option<&Expr>) {
        self.inc("VarDecl");
        if let Some(v) = value { self.count_expr(v); }
    }
    fn visit_fun_decl(&mut self, _: &str, _: &[String], body: &[Stmt]) {
        self.inc("FunDecl");
        for s in body { self.visit_stmt(s); }
    }
    fn visit_block(&mut self, stmts: &[Stmt]) {
        self.inc("Block");
        for s in stmts { self.visit_stmt(s); }
    }
    fn visit_print(&mut self, expr: &Expr) { self.inc("Print"); self.count_expr(expr); }
    fn visit_if(&mut self, cond: &Expr, then: &[Stmt], els: Option<&[Stmt]>) {
        self.inc("If");
        self.count_expr(cond);
        for s in then { self.visit_stmt(s); }
        if let Some(e) = els { for s in e { self.visit_stmt(s); } }
    }
    fn visit_while(&mut self, cond: &Expr, body: &[Stmt]) {
        self.inc("While");
        self.count_expr(cond);
        for s in body { self.visit_stmt(s); }
    }
    fn visit_return(&mut self, value: Option<&Expr>) {
        self.inc("Return");
        if let Some(v) = value { self.count_expr(v); }
    }
    fn visit_expr_stmt(&mut self, expr: &Expr) { self.inc("ExprStmt"); self.count_expr(expr); }
    fn visit_expr(&mut self, expr: &Expr) { self.count_expr(expr); }
}
```

## Running

```bash
# Go
go test -v ./...

# Rust
cargo test
```

## Expected Output

```
# Go tests
=== RUN   TestParseVarDecl
--- PASS
=== RUN   TestParseFunction
--- PASS
=== RUN   TestParseIfElse
--- PASS
=== RUN   TestParseWhile
--- PASS
=== RUN   TestErrorRecovery
--- PASS
=== RUN   TestMultipleErrors
--- PASS
=== RUN   TestSpanTracking
--- PASS
=== RUN   TestPrettyPrinter
--- PASS
=== RUN   TestNodeCounter
--- PASS
PASS
```

## Design Decisions

1. **Panic-mode recovery**: On a parse error in a statement, the parser catches the panic (Go) or returns an error (Rust), then advances to the next synchronization point (`;`, `}`, or a statement keyword). This approach is simple and effective for reporting multiple errors without complex state machines.

2. **Span on every node**: Adding source location to every AST node increases memory but is essential for error reporting in later compiler phases. The span covers the full extent of the production, from first to last token.

3. **Go interface vs Rust enum for AST**: Go uses the visitor pattern with interfaces and concrete struct types, requiring double dispatch. Rust uses enums and `match`, giving exhaustive checking at compile time. The Rust visitor trait provides default dispatch via `visit_stmt`, reducing boilerplate.

4. **Expressions as statements**: `ExprStmt` wraps an expression where a statement is expected. This allows function calls (`foo();`) as standalone statements. Assignment is an expression that returns a value, following C-family semantics.

## Common Mistakes

1. **Error recovery consuming too much**: If synchronization is too aggressive (advancing past `}`), you skip valid code and report phantom errors. If too conservative (stopping at every `;`), one error per line at most. Balance by checking multiple synchronization tokens.

2. **Span boundaries on error nodes**: When an error occurs mid-parse, the span for the error node must still be valid. Using the last successfully consumed token's position prevents invalid spans.

3. **Left-recursive grammars**: Recursive descent cannot handle left recursion directly. `expr -> expr + term` causes infinite recursion. Convert to `expr -> term (+ term)*` using a while loop.

4. **Precedence inversion**: Writing `parseFactor` that calls `parseTerm` instead of the other way around inverts all operator precedence. The lowest-precedence function must be the entry point.

## Performance Notes

- Go's panic/recover for error recovery has minimal overhead since it only fires on actual errors, not the happy path. Rust's `Result` propagation is zero-cost on the happy path.
- AST allocation is the main cost. Both solutions heap-allocate every node. Arena allocation would reduce allocation overhead for large programs.
- The visitor pattern in Go requires virtual dispatch for every node visit. In Rust, monomorphization eliminates this overhead when the visitor type is known at compile time.

## Going Further

- Add a tree-walking interpreter that executes the AST directly
- Implement scope analysis: detect undefined variables, duplicate declarations, unused variables
- Add type annotations and a basic type checker (see challenge 24)
- Implement constant folding as a visitor that simplifies `1 + 2` to `3` at compile time
- Generate bytecode from the AST and execute it on a stack-based VM (see challenge 25)

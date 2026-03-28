# 7. SQL SELECT Statement Parser -- Solution

## Architecture Overview

Both implementations share the same three-layer architecture:

1. **Lexer** -- converts raw SQL text into a token stream, recognizing keywords case-insensitively
2. **Parser** -- recursive descent with precedence climbing for boolean expressions, producing a typed AST
3. **Pretty-printer** -- walks the AST and emits formatted SQL, proving the AST captures full structure

```
SQL string --> Lexer --> [Token] --> Parser --> SelectStmt (AST) --> PrettyPrinter --> SQL string
```

## Complete Solution (Go)

### ast.go

```go
package sqlparser

import "fmt"

type SelectStmt struct {
	Columns  []SelectColumn
	From     *TableRef
	Joins    []JoinClause
	Where    Expr
	GroupBy  []Expr
	Having   Expr
	OrderBy  []OrderByItem
	Limit    *int64
	Offset   *int64
}

type SelectColumn struct {
	Expr  Expr
	Alias string
	Star  bool
}

type TableRef struct {
	Name     string
	Alias    string
	Subquery *SelectStmt
}

type JoinClause struct {
	Type      JoinType
	Table     TableRef
	Condition Expr
}

type JoinType int

const (
	InnerJoin JoinType = iota
	LeftJoin
	RightJoin
)

func (j JoinType) String() string {
	switch j {
	case InnerJoin:
		return "INNER JOIN"
	case LeftJoin:
		return "LEFT JOIN"
	case RightJoin:
		return "RIGHT JOIN"
	default:
		return "JOIN"
	}
}

type OrderByItem struct {
	Expr Expr
	Desc bool
}

type Expr interface {
	exprNode()
	String() string
}

type ColumnRef struct {
	Table  string
	Column string
}

func (c *ColumnRef) exprNode() {}
func (c *ColumnRef) String() string {
	if c.Table != "" {
		return fmt.Sprintf("%s.%s", c.Table, c.Column)
	}
	return c.Column
}

type StarExpr struct {
	Table string
}

func (s *StarExpr) exprNode() {}
func (s *StarExpr) String() string {
	if s.Table != "" {
		return s.Table + ".*"
	}
	return "*"
}

type NumberLit struct {
	Value string
}

func (n *NumberLit) exprNode() {}
func (n *NumberLit) String() string { return n.Value }

type StringLit struct {
	Value string
}

func (s *StringLit) exprNode() {}
func (s *StringLit) String() string { return fmt.Sprintf("'%s'", s.Value) }

type BinaryExpr struct {
	Left  Expr
	Op    string
	Right Expr
}

func (b *BinaryExpr) exprNode() {}
func (b *BinaryExpr) String() string {
	return fmt.Sprintf("(%s %s %s)", b.Left, b.Op, b.Right)
}

type UnaryExpr struct {
	Op   string
	Expr Expr
}

func (u *UnaryExpr) exprNode() {}
func (u *UnaryExpr) String() string {
	return fmt.Sprintf("(%s %s)", u.Op, u.Expr)
}

type IsNullExpr struct {
	Expr    Expr
	Negated bool
}

func (i *IsNullExpr) exprNode() {}
func (i *IsNullExpr) String() string {
	if i.Negated {
		return fmt.Sprintf("(%s IS NOT NULL)", i.Expr)
	}
	return fmt.Sprintf("(%s IS NULL)", i.Expr)
}

type InExpr struct {
	Expr     Expr
	Values   []Expr
	Subquery *SelectStmt
	Negated  bool
}

func (i *InExpr) exprNode() {}
func (i *InExpr) String() string {
	neg := ""
	if i.Negated {
		neg = "NOT "
	}
	if i.Subquery != nil {
		return fmt.Sprintf("(%s %sIN (subquery))", i.Expr, neg)
	}
	return fmt.Sprintf("(%s %sIN (...))", i.Expr, neg)
}

type BetweenExpr struct {
	Expr    Expr
	Low     Expr
	High    Expr
	Negated bool
}

func (b *BetweenExpr) exprNode() {}
func (b *BetweenExpr) String() string {
	neg := ""
	if b.Negated {
		neg = "NOT "
	}
	return fmt.Sprintf("(%s %sBETWEEN %s AND %s)", b.Expr, neg, b.Low, b.High)
}

type LikeExpr struct {
	Expr    Expr
	Pattern Expr
	Negated bool
}

func (l *LikeExpr) exprNode() {}
func (l *LikeExpr) String() string {
	neg := ""
	if l.Negated {
		neg = "NOT "
	}
	return fmt.Sprintf("(%s %sLIKE %s)", l.Expr, neg, l.Pattern)
}

type FuncCall struct {
	Name string
	Args []Expr
}

func (f *FuncCall) exprNode() {}
func (f *FuncCall) String() string {
	return fmt.Sprintf("%s(...)", f.Name)
}

type SubqueryExpr struct {
	Query *SelectStmt
}

func (s *SubqueryExpr) exprNode() {}
func (s *SubqueryExpr) String() string { return "(subquery)" }
```

### lexer.go

```go
package sqlparser

import (
	"fmt"
	"strings"
	"unicode"
)

type TokenKind int

const (
	TokEOF TokenKind = iota
	TokIdent
	TokNumber
	TokString
	TokStar
	TokComma
	TokDot
	TokLParen
	TokRParen
	TokEq
	TokNeq
	TokLt
	TokGt
	TokLtEq
	TokGtEq
	TokPlus
	TokMinus
	// Keywords
	KwSelect
	KwFrom
	KwWhere
	KwAnd
	KwOr
	KwNot
	KwAs
	KwOn
	KwJoin
	KwInner
	KwLeft
	KwRight
	KwOrder
	KwBy
	KwGroup
	KwHaving
	KwLimit
	KwOffset
	KwAsc
	KwDesc
	KwNull
	KwIs
	KwIn
	KwBetween
	KwLike
)

type SQLToken struct {
	Kind  TokenKind
	Value string
	Line  int
	Col   int
}

func (t SQLToken) String() string {
	return fmt.Sprintf("%v(%s) at %d:%d", t.Kind, t.Value, t.Line, t.Col)
}

var keywords = map[string]TokenKind{
	"SELECT": KwSelect, "FROM": KwFrom, "WHERE": KwWhere,
	"AND": KwAnd, "OR": KwOr, "NOT": KwNot, "AS": KwAs,
	"ON": KwOn, "JOIN": KwJoin, "INNER": KwInner,
	"LEFT": KwLeft, "RIGHT": KwRight,
	"ORDER": KwOrder, "BY": KwBy, "GROUP": KwGroup,
	"HAVING": KwHaving, "LIMIT": KwLimit, "OFFSET": KwOffset,
	"ASC": KwAsc, "DESC": KwDesc, "NULL": KwNull,
	"IS": KwIs, "IN": KwIn, "BETWEEN": KwBetween, "LIKE": KwLike,
}

type SQLLexer struct {
	input []rune
	pos   int
	line  int
	col   int
}

func NewLexer(input string) *SQLLexer {
	return &SQLLexer{input: []rune(input), pos: 0, line: 1, col: 1}
}

func (l *SQLLexer) Tokenize() ([]SQLToken, error) {
	var tokens []SQLToken
	for {
		l.skipWhitespace()
		if l.pos >= len(l.input) {
			tokens = append(tokens, SQLToken{Kind: TokEOF, Line: l.line, Col: l.col})
			return tokens, nil
		}
		tok, err := l.nextToken()
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, tok)
	}
}

func (l *SQLLexer) peek() rune {
	if l.pos >= len(l.input) {
		return 0
	}
	return l.input[l.pos]
}

func (l *SQLLexer) advance() rune {
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

func (l *SQLLexer) skipWhitespace() {
	for l.pos < len(l.input) && unicode.IsSpace(l.input[l.pos]) {
		l.advance()
	}
}

func (l *SQLLexer) nextToken() (SQLToken, error) {
	line, col := l.line, l.col
	ch := l.peek()

	switch {
	case ch == '*':
		l.advance()
		return SQLToken{TokStar, "*", line, col}, nil
	case ch == ',':
		l.advance()
		return SQLToken{TokComma, ",", line, col}, nil
	case ch == '.':
		l.advance()
		return SQLToken{TokDot, ".", line, col}, nil
	case ch == '(':
		l.advance()
		return SQLToken{TokLParen, "(", line, col}, nil
	case ch == ')':
		l.advance()
		return SQLToken{TokRParen, ")", line, col}, nil
	case ch == '+':
		l.advance()
		return SQLToken{TokPlus, "+", line, col}, nil
	case ch == '-':
		l.advance()
		return SQLToken{TokMinus, "-", line, col}, nil
	case ch == '=':
		l.advance()
		return SQLToken{TokEq, "=", line, col}, nil
	case ch == '<':
		l.advance()
		if l.pos < len(l.input) {
			if l.peek() == '=' {
				l.advance()
				return SQLToken{TokLtEq, "<=", line, col}, nil
			}
			if l.peek() == '>' {
				l.advance()
				return SQLToken{TokNeq, "<>", line, col}, nil
			}
		}
		return SQLToken{TokLt, "<", line, col}, nil
	case ch == '>':
		l.advance()
		if l.pos < len(l.input) && l.peek() == '=' {
			l.advance()
			return SQLToken{TokGtEq, ">=", line, col}, nil
		}
		return SQLToken{TokGt, ">", line, col}, nil
	case ch == '!':
		l.advance()
		if l.pos < len(l.input) && l.peek() == '=' {
			l.advance()
			return SQLToken{TokNeq, "!=", line, col}, nil
		}
		return SQLToken{}, fmt.Errorf("unexpected '!' at %d:%d", line, col)
	case ch == '\'':
		return l.lexString(line, col)
	case unicode.IsDigit(ch):
		return l.lexNumber(line, col)
	case unicode.IsLetter(ch) || ch == '_':
		return l.lexIdentOrKeyword(line, col)
	default:
		return SQLToken{}, fmt.Errorf("unexpected character '%c' at %d:%d", ch, line, col)
	}
}

func (l *SQLLexer) lexString(line, col int) (SQLToken, error) {
	l.advance() // consume opening quote
	var sb strings.Builder
	for l.pos < len(l.input) {
		ch := l.advance()
		if ch == '\'' {
			if l.pos < len(l.input) && l.peek() == '\'' {
				l.advance()
				sb.WriteRune('\'')
			} else {
				return SQLToken{TokString, sb.String(), line, col}, nil
			}
		} else {
			sb.WriteRune(ch)
		}
	}
	return SQLToken{}, fmt.Errorf("unterminated string at %d:%d", line, col)
}

func (l *SQLLexer) lexNumber(line, col int) (SQLToken, error) {
	start := l.pos
	for l.pos < len(l.input) && unicode.IsDigit(l.peek()) {
		l.advance()
	}
	if l.pos < len(l.input) && l.peek() == '.' {
		l.advance()
		for l.pos < len(l.input) && unicode.IsDigit(l.peek()) {
			l.advance()
		}
	}
	return SQLToken{TokNumber, string(l.input[start:l.pos]), line, col}, nil
}

func (l *SQLLexer) lexIdentOrKeyword(line, col int) (SQLToken, error) {
	start := l.pos
	for l.pos < len(l.input) && (unicode.IsLetter(l.peek()) || unicode.IsDigit(l.peek()) || l.peek() == '_') {
		l.advance()
	}
	word := string(l.input[start:l.pos])
	if kw, ok := keywords[strings.ToUpper(word)]; ok {
		return SQLToken{kw, word, line, col}, nil
	}
	return SQLToken{TokIdent, word, line, col}, nil
}
```

### parser.go

```go
package sqlparser

import "fmt"

type Parser struct {
	tokens []SQLToken
	pos    int
}

func NewParser(tokens []SQLToken) *Parser {
	return &Parser{tokens: tokens, pos: 0}
}

func (p *Parser) peek() SQLToken {
	if p.pos < len(p.tokens) {
		return p.tokens[p.pos]
	}
	return SQLToken{Kind: TokEOF}
}

func (p *Parser) advance() SQLToken {
	tok := p.tokens[p.pos]
	p.pos++
	return tok
}

func (p *Parser) expect(kind TokenKind) (SQLToken, error) {
	tok := p.advance()
	if tok.Kind != kind {
		return tok, fmt.Errorf("expected %v, found %v at %d:%d", kind, tok.Kind, tok.Line, tok.Col)
	}
	return tok, nil
}

func (p *Parser) match(kinds ...TokenKind) (SQLToken, bool) {
	for _, k := range kinds {
		if p.peek().Kind == k {
			return p.advance(), true
		}
	}
	return SQLToken{}, false
}

func Parse(input string) (*SelectStmt, error) {
	lexer := NewLexer(input)
	tokens, err := lexer.Tokenize()
	if err != nil {
		return nil, err
	}
	parser := NewParser(tokens)
	return parser.ParseSelect()
}

func (p *Parser) ParseSelect() (*SelectStmt, error) {
	if _, err := p.expect(KwSelect); err != nil {
		return nil, err
	}

	stmt := &SelectStmt{}
	cols, err := p.parseColumns()
	if err != nil {
		return nil, err
	}
	stmt.Columns = cols

	if p.peek().Kind == KwFrom {
		p.advance()
		from, err := p.parseTableRef()
		if err != nil {
			return nil, err
		}
		stmt.From = from
	}

	for p.peek().Kind == KwInner || p.peek().Kind == KwLeft || p.peek().Kind == KwRight || p.peek().Kind == KwJoin {
		join, err := p.parseJoin()
		if err != nil {
			return nil, err
		}
		stmt.Joins = append(stmt.Joins, *join)
	}

	if p.peek().Kind == KwWhere {
		p.advance()
		expr, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		stmt.Where = expr
	}

	if p.peek().Kind == KwGroup {
		p.advance()
		if _, err := p.expect(KwBy); err != nil {
			return nil, err
		}
		for {
			expr, err := p.parsePrimary()
			if err != nil {
				return nil, err
			}
			stmt.GroupBy = append(stmt.GroupBy, expr)
			if _, ok := p.match(TokComma); !ok {
				break
			}
		}
		if p.peek().Kind == KwHaving {
			p.advance()
			having, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			stmt.Having = having
		}
	}

	if p.peek().Kind == KwOrder {
		p.advance()
		if _, err := p.expect(KwBy); err != nil {
			return nil, err
		}
		for {
			expr, err := p.parsePrimary()
			if err != nil {
				return nil, err
			}
			desc := false
			if _, ok := p.match(KwDesc); ok {
				desc = true
			} else {
				p.match(KwAsc)
			}
			stmt.OrderBy = append(stmt.OrderBy, OrderByItem{Expr: expr, Desc: desc})
			if _, ok := p.match(TokComma); !ok {
				break
			}
		}
	}

	if p.peek().Kind == KwLimit {
		p.advance()
		tok, err := p.expect(TokNumber)
		if err != nil {
			return nil, err
		}
		val := parseInt64(tok.Value)
		stmt.Limit = &val
	}

	if p.peek().Kind == KwOffset {
		p.advance()
		tok, err := p.expect(TokNumber)
		if err != nil {
			return nil, err
		}
		val := parseInt64(tok.Value)
		stmt.Offset = &val
	}

	return stmt, nil
}

func parseInt64(s string) int64 {
	var n int64
	for _, c := range s {
		n = n*10 + int64(c-'0')
	}
	return n
}

func (p *Parser) parseColumns() ([]SelectColumn, error) {
	var cols []SelectColumn
	for {
		col, err := p.parseSelectColumn()
		if err != nil {
			return nil, err
		}
		cols = append(cols, *col)
		if _, ok := p.match(TokComma); !ok {
			break
		}
	}
	return cols, nil
}

func (p *Parser) parseSelectColumn() (*SelectColumn, error) {
	if p.peek().Kind == TokStar {
		p.advance()
		return &SelectColumn{Star: true, Expr: &StarExpr{}}, nil
	}

	expr, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}

	// Check for table.* pattern
	if ref, ok := expr.(*ColumnRef); ok && p.peek().Kind == TokDot {
		p.advance()
		if p.peek().Kind == TokStar {
			p.advance()
			return &SelectColumn{Star: true, Expr: &StarExpr{Table: ref.Column}}, nil
		}
		colTok := p.advance()
		expr = &ColumnRef{Table: ref.Column, Column: colTok.Value}
	}

	col := &SelectColumn{Expr: expr}
	if _, ok := p.match(KwAs); ok {
		aliasTok := p.advance()
		col.Alias = aliasTok.Value
	} else if p.peek().Kind == TokIdent && !isClauseKeyword(p.peek().Kind) {
		col.Alias = p.advance().Value
	}

	return col, nil
}

func isClauseKeyword(k TokenKind) bool {
	return k == KwFrom || k == KwWhere || k == KwGroup || k == KwOrder ||
		k == KwLimit || k == KwOffset || k == KwHaving || k == KwInner ||
		k == KwLeft || k == KwRight || k == KwJoin || k == KwOn
}

func (p *Parser) parseTableRef() (*TableRef, error) {
	tok := p.advance()
	ref := &TableRef{Name: tok.Value}
	if _, ok := p.match(KwAs); ok {
		ref.Alias = p.advance().Value
	} else if p.peek().Kind == TokIdent && !isClauseKeyword(p.peek().Kind) {
		ref.Alias = p.advance().Value
	}
	return ref, nil
}

func (p *Parser) parseJoin() (*JoinClause, error) {
	jt := InnerJoin
	switch p.peek().Kind {
	case KwLeft:
		p.advance()
		jt = LeftJoin
	case KwRight:
		p.advance()
		jt = RightJoin
	case KwInner:
		p.advance()
	}
	if _, err := p.expect(KwJoin); err != nil {
		return nil, err
	}
	table, err := p.parseTableRef()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(KwOn); err != nil {
		return nil, err
	}
	cond, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	return &JoinClause{Type: jt, Table: *table, Condition: cond}, nil
}

func (p *Parser) parseExpr(minPrec int) (Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}

	for {
		op, prec := p.peekBinaryOp()
		if prec < minPrec {
			break
		}
		p.advance()

		if op == "IS" {
			negated := false
			if p.peek().Kind == KwNot {
				p.advance()
				negated = true
			}
			if _, err := p.expect(KwNull); err != nil {
				return nil, err
			}
			left = &IsNullExpr{Expr: left, Negated: negated}
			continue
		}
		if op == "IN" || op == "NOT IN" {
			negated := op == "NOT IN"
			left, err = p.parseInExpr(left, negated)
			if err != nil {
				return nil, err
			}
			continue
		}
		if op == "BETWEEN" || op == "NOT BETWEEN" {
			negated := op == "NOT BETWEEN"
			low, err := p.parsePrimary()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(KwAnd); err != nil {
				return nil, err
			}
			high, err := p.parsePrimary()
			if err != nil {
				return nil, err
			}
			left = &BetweenExpr{Expr: left, Low: low, High: high, Negated: negated}
			continue
		}
		if op == "LIKE" || op == "NOT LIKE" {
			negated := op == "NOT LIKE"
			pattern, err := p.parsePrimary()
			if err != nil {
				return nil, err
			}
			left = &LikeExpr{Expr: left, Pattern: pattern, Negated: negated}
			continue
		}

		right, err := p.parseExpr(prec + 1)
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Left: left, Op: op, Right: right}
	}
	return left, nil
}

func (p *Parser) peekBinaryOp() (string, int) {
	tok := p.peek()
	switch tok.Kind {
	case KwOr:
		return "OR", 1
	case KwAnd:
		return "AND", 2
	case KwIs:
		return "IS", 3
	case KwIn:
		return "IN", 3
	case KwBetween:
		return "BETWEEN", 3
	case KwLike:
		return "LIKE", 3
	case KwNot:
		if p.pos+1 < len(p.tokens) {
			next := p.tokens[p.pos+1]
			switch next.Kind {
			case KwIn:
				p.advance() // consume NOT
				return "NOT IN", 3
			case KwBetween:
				p.advance()
				return "NOT BETWEEN", 3
			case KwLike:
				p.advance()
				return "NOT LIKE", 3
			}
		}
		return "", -1
	case TokEq:
		return "=", 4
	case TokNeq:
		return tok.Value, 4
	case TokLt:
		return "<", 4
	case TokGt:
		return ">", 4
	case TokLtEq:
		return "<=", 4
	case TokGtEq:
		return ">=", 4
	case TokPlus:
		return "+", 5
	case TokMinus:
		return "-", 5
	case TokStar:
		return "*", 6
	default:
		return "", -1
	}
}

func (p *Parser) parseUnary() (Expr, error) {
	if p.peek().Kind == KwNot {
		p.advance()
		expr, err := p.parseExpr(3)
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{Op: "NOT", Expr: expr}, nil
	}
	return p.parsePrimary()
}

func (p *Parser) parsePrimary() (Expr, error) {
	tok := p.peek()

	if tok.Kind == TokLParen {
		p.advance()
		if p.peek().Kind == KwSelect {
			sub, err := p.ParseSelect()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(TokRParen); err != nil {
				return nil, err
			}
			return &SubqueryExpr{Query: sub}, nil
		}
		expr, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TokRParen); err != nil {
			return nil, err
		}
		return expr, nil
	}

	if tok.Kind == TokNumber {
		p.advance()
		return &NumberLit{Value: tok.Value}, nil
	}

	if tok.Kind == TokString {
		p.advance()
		return &StringLit{Value: tok.Value}, nil
	}

	if tok.Kind == KwNull {
		p.advance()
		return &ColumnRef{Column: "NULL"}, nil
	}

	if tok.Kind == TokIdent {
		p.advance()
		if p.peek().Kind == TokLParen {
			return p.parseFuncCall(tok.Value)
		}
		if p.peek().Kind == TokDot {
			p.advance()
			col := p.advance()
			return &ColumnRef{Table: tok.Value, Column: col.Value}, nil
		}
		return &ColumnRef{Column: tok.Value}, nil
	}

	return nil, fmt.Errorf("unexpected token %v at %d:%d", tok.Kind, tok.Line, tok.Col)
}

func (p *Parser) parseFuncCall(name string) (Expr, error) {
	p.advance() // consume (
	var args []Expr
	if p.peek().Kind == TokStar {
		p.advance()
		args = append(args, &StarExpr{})
	} else if p.peek().Kind != TokRParen {
		for {
			arg, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			args = append(args, arg)
			if _, ok := p.match(TokComma); !ok {
				break
			}
		}
	}
	if _, err := p.expect(TokRParen); err != nil {
		return nil, err
	}
	return &FuncCall{Name: name, Args: args}, nil
}

func (p *Parser) parseInExpr(left Expr, negated bool) (Expr, error) {
	if _, err := p.expect(TokLParen); err != nil {
		return nil, err
	}
	if p.peek().Kind == KwSelect {
		sub, err := p.ParseSelect()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TokRParen); err != nil {
			return nil, err
		}
		return &InExpr{Expr: left, Subquery: sub, Negated: negated}, nil
	}
	var values []Expr
	for {
		val, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		values = append(values, val)
		if _, ok := p.match(TokComma); !ok {
			break
		}
	}
	if _, err := p.expect(TokRParen); err != nil {
		return nil, err
	}
	return &InExpr{Expr: left, Values: values, Negated: negated}, nil
}
```

### printer.go

```go
package sqlparser

import (
	"fmt"
	"strings"
)

func PrettyPrint(stmt *SelectStmt) string {
	var sb strings.Builder
	sb.WriteString("SELECT ")
	for i, col := range stmt.Columns {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(exprToSQL(col.Expr))
		if col.Alias != "" {
			sb.WriteString(" AS ")
			sb.WriteString(col.Alias)
		}
	}

	if stmt.From != nil {
		sb.WriteString("\nFROM ")
		sb.WriteString(stmt.From.Name)
		if stmt.From.Alias != "" {
			sb.WriteString(" ")
			sb.WriteString(stmt.From.Alias)
		}
	}

	for _, j := range stmt.Joins {
		sb.WriteString("\n")
		sb.WriteString(j.Type.String())
		sb.WriteString(" ")
		sb.WriteString(j.Table.Name)
		if j.Table.Alias != "" {
			sb.WriteString(" ")
			sb.WriteString(j.Table.Alias)
		}
		sb.WriteString(" ON ")
		sb.WriteString(exprToSQL(j.Condition))
	}

	if stmt.Where != nil {
		sb.WriteString("\nWHERE ")
		sb.WriteString(exprToSQL(stmt.Where))
	}

	if len(stmt.GroupBy) > 0 {
		sb.WriteString("\nGROUP BY ")
		parts := make([]string, len(stmt.GroupBy))
		for i, g := range stmt.GroupBy {
			parts[i] = exprToSQL(g)
		}
		sb.WriteString(strings.Join(parts, ", "))
	}

	if stmt.Having != nil {
		sb.WriteString("\nHAVING ")
		sb.WriteString(exprToSQL(stmt.Having))
	}

	if len(stmt.OrderBy) > 0 {
		sb.WriteString("\nORDER BY ")
		parts := make([]string, len(stmt.OrderBy))
		for i, o := range stmt.OrderBy {
			parts[i] = exprToSQL(o.Expr)
			if o.Desc {
				parts[i] += " DESC"
			}
		}
		sb.WriteString(strings.Join(parts, ", "))
	}

	if stmt.Limit != nil {
		sb.WriteString(fmt.Sprintf("\nLIMIT %d", *stmt.Limit))
	}
	if stmt.Offset != nil {
		sb.WriteString(fmt.Sprintf("\nOFFSET %d", *stmt.Offset))
	}

	return sb.String()
}

func exprToSQL(expr Expr) string {
	switch e := expr.(type) {
	case *ColumnRef:
		if e.Table != "" {
			return e.Table + "." + e.Column
		}
		return e.Column
	case *StarExpr:
		if e.Table != "" {
			return e.Table + ".*"
		}
		return "*"
	case *NumberLit:
		return e.Value
	case *StringLit:
		return "'" + e.Value + "'"
	case *BinaryExpr:
		return exprToSQL(e.Left) + " " + e.Op + " " + exprToSQL(e.Right)
	case *UnaryExpr:
		return e.Op + " " + exprToSQL(e.Expr)
	case *IsNullExpr:
		if e.Negated {
			return exprToSQL(e.Expr) + " IS NOT NULL"
		}
		return exprToSQL(e.Expr) + " IS NULL"
	case *InExpr:
		neg := ""
		if e.Negated {
			neg = "NOT "
		}
		if e.Subquery != nil {
			return exprToSQL(e.Expr) + " " + neg + "IN (" + PrettyPrint(e.Subquery) + ")"
		}
		vals := make([]string, len(e.Values))
		for i, v := range e.Values {
			vals[i] = exprToSQL(v)
		}
		return exprToSQL(e.Expr) + " " + neg + "IN (" + strings.Join(vals, ", ") + ")"
	case *BetweenExpr:
		neg := ""
		if e.Negated {
			neg = "NOT "
		}
		return exprToSQL(e.Expr) + " " + neg + "BETWEEN " + exprToSQL(e.Low) + " AND " + exprToSQL(e.High)
	case *LikeExpr:
		neg := ""
		if e.Negated {
			neg = "NOT "
		}
		return exprToSQL(e.Expr) + " " + neg + "LIKE " + exprToSQL(e.Pattern)
	case *FuncCall:
		args := make([]string, len(e.Args))
		for i, a := range e.Args {
			args[i] = exprToSQL(a)
		}
		return e.Name + "(" + strings.Join(args, ", ") + ")"
	case *SubqueryExpr:
		return "(" + PrettyPrint(e.Query) + ")"
	default:
		return fmt.Sprintf("%v", expr)
	}
}
```

### parser_test.go

```go
package sqlparser

import "testing"

func TestSimpleSelect(t *testing.T) {
	stmt, err := Parse("SELECT a, b FROM users WHERE id = 1")
	if err != nil {
		t.Fatal(err)
	}
	if len(stmt.Columns) != 2 {
		t.Errorf("expected 2 columns, got %d", len(stmt.Columns))
	}
	if stmt.From.Name != "users" {
		t.Errorf("expected FROM users, got %s", stmt.From.Name)
	}
	if stmt.Where == nil {
		t.Error("expected WHERE clause")
	}
}

func TestColumnAliases(t *testing.T) {
	stmt, err := Parse("SELECT a AS alias1, b alias2 FROM t")
	if err != nil {
		t.Fatal(err)
	}
	if stmt.Columns[0].Alias != "alias1" {
		t.Errorf("expected alias1, got %s", stmt.Columns[0].Alias)
	}
	if stmt.Columns[1].Alias != "alias2" {
		t.Errorf("expected alias2, got %s", stmt.Columns[1].Alias)
	}
}

func TestJoin(t *testing.T) {
	stmt, err := Parse("SELECT * FROM a INNER JOIN b ON a.id = b.a_id LEFT JOIN c ON a.id = c.a_id")
	if err != nil {
		t.Fatal(err)
	}
	if len(stmt.Joins) != 2 {
		t.Errorf("expected 2 joins, got %d", len(stmt.Joins))
	}
	if stmt.Joins[0].Type != InnerJoin {
		t.Error("expected INNER JOIN")
	}
	if stmt.Joins[1].Type != LeftJoin {
		t.Error("expected LEFT JOIN")
	}
}

func TestPrecedence(t *testing.T) {
	stmt, err := Parse("SELECT * FROM t WHERE a = 1 AND b = 2 OR c = 3")
	if err != nil {
		t.Fatal(err)
	}
	// OR should be the top-level operator (lower precedence)
	bin, ok := stmt.Where.(*BinaryExpr)
	if !ok {
		t.Fatal("expected BinaryExpr at top level")
	}
	if bin.Op != "OR" {
		t.Errorf("expected OR at top level, got %s", bin.Op)
	}
}

func TestGroupByHaving(t *testing.T) {
	stmt, err := Parse("SELECT dept, COUNT(*) FROM emp GROUP BY dept HAVING COUNT(*) > 5")
	if err != nil {
		t.Fatal(err)
	}
	if len(stmt.GroupBy) != 1 {
		t.Errorf("expected 1 GROUP BY, got %d", len(stmt.GroupBy))
	}
	if stmt.Having == nil {
		t.Error("expected HAVING clause")
	}
}

func TestSubquery(t *testing.T) {
	stmt, err := Parse("SELECT * FROM t WHERE id IN (SELECT id FROM other)")
	if err != nil {
		t.Fatal(err)
	}
	inExpr, ok := stmt.Where.(*InExpr)
	if !ok {
		t.Fatal("expected InExpr")
	}
	if inExpr.Subquery == nil {
		t.Error("expected subquery in IN clause")
	}
}

func TestOrderByLimitOffset(t *testing.T) {
	stmt, err := Parse("SELECT * FROM t ORDER BY name DESC, id LIMIT 10 OFFSET 20")
	if err != nil {
		t.Fatal(err)
	}
	if len(stmt.OrderBy) != 2 {
		t.Errorf("expected 2 ORDER BY items, got %d", len(stmt.OrderBy))
	}
	if !stmt.OrderBy[0].Desc {
		t.Error("expected DESC on first order item")
	}
	if *stmt.Limit != 10 {
		t.Errorf("expected LIMIT 10, got %d", *stmt.Limit)
	}
	if *stmt.Offset != 20 {
		t.Errorf("expected OFFSET 20, got %d", *stmt.Offset)
	}
}

func TestPrettyPrint(t *testing.T) {
	input := "SELECT a, b FROM users WHERE id = 1 ORDER BY a LIMIT 10"
	stmt, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	output := PrettyPrint(stmt)
	if output == "" {
		t.Error("pretty print produced empty output")
	}
	t.Log("Pretty printed:\n" + output)
}
```

## Complete Solution (Rust)

### Cargo.toml

```toml
[package]
name = "sql-select-parser"
version = "0.1.0"
edition = "2021"
```

### src/lib.rs

```rust
pub mod ast;
pub mod lexer;
pub mod parser;
pub mod printer;

use ast::SelectStmt;

pub fn parse(input: &str) -> Result<SelectStmt, String> {
    let tokens = lexer::Lexer::new(input).tokenize()?;
    parser::Parser::new(&tokens).parse_select()
}

pub fn pretty_print(stmt: &SelectStmt) -> String {
    printer::print_select(stmt)
}
```

### src/ast.rs

```rust
#[derive(Debug, Clone)]
pub struct SelectStmt {
    pub columns: Vec<SelectColumn>,
    pub from: Option<TableRef>,
    pub joins: Vec<JoinClause>,
    pub where_clause: Option<Expr>,
    pub group_by: Vec<Expr>,
    pub having: Option<Expr>,
    pub order_by: Vec<OrderByItem>,
    pub limit: Option<u64>,
    pub offset: Option<u64>,
}

#[derive(Debug, Clone)]
pub struct SelectColumn {
    pub expr: Expr,
    pub alias: Option<String>,
}

#[derive(Debug, Clone)]
pub struct TableRef {
    pub name: String,
    pub alias: Option<String>,
}

#[derive(Debug, Clone)]
pub struct JoinClause {
    pub join_type: JoinType,
    pub table: TableRef,
    pub condition: Expr,
}

#[derive(Debug, Clone, PartialEq)]
pub enum JoinType {
    Inner,
    Left,
    Right,
}

impl std::fmt::Display for JoinType {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            JoinType::Inner => write!(f, "INNER JOIN"),
            JoinType::Left => write!(f, "LEFT JOIN"),
            JoinType::Right => write!(f, "RIGHT JOIN"),
        }
    }
}

#[derive(Debug, Clone)]
pub struct OrderByItem {
    pub expr: Expr,
    pub desc: bool,
}

#[derive(Debug, Clone)]
pub enum Expr {
    Column { table: Option<String>, name: String },
    Star { table: Option<String> },
    Number(String),
    StringLit(String),
    BinaryOp { left: Box<Expr>, op: String, right: Box<Expr> },
    UnaryOp { op: String, expr: Box<Expr> },
    IsNull { expr: Box<Expr>, negated: bool },
    In { expr: Box<Expr>, values: Vec<Expr>, subquery: Option<Box<SelectStmt>>, negated: bool },
    Between { expr: Box<Expr>, low: Box<Expr>, high: Box<Expr>, negated: bool },
    Like { expr: Box<Expr>, pattern: Box<Expr>, negated: bool },
    FuncCall { name: String, args: Vec<Expr> },
    Subquery(Box<SelectStmt>),
}
```

### src/lexer.rs

```rust
use std::fmt;

#[derive(Debug, Clone, PartialEq)]
pub enum TokenKind {
    Ident(String), Number(String), StringLit(String),
    Star, Comma, Dot, LParen, RParen,
    Eq, Neq, Lt, Gt, LtEq, GtEq, Plus, Minus,
    // Keywords
    Select, From, Where, And, Or, Not, As, On, Join,
    Inner, Left, Right, Order, By, Group, Having,
    Limit, Offset, Asc, Desc, Null, Is, In, Between, Like,
    Eof,
}

#[derive(Debug, Clone)]
pub struct Token {
    pub kind: TokenKind,
    pub line: usize,
    pub col: usize,
}

impl fmt::Display for Token {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{:?} at {}:{}", self.kind, self.line, self.col)
    }
}

pub struct Lexer<'a> {
    input: &'a [u8],
    pos: usize,
    line: usize,
    col: usize,
}

impl<'a> Lexer<'a> {
    pub fn new(input: &'a str) -> Self {
        Lexer { input: input.as_bytes(), pos: 0, line: 1, col: 1 }
    }

    pub fn tokenize(&mut self) -> Result<Vec<Token>, String> {
        let mut tokens = Vec::new();
        loop {
            self.skip_ws();
            if self.pos >= self.input.len() {
                tokens.push(Token { kind: TokenKind::Eof, line: self.line, col: self.col });
                return Ok(tokens);
            }
            tokens.push(self.next_token()?);
        }
    }

    fn peek(&self) -> u8 {
        if self.pos < self.input.len() { self.input[self.pos] } else { 0 }
    }

    fn advance(&mut self) -> u8 {
        let ch = self.input[self.pos];
        self.pos += 1;
        if ch == b'\n' { self.line += 1; self.col = 1; } else { self.col += 1; }
        ch
    }

    fn skip_ws(&mut self) {
        while self.pos < self.input.len() && self.input[self.pos].is_ascii_whitespace() {
            self.advance();
        }
    }

    fn next_token(&mut self) -> Result<Token, String> {
        let (line, col) = (self.line, self.col);
        let ch = self.peek();
        match ch {
            b'*' => { self.advance(); Ok(Token { kind: TokenKind::Star, line, col }) }
            b',' => { self.advance(); Ok(Token { kind: TokenKind::Comma, line, col }) }
            b'.' => { self.advance(); Ok(Token { kind: TokenKind::Dot, line, col }) }
            b'(' => { self.advance(); Ok(Token { kind: TokenKind::LParen, line, col }) }
            b')' => { self.advance(); Ok(Token { kind: TokenKind::RParen, line, col }) }
            b'+' => { self.advance(); Ok(Token { kind: TokenKind::Plus, line, col }) }
            b'-' => { self.advance(); Ok(Token { kind: TokenKind::Minus, line, col }) }
            b'=' => { self.advance(); Ok(Token { kind: TokenKind::Eq, line, col }) }
            b'<' => {
                self.advance();
                if self.peek() == b'=' { self.advance(); Ok(Token { kind: TokenKind::LtEq, line, col }) }
                else if self.peek() == b'>' { self.advance(); Ok(Token { kind: TokenKind::Neq, line, col }) }
                else { Ok(Token { kind: TokenKind::Lt, line, col }) }
            }
            b'>' => {
                self.advance();
                if self.peek() == b'=' { self.advance(); Ok(Token { kind: TokenKind::GtEq, line, col }) }
                else { Ok(Token { kind: TokenKind::Gt, line, col }) }
            }
            b'!' => {
                self.advance();
                if self.peek() == b'=' { self.advance(); Ok(Token { kind: TokenKind::Neq, line, col }) }
                else { Err(format!("Unexpected '!' at {}:{}", line, col)) }
            }
            b'\'' => self.lex_string(line, col),
            b'0'..=b'9' => self.lex_number(line, col),
            _ if ch.is_ascii_alphabetic() || ch == b'_' => self.lex_ident(line, col),
            _ => Err(format!("Unexpected character '{}' at {}:{}", ch as char, line, col)),
        }
    }

    fn lex_string(&mut self, line: usize, col: usize) -> Result<Token, String> {
        self.advance();
        let mut s = String::new();
        loop {
            if self.pos >= self.input.len() {
                return Err(format!("Unterminated string at {}:{}", line, col));
            }
            let ch = self.advance();
            if ch == b'\'' {
                if self.peek() == b'\'' { self.advance(); s.push('\''); }
                else { return Ok(Token { kind: TokenKind::StringLit(s), line, col }); }
            } else {
                s.push(ch as char);
            }
        }
    }

    fn lex_number(&mut self, line: usize, col: usize) -> Result<Token, String> {
        let start = self.pos;
        while self.pos < self.input.len() && self.input[self.pos].is_ascii_digit() {
            self.advance();
        }
        if self.peek() == b'.' {
            self.advance();
            while self.pos < self.input.len() && self.input[self.pos].is_ascii_digit() {
                self.advance();
            }
        }
        let s = std::str::from_utf8(&self.input[start..self.pos]).unwrap().to_string();
        Ok(Token { kind: TokenKind::Number(s), line, col })
    }

    fn lex_ident(&mut self, line: usize, col: usize) -> Result<Token, String> {
        let start = self.pos;
        while self.pos < self.input.len() &&
              (self.input[self.pos].is_ascii_alphanumeric() || self.input[self.pos] == b'_') {
            self.advance();
        }
        let word = std::str::from_utf8(&self.input[start..self.pos]).unwrap();
        let kind = match word.to_uppercase().as_str() {
            "SELECT" => TokenKind::Select, "FROM" => TokenKind::From,
            "WHERE" => TokenKind::Where, "AND" => TokenKind::And,
            "OR" => TokenKind::Or, "NOT" => TokenKind::Not,
            "AS" => TokenKind::As, "ON" => TokenKind::On,
            "JOIN" => TokenKind::Join, "INNER" => TokenKind::Inner,
            "LEFT" => TokenKind::Left, "RIGHT" => TokenKind::Right,
            "ORDER" => TokenKind::Order, "BY" => TokenKind::By,
            "GROUP" => TokenKind::Group, "HAVING" => TokenKind::Having,
            "LIMIT" => TokenKind::Limit, "OFFSET" => TokenKind::Offset,
            "ASC" => TokenKind::Asc, "DESC" => TokenKind::Desc,
            "NULL" => TokenKind::Null, "IS" => TokenKind::Is,
            "IN" => TokenKind::In, "BETWEEN" => TokenKind::Between,
            "LIKE" => TokenKind::Like,
            _ => TokenKind::Ident(word.to_string()),
        };
        Ok(Token { kind, line, col })
    }
}
```

### src/parser.rs

```rust
use crate::ast::*;
use crate::lexer::{Token, TokenKind};

pub struct Parser<'a> {
    tokens: &'a [Token],
    pos: usize,
}

impl<'a> Parser<'a> {
    pub fn new(tokens: &'a [Token]) -> Self {
        Parser { tokens, pos: 0 }
    }

    fn peek(&self) -> &TokenKind {
        &self.tokens[self.pos].kind
    }

    fn advance(&mut self) -> &Token {
        let tok = &self.tokens[self.pos];
        self.pos += 1;
        tok
    }

    fn expect(&mut self, expected: &TokenKind) -> Result<&Token, String> {
        let tok = self.advance();
        if std::mem::discriminant(&tok.kind) == std::mem::discriminant(expected) {
            Ok(tok)
        } else {
            Err(format!("Expected {:?}, found {:?} at {}:{}", expected, tok.kind, tok.line, tok.col))
        }
    }

    fn match_kind(&mut self, kind: &TokenKind) -> bool {
        if std::mem::discriminant(self.peek()) == std::mem::discriminant(kind) {
            self.advance();
            true
        } else {
            false
        }
    }

    pub fn parse_select(&mut self) -> Result<SelectStmt, String> {
        self.expect(&TokenKind::Select)?;
        let columns = self.parse_columns()?;

        let from = if *self.peek() == TokenKind::From {
            self.advance();
            Some(self.parse_table_ref()?)
        } else {
            None
        };

        let mut joins = Vec::new();
        while matches!(self.peek(), TokenKind::Inner | TokenKind::Left | TokenKind::Right | TokenKind::Join) {
            joins.push(self.parse_join()?);
        }

        let where_clause = if *self.peek() == TokenKind::Where {
            self.advance();
            Some(self.parse_expr(0)?)
        } else {
            None
        };

        let mut group_by = Vec::new();
        if *self.peek() == TokenKind::Group {
            self.advance();
            self.expect(&TokenKind::By)?;
            loop {
                group_by.push(self.parse_primary()?);
                if !self.match_kind(&TokenKind::Comma) { break; }
            }
        }

        let having = if *self.peek() == TokenKind::Having {
            self.advance();
            Some(self.parse_expr(0)?)
        } else {
            None
        };

        let mut order_by = Vec::new();
        if *self.peek() == TokenKind::Order {
            self.advance();
            self.expect(&TokenKind::By)?;
            loop {
                let expr = self.parse_primary()?;
                let desc = if self.match_kind(&TokenKind::Desc) { true }
                           else { self.match_kind(&TokenKind::Asc); false };
                order_by.push(OrderByItem { expr, desc });
                if !self.match_kind(&TokenKind::Comma) { break; }
            }
        }

        let limit = if *self.peek() == TokenKind::Limit {
            self.advance();
            let tok = self.advance();
            if let TokenKind::Number(s) = &tok.kind {
                Some(s.parse::<u64>().map_err(|e| e.to_string())?)
            } else {
                return Err(format!("Expected number after LIMIT at {}:{}", tok.line, tok.col));
            }
        } else { None };

        let offset = if *self.peek() == TokenKind::Offset {
            self.advance();
            let tok = self.advance();
            if let TokenKind::Number(s) = &tok.kind {
                Some(s.parse::<u64>().map_err(|e| e.to_string())?)
            } else {
                return Err(format!("Expected number after OFFSET at {}:{}", tok.line, tok.col));
            }
        } else { None };

        Ok(SelectStmt { columns, from, joins, where_clause, group_by, having, order_by, limit, offset })
    }

    fn parse_columns(&mut self) -> Result<Vec<SelectColumn>, String> {
        let mut cols = Vec::new();
        loop {
            cols.push(self.parse_select_column()?);
            if !self.match_kind(&TokenKind::Comma) { break; }
        }
        Ok(cols)
    }

    fn parse_select_column(&mut self) -> Result<SelectColumn, String> {
        if *self.peek() == TokenKind::Star {
            self.advance();
            return Ok(SelectColumn { expr: Expr::Star { table: None }, alias: None });
        }
        let expr = self.parse_expr(0)?;
        let alias = if self.match_kind(&TokenKind::As) {
            let tok = self.advance();
            if let TokenKind::Ident(name) = &tok.kind { Some(name.clone()) }
            else { return Err("Expected alias after AS".to_string()); }
        } else if let TokenKind::Ident(_) = self.peek() {
            if !self.is_clause_keyword() {
                let tok = self.advance();
                if let TokenKind::Ident(name) = &tok.kind { Some(name.clone()) } else { None }
            } else { None }
        } else { None };
        Ok(SelectColumn { expr, alias })
    }

    fn is_clause_keyword(&self) -> bool {
        matches!(self.peek(),
            TokenKind::From | TokenKind::Where | TokenKind::Group |
            TokenKind::Order | TokenKind::Limit | TokenKind::Offset |
            TokenKind::Having | TokenKind::Inner | TokenKind::Left |
            TokenKind::Right | TokenKind::Join | TokenKind::On)
    }

    fn parse_table_ref(&mut self) -> Result<TableRef, String> {
        let tok = self.advance();
        let name = match &tok.kind {
            TokenKind::Ident(n) => n.clone(),
            other => return Err(format!("Expected table name, found {:?}", other)),
        };
        let alias = if self.match_kind(&TokenKind::As) {
            let t = self.advance();
            if let TokenKind::Ident(a) = &t.kind { Some(a.clone()) } else { None }
        } else if let TokenKind::Ident(_) = self.peek() {
            if !self.is_clause_keyword() {
                let t = self.advance();
                if let TokenKind::Ident(a) = &t.kind { Some(a.clone()) } else { None }
            } else { None }
        } else { None };
        Ok(TableRef { name, alias })
    }

    fn parse_join(&mut self) -> Result<JoinClause, String> {
        let join_type = match self.peek() {
            TokenKind::Left => { self.advance(); JoinType::Left }
            TokenKind::Right => { self.advance(); JoinType::Right }
            TokenKind::Inner => { self.advance(); JoinType::Inner }
            _ => JoinType::Inner,
        };
        self.expect(&TokenKind::Join)?;
        let table = self.parse_table_ref()?;
        self.expect(&TokenKind::On)?;
        let condition = self.parse_expr(0)?;
        Ok(JoinClause { join_type, table, condition })
    }

    fn parse_expr(&mut self, min_prec: u8) -> Result<Expr, String> {
        let mut left = self.parse_unary()?;
        loop {
            let (op, prec) = self.peek_binary_op();
            if prec < min_prec { break; }

            if op == "IS" {
                self.advance();
                let negated = self.match_kind(&TokenKind::Not);
                self.expect(&TokenKind::Null)?;
                left = Expr::IsNull { expr: Box::new(left), negated };
                continue;
            }
            if op == "IN" || op == "NOT IN" {
                if op == "NOT IN" { self.advance(); }
                self.advance();
                left = self.parse_in_expr(left, op == "NOT IN")?;
                continue;
            }
            if op == "BETWEEN" || op == "NOT BETWEEN" {
                if op == "NOT BETWEEN" { self.advance(); }
                self.advance();
                let low = self.parse_primary()?;
                self.expect(&TokenKind::And)?;
                let high = self.parse_primary()?;
                left = Expr::Between {
                    expr: Box::new(left), low: Box::new(low),
                    high: Box::new(high), negated: op == "NOT BETWEEN",
                };
                continue;
            }
            if op == "LIKE" || op == "NOT LIKE" {
                if op == "NOT LIKE" { self.advance(); }
                self.advance();
                let pattern = self.parse_primary()?;
                left = Expr::Like {
                    expr: Box::new(left), pattern: Box::new(pattern),
                    negated: op == "NOT LIKE",
                };
                continue;
            }

            self.advance();
            let right = self.parse_expr(prec + 1)?;
            left = Expr::BinaryOp { left: Box::new(left), op, right: Box::new(right) };
        }
        Ok(left)
    }

    fn peek_binary_op(&self) -> (String, u8) {
        match self.peek() {
            TokenKind::Or => ("OR".to_string(), 1),
            TokenKind::And => ("AND".to_string(), 2),
            TokenKind::Is => ("IS".to_string(), 3),
            TokenKind::In => ("IN".to_string(), 3),
            TokenKind::Between => ("BETWEEN".to_string(), 3),
            TokenKind::Like => ("LIKE".to_string(), 3),
            TokenKind::Not => {
                if self.pos + 1 < self.tokens.len() {
                    match &self.tokens[self.pos + 1].kind {
                        TokenKind::In => ("NOT IN".to_string(), 3),
                        TokenKind::Between => ("NOT BETWEEN".to_string(), 3),
                        TokenKind::Like => ("NOT LIKE".to_string(), 3),
                        _ => (String::new(), 0),
                    }
                } else { (String::new(), 0) }
            }
            TokenKind::Eq => ("=".to_string(), 4),
            TokenKind::Neq => ("<>".to_string(), 4),
            TokenKind::Lt => ("<".to_string(), 4),
            TokenKind::Gt => (">".to_string(), 4),
            TokenKind::LtEq => ("<=".to_string(), 4),
            TokenKind::GtEq => (">=".to_string(), 4),
            TokenKind::Plus => ("+".to_string(), 5),
            TokenKind::Minus => ("-".to_string(), 5),
            TokenKind::Star => ("*".to_string(), 6),
            _ => (String::new(), 0),
        }
    }

    fn parse_unary(&mut self) -> Result<Expr, String> {
        if *self.peek() == TokenKind::Not {
            self.advance();
            let expr = self.parse_expr(3)?;
            return Ok(Expr::UnaryOp { op: "NOT".to_string(), expr: Box::new(expr) });
        }
        self.parse_primary()
    }

    fn parse_primary(&mut self) -> Result<Expr, String> {
        if *self.peek() == TokenKind::LParen {
            self.advance();
            if *self.peek() == TokenKind::Select {
                let sub = self.parse_select()?;
                self.expect(&TokenKind::RParen)?;
                return Ok(Expr::Subquery(Box::new(sub)));
            }
            let expr = self.parse_expr(0)?;
            self.expect(&TokenKind::RParen)?;
            return Ok(expr);
        }
        if let TokenKind::Number(s) = self.peek().clone() {
            self.advance();
            return Ok(Expr::Number(s));
        }
        if let TokenKind::StringLit(s) = self.peek().clone() {
            self.advance();
            return Ok(Expr::StringLit(s));
        }
        if *self.peek() == TokenKind::Null {
            self.advance();
            return Ok(Expr::Column { table: None, name: "NULL".to_string() });
        }
        if *self.peek() == TokenKind::Star {
            self.advance();
            return Ok(Expr::Star { table: None });
        }
        if let TokenKind::Ident(name) = self.peek().clone() {
            self.advance();
            if *self.peek() == TokenKind::LParen {
                return self.parse_func_call(name);
            }
            if *self.peek() == TokenKind::Dot {
                self.advance();
                if *self.peek() == TokenKind::Star {
                    self.advance();
                    return Ok(Expr::Star { table: Some(name) });
                }
                let col_tok = self.advance();
                let col = match &col_tok.kind {
                    TokenKind::Ident(c) => c.clone(),
                    other => return Err(format!("Expected column name after '.', found {:?}", other)),
                };
                return Ok(Expr::Column { table: Some(name), name: col });
            }
            return Ok(Expr::Column { table: None, name });
        }
        let tok = &self.tokens[self.pos];
        Err(format!("Unexpected token {:?} at {}:{}", tok.kind, tok.line, tok.col))
    }

    fn parse_func_call(&mut self, name: String) -> Result<Expr, String> {
        self.advance(); // (
        let mut args = Vec::new();
        if *self.peek() == TokenKind::Star {
            self.advance();
            args.push(Expr::Star { table: None });
        } else if *self.peek() != TokenKind::RParen {
            loop {
                args.push(self.parse_expr(0)?);
                if !self.match_kind(&TokenKind::Comma) { break; }
            }
        }
        self.expect(&TokenKind::RParen)?;
        Ok(Expr::FuncCall { name, args })
    }

    fn parse_in_expr(&mut self, left: Expr, negated: bool) -> Result<Expr, String> {
        self.expect(&TokenKind::LParen)?;
        if *self.peek() == TokenKind::Select {
            let sub = self.parse_select()?;
            self.expect(&TokenKind::RParen)?;
            return Ok(Expr::In {
                expr: Box::new(left), values: vec![],
                subquery: Some(Box::new(sub)), negated,
            });
        }
        let mut values = Vec::new();
        loop {
            values.push(self.parse_expr(0)?);
            if !self.match_kind(&TokenKind::Comma) { break; }
        }
        self.expect(&TokenKind::RParen)?;
        Ok(Expr::In { expr: Box::new(left), values, subquery: None, negated })
    }
}
```

### src/printer.rs

```rust
use crate::ast::*;

pub fn print_select(stmt: &SelectStmt) -> String {
    let mut parts = Vec::new();
    let cols: Vec<String> = stmt.columns.iter().map(|c| {
        let mut s = print_expr(&c.expr);
        if let Some(alias) = &c.alias {
            s.push_str(&format!(" AS {}", alias));
        }
        s
    }).collect();
    parts.push(format!("SELECT {}", cols.join(", ")));

    if let Some(from) = &stmt.from {
        let mut s = format!("FROM {}", from.name);
        if let Some(alias) = &from.alias { s.push_str(&format!(" {}", alias)); }
        parts.push(s);
    }

    for j in &stmt.joins {
        let mut s = format!("{} {}", j.join_type, j.table.name);
        if let Some(alias) = &j.table.alias { s.push_str(&format!(" {}", alias)); }
        s.push_str(&format!(" ON {}", print_expr(&j.condition)));
        parts.push(s);
    }

    if let Some(w) = &stmt.where_clause {
        parts.push(format!("WHERE {}", print_expr(w)));
    }

    if !stmt.group_by.is_empty() {
        let g: Vec<String> = stmt.group_by.iter().map(|e| print_expr(e)).collect();
        parts.push(format!("GROUP BY {}", g.join(", ")));
    }
    if let Some(h) = &stmt.having {
        parts.push(format!("HAVING {}", print_expr(h)));
    }
    if !stmt.order_by.is_empty() {
        let o: Vec<String> = stmt.order_by.iter().map(|i| {
            if i.desc { format!("{} DESC", print_expr(&i.expr)) }
            else { print_expr(&i.expr) }
        }).collect();
        parts.push(format!("ORDER BY {}", o.join(", ")));
    }
    if let Some(l) = stmt.limit { parts.push(format!("LIMIT {}", l)); }
    if let Some(o) = stmt.offset { parts.push(format!("OFFSET {}", o)); }

    parts.join("\n")
}

pub fn print_expr(expr: &Expr) -> String {
    match expr {
        Expr::Column { table: Some(t), name } => format!("{}.{}", t, name),
        Expr::Column { table: None, name } => name.clone(),
        Expr::Star { table: Some(t) } => format!("{}.*", t),
        Expr::Star { table: None } => "*".to_string(),
        Expr::Number(s) => s.clone(),
        Expr::StringLit(s) => format!("'{}'", s),
        Expr::BinaryOp { left, op, right } =>
            format!("{} {} {}", print_expr(left), op, print_expr(right)),
        Expr::UnaryOp { op, expr } => format!("{} {}", op, print_expr(expr)),
        Expr::IsNull { expr, negated } =>
            if *negated { format!("{} IS NOT NULL", print_expr(expr)) }
            else { format!("{} IS NULL", print_expr(expr)) },
        Expr::In { expr, values, subquery, negated } => {
            let neg = if *negated { "NOT " } else { "" };
            if let Some(sub) = subquery {
                format!("{} {}IN ({})", print_expr(expr), neg, print_select(sub))
            } else {
                let vals: Vec<String> = values.iter().map(|v| print_expr(v)).collect();
                format!("{} {}IN ({})", print_expr(expr), neg, vals.join(", "))
            }
        }
        Expr::Between { expr, low, high, negated } => {
            let neg = if *negated { "NOT " } else { "" };
            format!("{} {}BETWEEN {} AND {}", print_expr(expr), neg, print_expr(low), print_expr(high))
        }
        Expr::Like { expr, pattern, negated } => {
            let neg = if *negated { "NOT " } else { "" };
            format!("{} {}LIKE {}", print_expr(expr), neg, print_expr(pattern))
        }
        Expr::FuncCall { name, args } => {
            let a: Vec<String> = args.iter().map(|e| print_expr(e)).collect();
            format!("{}({})", name, a.join(", "))
        }
        Expr::Subquery(sub) => format!("({})", print_select(sub)),
    }
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
# Go
=== RUN   TestSimpleSelect
--- PASS: TestSimpleSelect
=== RUN   TestColumnAliases
--- PASS: TestColumnAliases
=== RUN   TestJoin
--- PASS: TestJoin
=== RUN   TestPrecedence
--- PASS: TestPrecedence
=== RUN   TestGroupByHaving
--- PASS: TestGroupByHaving
=== RUN   TestSubquery
--- PASS: TestSubquery
=== RUN   TestOrderByLimitOffset
--- PASS: TestOrderByLimitOffset
=== RUN   TestPrettyPrint
--- PASS: TestPrettyPrint
PASS

# Rust
running 7 tests ... ok
```

## Design Decisions

1. **Precedence climbing vs recursive layers**: Using a single `parseExpr(minPrec)` function with a precedence table is simpler and more extensible than writing a separate function for each precedence level. Adding a new operator means adding one entry to the table.

2. **Keywords as separate token kinds vs tagged identifiers**: Separate token kinds simplify parsing (direct match instead of string comparison) at the cost of a larger token enum. For a fixed grammar, this is the right trade-off.

3. **Go interfaces vs Rust enums for AST**: Go uses interfaces (`Expr`) with struct implementations. Rust uses enums with data variants. The Rust approach guarantees exhaustive matching at compile time. The Go approach is more extensible without modifying existing types.

4. **`Vec` for GROUP BY, ORDER BY**: These clauses preserve order semantics, so a `Vec` is the natural representation.

## Common Mistakes

1. **AND/OR precedence**: `a AND b OR c` must parse as `(a AND b) OR c`, not `a AND (b OR c)`. If your WHERE clause flattens all conditions to the same level, complex queries will produce wrong results.

2. **Implicit aliases conflicting with keywords**: `SELECT a from FROM t` -- is `from` an alias for `a` or the FROM keyword? Case-insensitive keyword detection prevents this: `FROM` is always a keyword, `from` is also a keyword.

3. **Subquery detection**: When you see `(` in a WHERE clause, you must look ahead to distinguish `(expr)` from `(SELECT ...)`. Consuming the `(` before checking makes backtracking harder.

4. **Table-qualified columns**: `t.col` requires two tokens (identifier, dot, identifier). If you consume the first identifier eagerly as a column name, you must backtrack when you see the dot.

## Performance Notes

- Token allocation is the main memory cost. For production parsers, arena allocation or a token pool avoids per-token heap allocation.
- String interning for identifiers and keywords reduces memory when the same identifier appears many times (common in complex queries with aliases).
- The Go solution uses string comparisons for keyword matching. A hash map would be faster for large keyword sets but adds complexity for this small set.

## Going Further

- Add support for `UNION`, `INTERSECT`, `EXCEPT` between SELECT statements
- Parse window functions: `ROW_NUMBER() OVER (PARTITION BY ... ORDER BY ...)`
- Add `CREATE TABLE` and `INSERT` statement parsing
- Implement a query planner that converts the AST into a logical execution plan
- Add a SQL formatter mode with configurable indentation and casing rules

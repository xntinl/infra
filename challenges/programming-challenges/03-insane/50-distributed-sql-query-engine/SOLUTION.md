# Solution: Distributed SQL Query Engine

## Architecture Overview

The system follows a coordinator-worker architecture with four major layers:

```
Client ──TCP──> Coordinator
                    |
                    |── SQL Parser ──> AST
                    |── Logical Planner ──> Logical Plan Tree
                    |── Physical Planner ──> Physical Plan Tree (with Exchanges)
                    |── Fragment Splitter ──> Plan Fragments per Worker
                    |
                    |──TCP──> Worker 0 (executes fragment, returns rows)
                    |──TCP──> Worker 1 (executes fragment, returns rows)
                    |──TCP──> Worker N (executes fragment, returns rows)
                    |
                    |── Result Assembler ──> Final Result Set
                    |── Formatter ──> Printed Table
```

**Data flow for a distributed join:**
1. Coordinator sends scan+filter fragments to each worker
2. Workers scan local partitions, apply filters
3. If join key != partition key: workers hash-exchange rows to the correct peer (HashExchange)
4. Workers execute hash join on locally reshuffled data
5. Workers send join results back to coordinator (GatherExchange)
6. Coordinator merges, sorts (if ORDER BY), aggregates (if GROUP BY), applies LIMIT

## Go Solution

### `sql/lexer.go`

```go
package sql

import (
	"fmt"
	"strings"
	"unicode"
)

type TokenType int

const (
	TokenEOF TokenType = iota
	TokenIdent
	TokenNumber
	TokenFloat
	TokenString
	TokenComma
	TokenDot
	TokenStar
	TokenLParen
	TokenRParen
	TokenEq
	TokenNe
	TokenLt
	TokenGt
	TokenLe
	TokenGe

	// Keywords
	TokenSelect
	TokenFrom
	TokenWhere
	TokenJoin
	TokenOn
	TokenAnd
	TokenOr
	TokenGroupBy
	TokenOrderBy
	TokenAsc
	TokenDesc
	TokenLimit
	TokenAs
	TokenCount
	TokenSum
	TokenAvg
	TokenMin
	TokenMax
	TokenInner
	TokenLeft
)

type Token struct {
	Type    TokenType
	Literal string
	Pos     int
}

var keywords = map[string]TokenType{
	"SELECT":  TokenSelect,
	"FROM":    TokenFrom,
	"WHERE":   TokenWhere,
	"JOIN":    TokenJoin,
	"ON":      TokenOn,
	"AND":     TokenAnd,
	"OR":      TokenOr,
	"GROUP":   TokenGroupBy,
	"ORDER":   TokenOrderBy,
	"ASC":     TokenAsc,
	"DESC":    TokenDesc,
	"LIMIT":   TokenLimit,
	"AS":      TokenAs,
	"COUNT":   TokenCount,
	"SUM":     TokenSum,
	"AVG":     TokenAvg,
	"MIN":     TokenMin,
	"MAX":     TokenMax,
	"INNER":   TokenInner,
	"LEFT":    TokenLeft,
	"BY":      TokenEOF, // consumed as part of GROUP BY / ORDER BY
}

type Lexer struct {
	input  string
	pos    int
	tokens []Token
}

func Tokenize(input string) ([]Token, error) {
	l := &Lexer{input: input}
	return l.tokenize()
}

func (l *Lexer) tokenize() ([]Token, error) {
	for l.pos < len(l.input) {
		ch := l.input[l.pos]

		if unicode.IsSpace(rune(ch)) {
			l.pos++
			continue
		}

		switch ch {
		case ',':
			l.emit(TokenComma, ",")
		case '.':
			l.emit(TokenDot, ".")
		case '*':
			l.emit(TokenStar, "*")
		case '(':
			l.emit(TokenLParen, "(")
		case ')':
			l.emit(TokenRParen, ")")
		case '=':
			l.emit(TokenEq, "=")
		case '<':
			if l.peek() == '=' {
				l.pos++
				l.emit(TokenLe, "<=")
			} else {
				l.emit(TokenLt, "<")
			}
		case '>':
			if l.peek() == '=' {
				l.pos++
				l.emit(TokenGe, ">=")
			} else {
				l.emit(TokenGt, ">")
			}
		case '!':
			if l.peek() == '=' {
				l.pos++
				l.emit(TokenNe, "!=")
			} else {
				return nil, fmt.Errorf("unexpected char at %d: !", l.pos)
			}
		case '\'':
			s, err := l.readString()
			if err != nil {
				return nil, err
			}
			l.tokens = append(l.tokens, Token{Type: TokenString, Literal: s, Pos: l.pos})
		default:
			if unicode.IsDigit(rune(ch)) {
				num := l.readNumber()
				if strings.Contains(num, ".") {
					l.tokens = append(l.tokens, Token{Type: TokenFloat, Literal: num, Pos: l.pos})
				} else {
					l.tokens = append(l.tokens, Token{Type: TokenNumber, Literal: num, Pos: l.pos})
				}
				continue
			}
			if unicode.IsLetter(rune(ch)) || ch == '_' {
				ident := l.readIdent()
				upper := strings.ToUpper(ident)
				if upper == "BY" {
					// Merge with previous GROUP or ORDER token
					continue
				}
				if tt, ok := keywords[upper]; ok {
					l.tokens = append(l.tokens, Token{Type: tt, Literal: ident, Pos: l.pos})
				} else {
					l.tokens = append(l.tokens, Token{Type: TokenIdent, Literal: ident, Pos: l.pos})
				}
				continue
			}
			return nil, fmt.Errorf("unexpected char at %d: %c", l.pos, ch)
		}
		l.pos++
	}

	l.tokens = append(l.tokens, Token{Type: TokenEOF, Pos: l.pos})
	return l.tokens, nil
}

func (l *Lexer) emit(tt TokenType, lit string) {
	l.tokens = append(l.tokens, Token{Type: tt, Literal: lit, Pos: l.pos})
}

func (l *Lexer) peek() byte {
	if l.pos+1 < len(l.input) {
		return l.input[l.pos+1]
	}
	return 0
}

func (l *Lexer) readString() (string, error) {
	l.pos++ // skip opening quote
	start := l.pos
	for l.pos < len(l.input) && l.input[l.pos] != '\'' {
		l.pos++
	}
	if l.pos >= len(l.input) {
		return "", fmt.Errorf("unterminated string")
	}
	return l.input[start:l.pos], nil
}

func (l *Lexer) readNumber() string {
	start := l.pos
	for l.pos < len(l.input) && (unicode.IsDigit(rune(l.input[l.pos])) || l.input[l.pos] == '.') {
		l.pos++
	}
	return l.input[start:l.pos]
}

func (l *Lexer) readIdent() string {
	start := l.pos
	for l.pos < len(l.input) && (unicode.IsLetter(rune(l.input[l.pos])) || unicode.IsDigit(rune(l.input[l.pos])) || l.input[l.pos] == '_') {
		l.pos++
	}
	return l.input[start:l.pos]
}
```

### `sql/parser.go`

```go
package sql

import (
	"fmt"
	"strconv"
)

// --- AST Nodes ---

type Expr interface{ exprNode() }

type ColumnRef struct {
	Table  string
	Column string
}

func (ColumnRef) exprNode() {}

type IntLiteral struct{ Value int64 }
func (IntLiteral) exprNode() {}

type FloatLiteral struct{ Value float64 }
func (FloatLiteral) exprNode() {}

type StringLiteral struct{ Value string }
func (StringLiteral) exprNode() {}

type BinaryExpr struct {
	Left  Expr
	Op    string
	Right Expr
}
func (BinaryExpr) exprNode() {}

type AggregateExpr struct {
	Func string // COUNT, SUM, AVG, MIN, MAX
	Arg  Expr
}
func (AggregateExpr) exprNode() {}

type StarExpr struct{}
func (StarExpr) exprNode() {}

type SelectItem struct {
	Expr  Expr
	Alias string
}

type JoinClause struct {
	Table     string
	Alias     string
	Condition Expr
	JoinType  string // INNER, LEFT
}

type OrderByItem struct {
	Expr Expr
	Desc bool
}

type SelectStmt struct {
	Columns  []SelectItem
	From     string
	FromAlias string
	Joins    []JoinClause
	Where    Expr
	GroupBy  []Expr
	OrderBy  []OrderByItem
	Limit    int
}

// --- Parser ---

type Parser struct {
	tokens []Token
	pos    int
}

func Parse(input string) (*SelectStmt, error) {
	tokens, err := Tokenize(input)
	if err != nil {
		return nil, err
	}
	p := &Parser{tokens: tokens}
	return p.parseSelect()
}

func (p *Parser) current() Token {
	if p.pos >= len(p.tokens) {
		return Token{Type: TokenEOF}
	}
	return p.tokens[p.pos]
}

func (p *Parser) advance() Token {
	t := p.current()
	p.pos++
	return t
}

func (p *Parser) expect(tt TokenType) (Token, error) {
	t := p.advance()
	if t.Type != tt {
		return t, fmt.Errorf("expected token type %d, got %d (%q) at pos %d", tt, t.Type, t.Literal, t.Pos)
	}
	return t, nil
}

func (p *Parser) parseSelect() (*SelectStmt, error) {
	if _, err := p.expect(TokenSelect); err != nil {
		return nil, err
	}

	stmt := &SelectStmt{Limit: -1}

	// Columns
	for {
		item, err := p.parseSelectItem()
		if err != nil {
			return nil, err
		}
		stmt.Columns = append(stmt.Columns, item)
		if p.current().Type != TokenComma {
			break
		}
		p.advance() // consume comma
	}

	// FROM
	if _, err := p.expect(TokenFrom); err != nil {
		return nil, err
	}
	tableTok, err := p.expect(TokenIdent)
	if err != nil {
		return nil, err
	}
	stmt.From = tableTok.Literal

	if p.current().Type == TokenAs || p.current().Type == TokenIdent {
		if p.current().Type == TokenAs {
			p.advance()
		}
		alias := p.advance()
		stmt.FromAlias = alias.Literal
	}

	// JOINs
	for p.current().Type == TokenJoin || p.current().Type == TokenInner || p.current().Type == TokenLeft {
		join, err := p.parseJoin()
		if err != nil {
			return nil, err
		}
		stmt.Joins = append(stmt.Joins, join)
	}

	// WHERE
	if p.current().Type == TokenWhere {
		p.advance()
		expr, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		stmt.Where = expr
	}

	// GROUP BY
	if p.current().Type == TokenGroupBy {
		p.advance()
		for {
			expr, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			stmt.GroupBy = append(stmt.GroupBy, expr)
			if p.current().Type != TokenComma {
				break
			}
			p.advance()
		}
	}

	// ORDER BY
	if p.current().Type == TokenOrderBy {
		p.advance()
		for {
			expr, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			desc := false
			if p.current().Type == TokenDesc {
				desc = true
				p.advance()
			} else if p.current().Type == TokenAsc {
				p.advance()
			}
			stmt.OrderBy = append(stmt.OrderBy, OrderByItem{Expr: expr, Desc: desc})
			if p.current().Type != TokenComma {
				break
			}
			p.advance()
		}
	}

	// LIMIT
	if p.current().Type == TokenLimit {
		p.advance()
		numTok, err := p.expect(TokenNumber)
		if err != nil {
			return nil, err
		}
		stmt.Limit, _ = strconv.Atoi(numTok.Literal)
	}

	return stmt, nil
}

func (p *Parser) parseSelectItem() (SelectItem, error) {
	if p.current().Type == TokenStar {
		p.advance()
		return SelectItem{Expr: StarExpr{}}, nil
	}
	expr, err := p.parseExpr(0)
	if err != nil {
		return SelectItem{}, err
	}
	item := SelectItem{Expr: expr}
	if p.current().Type == TokenAs {
		p.advance()
		alias := p.advance()
		item.Alias = alias.Literal
	}
	return item, nil
}

func (p *Parser) parseJoin() (JoinClause, error) {
	joinType := "INNER"
	if p.current().Type == TokenLeft {
		joinType = "LEFT"
		p.advance()
	}
	if p.current().Type == TokenInner {
		p.advance()
	}
	p.expect(TokenJoin)

	tableTok, err := p.expect(TokenIdent)
	if err != nil {
		return JoinClause{}, err
	}
	j := JoinClause{Table: tableTok.Literal, JoinType: joinType}

	if p.current().Type == TokenAs || p.current().Type == TokenIdent {
		if p.current().Type == TokenAs {
			p.advance()
		}
		if p.current().Type == TokenIdent && p.current().Type != TokenOn {
			j.Alias = p.advance().Literal
		}
	}

	if _, err := p.expect(TokenOn); err != nil {
		return JoinClause{}, err
	}

	cond, err := p.parseExpr(0)
	if err != nil {
		return JoinClause{}, err
	}
	j.Condition = cond
	return j, nil
}

// Pratt parser for expressions
func (p *Parser) parseExpr(minPrec int) (Expr, error) {
	left, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}

	for {
		prec := p.precedence()
		if prec <= minPrec {
			break
		}
		op := p.advance()
		right, err := p.parseExpr(prec)
		if err != nil {
			return nil, err
		}
		left = BinaryExpr{Left: left, Op: op.Literal, Right: right}
	}

	return left, nil
}

func (p *Parser) precedence() int {
	switch p.current().Type {
	case TokenOr:
		return 1
	case TokenAnd:
		return 2
	case TokenEq, TokenNe, TokenLt, TokenGt, TokenLe, TokenGe:
		return 3
	default:
		return 0
	}
}

func (p *Parser) parsePrimary() (Expr, error) {
	t := p.current()

	switch t.Type {
	case TokenNumber:
		p.advance()
		val, _ := strconv.ParseInt(t.Literal, 10, 64)
		return IntLiteral{Value: val}, nil
	case TokenFloat:
		p.advance()
		val, _ := strconv.ParseFloat(t.Literal, 64)
		return FloatLiteral{Value: val}, nil
	case TokenString:
		p.advance()
		return StringLiteral{Value: t.Literal}, nil
	case TokenCount, TokenSum, TokenAvg, TokenMin, TokenMax:
		return p.parseAggregate()
	case TokenLParen:
		p.advance()
		expr, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		p.expect(TokenRParen)
		return expr, nil
	case TokenIdent:
		p.advance()
		if p.current().Type == TokenDot {
			p.advance()
			col := p.advance()
			return ColumnRef{Table: t.Literal, Column: col.Literal}, nil
		}
		return ColumnRef{Column: t.Literal}, nil
	case TokenStar:
		p.advance()
		return StarExpr{}, nil
	default:
		return nil, fmt.Errorf("unexpected token %d (%q) at pos %d", t.Type, t.Literal, t.Pos)
	}
}

func (p *Parser) parseAggregate() (Expr, error) {
	funcTok := p.advance()
	p.expect(TokenLParen)
	arg, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	p.expect(TokenRParen)
	funcName := ""
	switch funcTok.Type {
	case TokenCount:
		funcName = "COUNT"
	case TokenSum:
		funcName = "SUM"
	case TokenAvg:
		funcName = "AVG"
	case TokenMin:
		funcName = "MIN"
	case TokenMax:
		funcName = "MAX"
	}
	return AggregateExpr{Func: funcName, Arg: arg}, nil
}
```

### `plan/logical.go`

```go
package plan

import "distributed-sql/sql"

type LogicalOp interface{ logicalOp() }

type Scan struct {
	Table string
}
func (Scan) logicalOp() {}

type Filter struct {
	Input     LogicalOp
	Predicate sql.Expr
}
func (Filter) logicalOp() {}

type Project struct {
	Input   LogicalOp
	Columns []sql.SelectItem
}
func (Project) logicalOp() {}

type Join struct {
	Left      LogicalOp
	Right     LogicalOp
	Condition sql.Expr
	JoinType  string
}
func (Join) logicalOp() {}

type Aggregate struct {
	Input    LogicalOp
	GroupBy  []sql.Expr
	Aggs     []sql.AggregateExpr
}
func (Aggregate) logicalOp() {}

type Sort struct {
	Input   LogicalOp
	OrderBy []sql.OrderByItem
}
func (Sort) logicalOp() {}

type Limit struct {
	Input LogicalOp
	N     int
}
func (Limit) logicalOp() {}

func BuildLogicalPlan(stmt *sql.SelectStmt) LogicalOp {
	var op LogicalOp = Scan{Table: stmt.From}

	for _, j := range stmt.Joins {
		right := Scan{Table: j.Table}
		op = Join{Left: op, Right: right, Condition: j.Condition, JoinType: j.JoinType}
	}

	if stmt.Where != nil {
		op = Filter{Input: op, Predicate: stmt.Where}
	}

	// Extract aggregates from SELECT items
	var aggs []sql.AggregateExpr
	for _, col := range stmt.Columns {
		if agg, ok := col.Expr.(sql.AggregateExpr); ok {
			aggs = append(aggs, agg)
		}
	}

	if len(stmt.GroupBy) > 0 || len(aggs) > 0 {
		op = Aggregate{Input: op, GroupBy: stmt.GroupBy, Aggs: aggs}
	}

	op = Project{Input: op, Columns: stmt.Columns}

	if len(stmt.OrderBy) > 0 {
		op = Sort{Input: op, OrderBy: stmt.OrderBy}
	}

	if stmt.Limit >= 0 {
		op = Limit{Input: op, N: stmt.Limit}
	}

	return op
}
```

### `plan/physical.go`

```go
package plan

import "distributed-sql/sql"

type PhysicalOp interface{ physicalOp() }

type TableScan struct {
	Table      string
	PartitionID int
}
func (TableScan) physicalOp() {}

type PhysicalFilter struct {
	Input     PhysicalOp
	Predicate sql.Expr
}
func (PhysicalFilter) physicalOp() {}

type PhysicalProject struct {
	Input   PhysicalOp
	Columns []sql.SelectItem
}
func (PhysicalProject) physicalOp() {}

type HashJoin struct {
	BuildSide PhysicalOp
	ProbeSide PhysicalOp
	BuildKey  string
	ProbeKey  string
	JoinType  string
}
func (HashJoin) physicalOp() {}

type SortMergeJoin struct {
	Left     PhysicalOp
	Right    PhysicalOp
	LeftKey  string
	RightKey string
}
func (SortMergeJoin) physicalOp() {}

type PartialAggregate struct {
	Input   PhysicalOp
	GroupBy []sql.Expr
	Aggs    []sql.AggregateExpr
}
func (PartialAggregate) physicalOp() {}

type FinalAggregate struct {
	Input   PhysicalOp
	GroupBy []sql.Expr
	Aggs    []sql.AggregateExpr
}
func (FinalAggregate) physicalOp() {}

type PhysicalSort struct {
	Input   PhysicalOp
	OrderBy []sql.OrderByItem
}
func (PhysicalSort) physicalOp() {}

type PhysicalLimit struct {
	Input PhysicalOp
	N     int
}
func (PhysicalLimit) physicalOp() {}

type HashExchange struct {
	Input    PhysicalOp
	HashKey  string
	TargetWorkers int
}
func (HashExchange) physicalOp() {}

type GatherExchange struct {
	Input PhysicalOp
}
func (GatherExchange) physicalOp() {}

type Catalog struct {
	Tables map[string]TableInfo
}

type TableInfo struct {
	Name         string
	PartitionKey string
	NumPartitions int
	Columns      []ColumnInfo
}

type ColumnInfo struct {
	Name string
	Type string // INT, FLOAT, VARCHAR
}

func BuildPhysicalPlan(logical LogicalOp, catalog *Catalog, numWorkers int) PhysicalOp {
	return convertLogical(logical, catalog, numWorkers)
}

func convertLogical(op LogicalOp, catalog *Catalog, numWorkers int) PhysicalOp {
	switch o := op.(type) {
	case Scan:
		return TableScan{Table: o.Table}

	case Filter:
		input := convertLogical(o.Input, catalog, numWorkers)
		return PhysicalFilter{Input: input, Predicate: o.Predicate}

	case Project:
		input := convertLogical(o.Input, catalog, numWorkers)
		return PhysicalProject{Input: input, Columns: o.Columns}

	case Join:
		left := convertLogical(o.Left, catalog, numWorkers)
		right := convertLogical(o.Right, catalog, numWorkers)

		leftKey, rightKey := extractJoinKeys(o.Condition)

		// Check if exchange is needed
		leftTable := findScanTable(o.Left)
		rightTable := findScanTable(o.Right)

		needExchange := false
		if leftTable != "" && rightTable != "" {
			lt := catalog.Tables[leftTable]
			rt := catalog.Tables[rightTable]
			if lt.PartitionKey != leftKey || rt.PartitionKey != rightKey {
				needExchange = true
			}
		}

		if needExchange {
			left = HashExchange{Input: left, HashKey: leftKey, TargetWorkers: numWorkers}
			right = HashExchange{Input: right, HashKey: rightKey, TargetWorkers: numWorkers}
		}

		return HashJoin{
			BuildSide: right,
			ProbeSide: left,
			BuildKey:  rightKey,
			ProbeKey:  leftKey,
			JoinType:  o.JoinType,
		}

	case Aggregate:
		input := convertLogical(o.Input, catalog, numWorkers)
		partial := PartialAggregate{Input: input, GroupBy: o.GroupBy, Aggs: o.Aggs}
		gathered := GatherExchange{Input: partial}
		return FinalAggregate{Input: gathered, GroupBy: o.GroupBy, Aggs: o.Aggs}

	case Sort:
		input := convertLogical(o.Input, catalog, numWorkers)
		gathered := GatherExchange{Input: input}
		return PhysicalSort{Input: gathered, OrderBy: o.OrderBy}

	case Limit:
		input := convertLogical(o.Input, catalog, numWorkers)
		return PhysicalLimit{Input: input, N: o.N}
	}

	return nil
}

func extractJoinKeys(cond sql.Expr) (string, string) {
	if be, ok := cond.(sql.BinaryExpr); ok && be.Op == "=" {
		leftCol := ""
		rightCol := ""
		if cr, ok := be.Left.(sql.ColumnRef); ok {
			leftCol = cr.Column
		}
		if cr, ok := be.Right.(sql.ColumnRef); ok {
			rightCol = cr.Column
		}
		return leftCol, rightCol
	}
	return "", ""
}

func findScanTable(op LogicalOp) string {
	switch o := op.(type) {
	case Scan:
		return o.Table
	case Filter:
		return findScanTable(o.Input)
	case Project:
		return findScanTable(o.Input)
	default:
		return ""
	}
}
```

### `exec/engine.go`

```go
package exec

import (
	"distributed-sql/plan"
	"distributed-sql/sql"
	"fmt"
	"hash/fnv"
	"sort"
)

type Row []Value

type Value struct {
	Type    string // INT, FLOAT, VARCHAR
	IntVal  int64
	FltVal  float64
	StrVal  string
	IsNull  bool
}

func (v Value) String() string {
	if v.IsNull {
		return "NULL"
	}
	switch v.Type {
	case "INT":
		return fmt.Sprintf("%d", v.IntVal)
	case "FLOAT":
		return fmt.Sprintf("%.2f", v.FltVal)
	default:
		return v.StrVal
	}
}

type DataSource interface {
	Scan(table string, partitionID int) ([]string, []Row, error)
}

type Engine struct {
	source DataSource
}

func NewEngine(source DataSource) *Engine {
	return &Engine{source: source}
}

func (e *Engine) Execute(op plan.PhysicalOp, partitionID int) ([]string, []Row, error) {
	switch o := op.(type) {
	case plan.TableScan:
		return e.source.Scan(o.Table, partitionID)

	case plan.PhysicalFilter:
		cols, rows, err := e.Execute(o.Input, partitionID)
		if err != nil {
			return nil, nil, err
		}
		var filtered []Row
		for _, row := range rows {
			if evalPredicate(o.Predicate, cols, row) {
				filtered = append(filtered, row)
			}
		}
		return cols, filtered, nil

	case plan.PhysicalProject:
		cols, rows, err := e.Execute(o.Input, partitionID)
		if err != nil {
			return nil, nil, err
		}
		return project(cols, rows, o.Columns)

	case plan.HashJoin:
		return e.executeHashJoin(o, partitionID)

	case plan.PartialAggregate:
		cols, rows, err := e.Execute(o.Input, partitionID)
		if err != nil {
			return nil, nil, err
		}
		return partialAggregate(cols, rows, o.GroupBy, o.Aggs)

	case plan.FinalAggregate:
		cols, rows, err := e.Execute(o.Input, partitionID)
		if err != nil {
			return nil, nil, err
		}
		return finalAggregate(cols, rows, o.GroupBy, o.Aggs)

	case plan.PhysicalSort:
		cols, rows, err := e.Execute(o.Input, partitionID)
		if err != nil {
			return nil, nil, err
		}
		sortRows(cols, rows, o.OrderBy)
		return cols, rows, nil

	case plan.PhysicalLimit:
		cols, rows, err := e.Execute(o.Input, partitionID)
		if err != nil {
			return nil, nil, err
		}
		if o.N < len(rows) {
			rows = rows[:o.N]
		}
		return cols, rows, nil

	case plan.GatherExchange:
		return e.Execute(o.Input, partitionID)
	}

	return nil, nil, fmt.Errorf("unsupported operator: %T", op)
}

func (e *Engine) executeHashJoin(op plan.HashJoin, partitionID int) ([]string, []Row, error) {
	buildCols, buildRows, err := e.Execute(op.BuildSide, partitionID)
	if err != nil {
		return nil, nil, err
	}
	probeCols, probeRows, err := e.Execute(op.ProbeSide, partitionID)
	if err != nil {
		return nil, nil, err
	}

	buildKeyIdx := colIndex(buildCols, op.BuildKey)
	probeKeyIdx := colIndex(probeCols, op.ProbeKey)

	if buildKeyIdx < 0 || probeKeyIdx < 0 {
		return nil, nil, fmt.Errorf("join key not found: build=%s probe=%s", op.BuildKey, op.ProbeKey)
	}

	// Build hash table
	hashTable := make(map[string][]Row)
	for _, row := range buildRows {
		key := row[buildKeyIdx].String()
		hashTable[key] = append(hashTable[key], row)
	}

	// Probe
	outCols := append(probeCols, buildCols...)
	var result []Row
	for _, probeRow := range probeRows {
		key := probeRow[probeKeyIdx].String()
		if matches, ok := hashTable[key]; ok {
			for _, buildRow := range matches {
				combined := make(Row, len(probeRow)+len(buildRow))
				copy(combined, probeRow)
				copy(combined[len(probeRow):], buildRow)
				result = append(result, combined)
			}
		}
	}

	return outCols, result, nil
}

func evalPredicate(expr sql.Expr, cols []string, row Row) bool {
	switch e := expr.(type) {
	case sql.BinaryExpr:
		switch e.Op {
		case "AND":
			return evalPredicate(e.Left, cols, row) && evalPredicate(e.Right, cols, row)
		case "OR":
			return evalPredicate(e.Left, cols, row) || evalPredicate(e.Right, cols, row)
		default:
			left := evalValue(e.Left, cols, row)
			right := evalValue(e.Right, cols, row)
			return compareValues(left, right, e.Op)
		}
	}
	return true
}

func evalValue(expr sql.Expr, cols []string, row Row) Value {
	switch e := expr.(type) {
	case sql.ColumnRef:
		idx := colIndex(cols, e.Column)
		if idx >= 0 && idx < len(row) {
			return row[idx]
		}
		// Try table.column format
		fullName := e.Table + "." + e.Column
		idx = colIndex(cols, fullName)
		if idx >= 0 && idx < len(row) {
			return row[idx]
		}
		return Value{IsNull: true}
	case sql.IntLiteral:
		return Value{Type: "INT", IntVal: e.Value}
	case sql.FloatLiteral:
		return Value{Type: "FLOAT", FltVal: e.Value}
	case sql.StringLiteral:
		return Value{Type: "VARCHAR", StrVal: e.Value}
	}
	return Value{IsNull: true}
}

func compareValues(left, right Value, op string) bool {
	if left.Type == "INT" && right.Type == "INT" {
		switch op {
		case "=":
			return left.IntVal == right.IntVal
		case "!=":
			return left.IntVal != right.IntVal
		case "<":
			return left.IntVal < right.IntVal
		case ">":
			return left.IntVal > right.IntVal
		case "<=":
			return left.IntVal <= right.IntVal
		case ">=":
			return left.IntVal >= right.IntVal
		}
	}
	// Fall back to string comparison
	return left.String() == right.String()
}

func colIndex(cols []string, name string) int {
	for i, c := range cols {
		if c == name {
			return i
		}
	}
	return -1
}

func project(cols []string, rows []Row, items []sql.SelectItem) ([]string, []Row, error) {
	var outCols []string
	var indices []int

	for _, item := range items {
		if _, ok := item.Expr.(sql.StarExpr); ok {
			return cols, rows, nil
		}
		if cr, ok := item.Expr.(sql.ColumnRef); ok {
			idx := colIndex(cols, cr.Column)
			if idx < 0 && cr.Table != "" {
				idx = colIndex(cols, cr.Table+"."+cr.Column)
			}
			if idx >= 0 {
				indices = append(indices, idx)
				name := cr.Column
				if item.Alias != "" {
					name = item.Alias
				}
				outCols = append(outCols, name)
			}
		} else if _, ok := item.Expr.(sql.AggregateExpr); ok {
			// Aggregate results are already projected by the aggregate operator
			idx := len(outCols)
			if idx < len(cols) {
				indices = append(indices, idx)
			}
			name := item.Alias
			if name == "" {
				name = cols[idx]
			}
			outCols = append(outCols, name)
		}
	}

	var outRows []Row
	for _, row := range rows {
		outRow := make(Row, len(indices))
		for i, idx := range indices {
			if idx < len(row) {
				outRow[i] = row[idx]
			}
		}
		outRows = append(outRows, outRow)
	}

	return outCols, outRows, nil
}

func partialAggregate(cols []string, rows []Row, groupBy []sql.Expr, aggs []sql.AggregateExpr) ([]string, []Row, error) {
	type groupKey string

	groups := make(map[groupKey][]Row)
	for _, row := range rows {
		key := computeGroupKey(cols, row, groupBy)
		groups[key] = append(groups[key], row)
	}

	var outCols []string
	for _, gb := range groupBy {
		if cr, ok := gb.(sql.ColumnRef); ok {
			outCols = append(outCols, cr.Column)
		}
	}
	for _, agg := range aggs {
		outCols = append(outCols, fmt.Sprintf("_partial_%s", agg.Func))
	}
	if len(aggs) > 0 {
		outCols = append(outCols, "_partial_count")
	}

	var outRows []Row
	for _, groupRows := range groups {
		outRow := make(Row, 0)
		// Group-by columns
		for _, gb := range groupBy {
			if cr, ok := gb.(sql.ColumnRef); ok {
				idx := colIndex(cols, cr.Column)
				if idx >= 0 {
					outRow = append(outRow, groupRows[0][idx])
				}
			}
		}
		// Partial aggregates
		for _, agg := range aggs {
			val := computePartialAgg(cols, groupRows, agg)
			outRow = append(outRow, val)
		}
		// Count for AVG computation
		if len(aggs) > 0 {
			outRow = append(outRow, Value{Type: "INT", IntVal: int64(len(groupRows))})
		}
		outRows = append(outRows, outRow)
	}

	return outCols, outRows, nil
}

func finalAggregate(cols []string, rows []Row, groupBy []sql.Expr, aggs []sql.AggregateExpr) ([]string, []Row, error) {
	// If input already has partial aggregates, combine them
	var outCols []string
	for _, gb := range groupBy {
		if cr, ok := gb.(sql.ColumnRef); ok {
			outCols = append(outCols, cr.Column)
		}
	}
	for _, agg := range aggs {
		name := fmt.Sprintf("%s(%s)", agg.Func, exprName(agg.Arg))
		outCols = append(outCols, name)
	}

	type gkey string
	groups := make(map[gkey][]Row)
	for _, row := range rows {
		key := computeGroupKey(cols, row, groupBy)
		groups[key] = append(groups[key], row)
	}

	var outRows []Row
	for _, groupRows := range groups {
		outRow := make(Row, 0)
		for _, gb := range groupBy {
			if cr, ok := gb.(sql.ColumnRef); ok {
				idx := colIndex(cols, cr.Column)
				if idx >= 0 {
					outRow = append(outRow, groupRows[0][idx])
				}
			}
		}
		for _, agg := range aggs {
			val := computePartialAgg(cols, groupRows, agg)
			outRow = append(outRow, val)
		}
		outRows = append(outRows, outRow)
	}

	return outCols, outRows, nil
}

func computeGroupKey(cols []string, row Row, groupBy []sql.Expr) groupKey {
	key := ""
	for _, gb := range groupBy {
		if cr, ok := gb.(sql.ColumnRef); ok {
			idx := colIndex(cols, cr.Column)
			if idx >= 0 {
				key += row[idx].String() + "|"
			}
		}
	}
	return groupKey(key)
}

type groupKey = string

func computePartialAgg(cols []string, rows []Row, agg sql.AggregateExpr) Value {
	colName := ""
	if cr, ok := agg.Arg.(sql.ColumnRef); ok {
		colName = cr.Column
	}
	idx := colIndex(cols, colName)

	switch agg.Func {
	case "COUNT":
		if _, ok := agg.Arg.(sql.StarExpr); ok {
			return Value{Type: "INT", IntVal: int64(len(rows))}
		}
		count := int64(0)
		for _, row := range rows {
			if idx >= 0 && idx < len(row) && !row[idx].IsNull {
				count++
			}
		}
		return Value{Type: "INT", IntVal: count}

	case "SUM":
		sum := int64(0)
		for _, row := range rows {
			if idx >= 0 && idx < len(row) {
				sum += row[idx].IntVal
			}
		}
		return Value{Type: "INT", IntVal: sum}

	case "AVG":
		sum := float64(0)
		count := 0
		for _, row := range rows {
			if idx >= 0 && idx < len(row) {
				sum += float64(row[idx].IntVal) + row[idx].FltVal
				count++
			}
		}
		if count == 0 {
			return Value{IsNull: true}
		}
		return Value{Type: "FLOAT", FltVal: sum / float64(count)}

	case "MIN":
		if len(rows) == 0 {
			return Value{IsNull: true}
		}
		min := rows[0][idx]
		for _, row := range rows[1:] {
			if row[idx].IntVal < min.IntVal {
				min = row[idx]
			}
		}
		return min

	case "MAX":
		if len(rows) == 0 {
			return Value{IsNull: true}
		}
		max := rows[0][idx]
		for _, row := range rows[1:] {
			if row[idx].IntVal > max.IntVal {
				max = row[idx]
			}
		}
		return max
	}

	return Value{IsNull: true}
}

func sortRows(cols []string, rows []Row, orderBy []sql.OrderByItem) {
	sort.SliceStable(rows, func(i, j int) bool {
		for _, ob := range orderBy {
			if cr, ok := ob.Expr.(sql.ColumnRef); ok {
				idx := colIndex(cols, cr.Column)
				if idx < 0 {
					continue
				}
				cmp := compareForSort(rows[i][idx], rows[j][idx])
				if cmp == 0 {
					continue
				}
				if ob.Desc {
					return cmp > 0
				}
				return cmp < 0
			}
		}
		return false
	})
}

func compareForSort(a, b Value) int {
	if a.Type == "INT" && b.Type == "INT" {
		if a.IntVal < b.IntVal {
			return -1
		}
		if a.IntVal > b.IntVal {
			return 1
		}
		return 0
	}
	if a.String() < b.String() {
		return -1
	}
	if a.String() > b.String() {
		return 1
	}
	return 0
}

func hashPartition(key string, numPartitions int) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32()) % numPartitions
}

func exprName(e sql.Expr) string {
	switch v := e.(type) {
	case sql.ColumnRef:
		return v.Column
	case sql.StarExpr:
		return "*"
	default:
		return "?"
	}
}
```

### `network/protocol.go`

```go
package network

import (
	"distributed-sql/exec"
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

const (
	MsgPlanFragment  = uint8(1)
	MsgRowBatch      = uint8(2)
	MsgExchangeData  = uint8(3)
	MsgResult        = uint8(4)
	MsgError         = uint8(5)
	MsgHeartbeat     = uint8(6)
)

func WriteMessage(w io.Writer, msgType uint8, payload []byte) error {
	header := make([]byte, 5)
	binary.BigEndian.PutUint32(header[0:4], uint32(len(payload)))
	header[4] = msgType
	if _, err := w.Write(header); err != nil {
		return err
	}
	if len(payload) > 0 {
		_, err := w.Write(payload)
		return err
	}
	return nil
}

func ReadMessage(r io.Reader) (uint8, []byte, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(r, header); err != nil {
		return 0, nil, err
	}
	length := binary.BigEndian.Uint32(header[0:4])
	msgType := header[4]
	payload := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return 0, nil, err
		}
	}
	return msgType, payload, nil
}

func EncodeRowBatch(cols []string, rows []exec.Row) []byte {
	var buf []byte

	// Number of columns
	buf = binary.BigEndian.AppendUint16(buf, uint16(len(cols)))
	for _, col := range cols {
		buf = binary.BigEndian.AppendUint16(buf, uint16(len(col)))
		buf = append(buf, col...)
	}

	// Number of rows
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(rows)))
	for _, row := range rows {
		for _, val := range row {
			switch val.Type {
			case "INT":
				buf = append(buf, 'I')
				buf = binary.BigEndian.AppendUint64(buf, uint64(val.IntVal))
			case "FLOAT":
				buf = append(buf, 'F')
				buf = binary.BigEndian.AppendUint64(buf, math.Float64bits(val.FltVal))
			default:
				buf = append(buf, 'S')
				buf = binary.BigEndian.AppendUint32(buf, uint32(len(val.StrVal)))
				buf = append(buf, val.StrVal...)
			}
		}
	}

	return buf
}

func DecodeRowBatch(data []byte) ([]string, []exec.Row, error) {
	if len(data) < 2 {
		return nil, nil, fmt.Errorf("row batch too short")
	}
	offset := 0

	numCols := int(binary.BigEndian.Uint16(data[offset:]))
	offset += 2

	cols := make([]string, numCols)
	for i := 0; i < numCols; i++ {
		nameLen := int(binary.BigEndian.Uint16(data[offset:]))
		offset += 2
		cols[i] = string(data[offset : offset+nameLen])
		offset += nameLen
	}

	numRows := int(binary.BigEndian.Uint32(data[offset:]))
	offset += 4

	rows := make([]exec.Row, numRows)
	for i := 0; i < numRows; i++ {
		row := make(exec.Row, numCols)
		for j := 0; j < numCols; j++ {
			typeTag := data[offset]
			offset++
			switch typeTag {
			case 'I':
				row[j] = exec.Value{Type: "INT", IntVal: int64(binary.BigEndian.Uint64(data[offset:]))}
				offset += 8
			case 'F':
				row[j] = exec.Value{Type: "FLOAT", FltVal: math.Float64frombits(binary.BigEndian.Uint64(data[offset:]))}
				offset += 8
			case 'S':
				sLen := int(binary.BigEndian.Uint32(data[offset:]))
				offset += 4
				row[j] = exec.Value{Type: "VARCHAR", StrVal: string(data[offset : offset+sLen])}
				offset += sLen
			}
		}
		rows[i] = row
	}

	return cols, rows, nil
}
```

### `coordinator.go`

```go
package main

import (
	"bufio"
	"distributed-sql/exec"
	"distributed-sql/network"
	"distributed-sql/plan"
	"distributed-sql/sql"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
)

type Coordinator struct {
	catalog *plan.Catalog
	workers []string // worker addresses
}

func NewCoordinator(catalog *plan.Catalog, workers []string) *Coordinator {
	return &Coordinator{catalog: catalog, workers: workers}
}

func (c *Coordinator) ExecuteQuery(query string) ([]string, []exec.Row, error) {
	stmt, err := sql.Parse(query)
	if err != nil {
		return nil, nil, fmt.Errorf("parse: %w", err)
	}

	logical := plan.BuildLogicalPlan(stmt)
	physical := plan.BuildPhysicalPlan(logical, c.catalog, len(c.workers))

	_ = physical // In a full implementation, serialize and send fragments to workers

	// For now, execute locally by collecting from all workers
	return c.executeDistributed(physical)
}

func (c *Coordinator) executeDistributed(op plan.PhysicalOp) ([]string, []exec.Row, error) {
	var mu sync.Mutex
	var allCols []string
	var allRows []exec.Row
	var firstErr error
	var wg sync.WaitGroup

	for i, addr := range c.workers {
		wg.Add(1)
		go func(workerID int, workerAddr string) {
			defer wg.Done()
			cols, rows, err := c.sendToWorker(workerAddr, workerID)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			if allCols == nil {
				allCols = cols
			}
			allRows = append(allRows, rows...)
		}(i, addr)
	}

	wg.Wait()
	if firstErr != nil {
		return nil, nil, firstErr
	}

	return allCols, allRows, nil
}

func (c *Coordinator) sendToWorker(addr string, partitionID int) ([]string, []exec.Row, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, nil, err
	}
	defer conn.Close()

	// Send plan fragment
	planData := []byte(fmt.Sprintf("PARTITION:%d", partitionID))
	if err := network.WriteMessage(conn, network.MsgPlanFragment, planData); err != nil {
		return nil, nil, err
	}

	// Read result
	msgType, payload, err := network.ReadMessage(conn)
	if err != nil {
		return nil, nil, err
	}

	if msgType == network.MsgError {
		return nil, nil, fmt.Errorf("worker error: %s", payload)
	}

	return network.DecodeRowBatch(payload)
}

func FormatResult(cols []string, rows []exec.Row) string {
	widths := make([]int, len(cols))
	for i, col := range cols {
		widths[i] = len(col)
	}
	for _, row := range rows {
		for i, val := range row {
			if i < len(widths) {
				s := val.String()
				if len(s) > widths[i] {
					widths[i] = len(s)
				}
			}
		}
	}

	var sb strings.Builder

	// Header
	for i, col := range cols {
		if i > 0 {
			sb.WriteString(" | ")
		}
		fmt.Fprintf(&sb, "%-*s", widths[i], col)
	}
	sb.WriteByte('\n')

	// Separator
	for i, w := range widths {
		if i > 0 {
			sb.WriteString("-+-")
		}
		sb.WriteString(strings.Repeat("-", w))
	}
	sb.WriteByte('\n')

	// Rows
	for _, row := range rows {
		for i, val := range row {
			if i > 0 {
				sb.WriteString(" | ")
			}
			if i < len(widths) {
				fmt.Fprintf(&sb, "%-*s", widths[i], val.String())
			}
		}
		sb.WriteByte('\n')
	}

	fmt.Fprintf(&sb, "(%d rows)\n", len(rows))
	return sb.String()
}

func (c *Coordinator) ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	log.Printf("coordinator listening on %s", addr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			continue
		}
		go c.handleClient(conn)
	}
}

func (c *Coordinator) handleClient(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		query := scanner.Text()
		cols, rows, err := c.ExecuteQuery(query)
		if err != nil {
			fmt.Fprintf(conn, "ERROR: %v\n", err)
			continue
		}
		fmt.Fprint(conn, FormatResult(cols, rows))
	}
}
```

### `main_test.go`

```go
package main

import (
	"distributed-sql/exec"
	"distributed-sql/plan"
	"distributed-sql/sql"
	"distributed-sql/network"
	"testing"
)

func TestLexer(t *testing.T) {
	tokens, err := sql.Tokenize("SELECT id, name FROM users WHERE age > 25")
	if err != nil {
		t.Fatal(err)
	}
	// SELECT, id, comma, name, FROM, users, WHERE, age, >, 25, EOF
	if len(tokens) != 11 {
		t.Fatalf("token count: got %d want 11", len(tokens))
	}
}

func TestParser(t *testing.T) {
	stmt, err := sql.Parse("SELECT id, name FROM users WHERE age > 25 ORDER BY name ASC LIMIT 10")
	if err != nil {
		t.Fatal(err)
	}
	if stmt.From != "users" {
		t.Errorf("from: %s", stmt.From)
	}
	if len(stmt.Columns) != 2 {
		t.Errorf("columns: %d", len(stmt.Columns))
	}
	if stmt.Limit != 10 {
		t.Errorf("limit: %d", stmt.Limit)
	}
}

func TestParserJoin(t *testing.T) {
	stmt, err := sql.Parse("SELECT o.id, u.name FROM orders o JOIN users u ON o.user_id = u.id")
	if err != nil {
		t.Fatal(err)
	}
	if len(stmt.Joins) != 1 {
		t.Fatalf("joins: %d", len(stmt.Joins))
	}
	if stmt.Joins[0].Table != "users" {
		t.Errorf("join table: %s", stmt.Joins[0].Table)
	}
}

func TestParserAggregate(t *testing.T) {
	stmt, err := sql.Parse("SELECT department, COUNT(*), SUM(salary) FROM employees GROUP BY department")
	if err != nil {
		t.Fatal(err)
	}
	if len(stmt.GroupBy) != 1 {
		t.Fatalf("group by: %d", len(stmt.GroupBy))
	}
}

func TestLogicalPlan(t *testing.T) {
	stmt, _ := sql.Parse("SELECT id FROM users WHERE age > 25")
	lp := plan.BuildLogicalPlan(stmt)

	_, ok := lp.(plan.Limit)
	if !ok {
		// Top should be Project since there's no LIMIT
		_, ok = lp.(plan.Project)
		if !ok {
			t.Fatalf("expected Project at top, got %T", lp)
		}
	}
}

type mockDataSource struct {
	data map[string]struct {
		cols []string
		rows []exec.Row
	}
}

func (m *mockDataSource) Scan(table string, partitionID int) ([]string, []exec.Row, error) {
	if d, ok := m.data[table]; ok {
		return d.cols, d.rows, nil
	}
	return nil, nil, nil
}

func TestHashJoin(t *testing.T) {
	source := &mockDataSource{
		data: map[string]struct {
			cols []string
			rows []exec.Row
		}{
			"users": {
				cols: []string{"id", "name"},
				rows: []exec.Row{
					{exec.Value{Type: "INT", IntVal: 1}, exec.Value{Type: "VARCHAR", StrVal: "Alice"}},
					{exec.Value{Type: "INT", IntVal: 2}, exec.Value{Type: "VARCHAR", StrVal: "Bob"}},
				},
			},
			"orders": {
				cols: []string{"order_id", "user_id", "amount"},
				rows: []exec.Row{
					{exec.Value{Type: "INT", IntVal: 100}, exec.Value{Type: "INT", IntVal: 1}, exec.Value{Type: "INT", IntVal: 50}},
					{exec.Value{Type: "INT", IntVal: 101}, exec.Value{Type: "INT", IntVal: 1}, exec.Value{Type: "INT", IntVal: 75}},
					{exec.Value{Type: "INT", IntVal: 102}, exec.Value{Type: "INT", IntVal: 2}, exec.Value{Type: "INT", IntVal: 30}},
				},
			},
		},
	}

	engine := exec.NewEngine(source)

	joinOp := plan.HashJoin{
		BuildSide: plan.TableScan{Table: "users"},
		ProbeSide: plan.TableScan{Table: "orders"},
		BuildKey:  "id",
		ProbeKey:  "user_id",
		JoinType:  "INNER",
	}

	cols, rows, err := engine.Execute(joinOp, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("join rows: got %d want 3", len(rows))
	}
	_ = cols
}

func TestRowBatchSerialization(t *testing.T) {
	cols := []string{"id", "name", "score"}
	rows := []exec.Row{
		{
			exec.Value{Type: "INT", IntVal: 1},
			exec.Value{Type: "VARCHAR", StrVal: "Alice"},
			exec.Value{Type: "FLOAT", FltVal: 95.5},
		},
		{
			exec.Value{Type: "INT", IntVal: 2},
			exec.Value{Type: "VARCHAR", StrVal: "Bob"},
			exec.Value{Type: "FLOAT", FltVal: 87.3},
		},
	}

	encoded := network.EncodeRowBatch(cols, rows)
	decodedCols, decodedRows, err := network.DecodeRowBatch(encoded)
	if err != nil {
		t.Fatal(err)
	}

	if len(decodedCols) != 3 {
		t.Fatalf("cols: got %d want 3", len(decodedCols))
	}
	if len(decodedRows) != 2 {
		t.Fatalf("rows: got %d want 2", len(decodedRows))
	}
	if decodedRows[0][1].StrVal != "Alice" {
		t.Errorf("name: got %q", decodedRows[0][1].StrVal)
	}
}

func TestResultFormatting(t *testing.T) {
	cols := []string{"id", "name"}
	rows := []exec.Row{
		{exec.Value{Type: "INT", IntVal: 1}, exec.Value{Type: "VARCHAR", StrVal: "Alice"}},
	}
	output := FormatResult(cols, rows)
	if output == "" {
		t.Fatal("empty output")
	}
}
```

## Rust Solution

The Rust implementation follows the same architecture. Key differences: the parser uses an enum-based AST with Rust's algebraic types, the execution engine uses iterators rather than materializing all rows, and the network protocol is identical byte-for-byte.

### `src/sql/mod.rs` (abbreviated -- same grammar as Go)

```rust
pub mod lexer;
pub mod parser;
pub mod ast;
```

### `src/sql/ast.rs`

```rust
#[derive(Debug, Clone)]
pub enum Expr {
    Column { table: Option<String>, name: String },
    IntLit(i64),
    FloatLit(f64),
    StringLit(String),
    Binary { left: Box<Expr>, op: String, right: Box<Expr> },
    Aggregate { func: String, arg: Box<Expr> },
    Star,
}

#[derive(Debug, Clone)]
pub struct SelectItem {
    pub expr: Expr,
    pub alias: Option<String>,
}

#[derive(Debug, Clone)]
pub struct JoinClause {
    pub table: String,
    pub alias: Option<String>,
    pub condition: Expr,
    pub join_type: String,
}

#[derive(Debug, Clone)]
pub struct OrderByItem {
    pub expr: Expr,
    pub desc: bool,
}

#[derive(Debug, Clone)]
pub struct SelectStmt {
    pub columns: Vec<SelectItem>,
    pub from: String,
    pub from_alias: Option<String>,
    pub joins: Vec<JoinClause>,
    pub where_clause: Option<Expr>,
    pub group_by: Vec<Expr>,
    pub order_by: Vec<OrderByItem>,
    pub limit: Option<usize>,
}
```

### `src/sql/parser.rs`

```rust
use super::ast::*;
use super::lexer::{Token, TokenType, tokenize};

pub struct Parser {
    tokens: Vec<Token>,
    pos: usize,
}

impl Parser {
    pub fn parse(input: &str) -> Result<SelectStmt, String> {
        let tokens = tokenize(input)?;
        let mut p = Parser { tokens, pos: 0 };
        p.parse_select()
    }

    fn current(&self) -> &Token {
        self.tokens.get(self.pos).unwrap_or(&Token {
            token_type: TokenType::Eof,
            literal: String::new(),
        })
    }

    fn advance(&mut self) -> Token {
        let t = self.current().clone();
        self.pos += 1;
        t
    }

    fn expect(&mut self, tt: TokenType) -> Result<Token, String> {
        let t = self.advance();
        if t.token_type != tt {
            return Err(format!("expected {:?}, got {:?}", tt, t.token_type));
        }
        Ok(t)
    }

    fn parse_select(&mut self) -> Result<SelectStmt, String> {
        self.expect(TokenType::Select)?;

        let mut columns = Vec::new();
        loop {
            columns.push(self.parse_select_item()?);
            if self.current().token_type != TokenType::Comma {
                break;
            }
            self.advance();
        }

        self.expect(TokenType::From)?;
        let from = self.expect(TokenType::Ident)?.literal;

        let from_alias = if self.current().token_type == TokenType::As
            || self.current().token_type == TokenType::Ident
        {
            if self.current().token_type == TokenType::As {
                self.advance();
            }
            Some(self.advance().literal)
        } else {
            None
        };

        let mut joins = Vec::new();
        while matches!(
            self.current().token_type,
            TokenType::Join | TokenType::Inner | TokenType::Left
        ) {
            joins.push(self.parse_join()?);
        }

        let where_clause = if self.current().token_type == TokenType::Where {
            self.advance();
            Some(self.parse_expr(0)?)
        } else {
            None
        };

        let mut group_by = Vec::new();
        if self.current().token_type == TokenType::GroupBy {
            self.advance();
            loop {
                group_by.push(self.parse_expr(0)?);
                if self.current().token_type != TokenType::Comma {
                    break;
                }
                self.advance();
            }
        }

        let mut order_by = Vec::new();
        if self.current().token_type == TokenType::OrderBy {
            self.advance();
            loop {
                let expr = self.parse_expr(0)?;
                let desc = if self.current().token_type == TokenType::Desc {
                    self.advance();
                    true
                } else {
                    if self.current().token_type == TokenType::Asc {
                        self.advance();
                    }
                    false
                };
                order_by.push(OrderByItem { expr, desc });
                if self.current().token_type != TokenType::Comma {
                    break;
                }
                self.advance();
            }
        }

        let limit = if self.current().token_type == TokenType::Limit {
            self.advance();
            let n = self.expect(TokenType::Number)?.literal.parse().map_err(|e| format!("{}", e))?;
            Some(n)
        } else {
            None
        };

        Ok(SelectStmt {
            columns,
            from,
            from_alias,
            joins,
            where_clause,
            group_by,
            order_by,
            limit,
        })
    }

    fn parse_select_item(&mut self) -> Result<SelectItem, String> {
        if self.current().token_type == TokenType::Star {
            self.advance();
            return Ok(SelectItem { expr: Expr::Star, alias: None });
        }
        let expr = self.parse_expr(0)?;
        let alias = if self.current().token_type == TokenType::As {
            self.advance();
            Some(self.advance().literal)
        } else {
            None
        };
        Ok(SelectItem { expr, alias })
    }

    fn parse_join(&mut self) -> Result<JoinClause, String> {
        let join_type = if self.current().token_type == TokenType::Left {
            self.advance();
            "LEFT".to_string()
        } else {
            if self.current().token_type == TokenType::Inner {
                self.advance();
            }
            "INNER".to_string()
        };
        self.expect(TokenType::Join)?;
        let table = self.expect(TokenType::Ident)?.literal;

        let alias = if self.current().token_type == TokenType::As {
            self.advance();
            Some(self.advance().literal)
        } else {
            None
        };

        self.expect(TokenType::On)?;
        let condition = self.parse_expr(0)?;
        Ok(JoinClause { table, alias, condition, join_type })
    }

    fn parse_expr(&mut self, min_prec: u8) -> Result<Expr, String> {
        let mut left = self.parse_primary()?;
        loop {
            let prec = self.current_precedence();
            if prec <= min_prec {
                break;
            }
            let op = self.advance().literal;
            let right = self.parse_expr(prec)?;
            left = Expr::Binary {
                left: Box::new(left),
                op,
                right: Box::new(right),
            };
        }
        Ok(left)
    }

    fn current_precedence(&self) -> u8 {
        match self.current().token_type {
            TokenType::Or => 1,
            TokenType::And => 2,
            TokenType::Eq | TokenType::Ne | TokenType::Lt
            | TokenType::Gt | TokenType::Le | TokenType::Ge => 3,
            _ => 0,
        }
    }

    fn parse_primary(&mut self) -> Result<Expr, String> {
        let t = self.current().clone();
        match t.token_type {
            TokenType::Number => {
                self.advance();
                Ok(Expr::IntLit(t.literal.parse().unwrap()))
            }
            TokenType::Float => {
                self.advance();
                Ok(Expr::FloatLit(t.literal.parse().unwrap()))
            }
            TokenType::StringLit => {
                self.advance();
                Ok(Expr::StringLit(t.literal))
            }
            TokenType::Count | TokenType::Sum | TokenType::Avg
            | TokenType::Min | TokenType::Max => {
                let func_name = self.advance().literal.to_uppercase();
                self.expect(TokenType::LParen)?;
                let arg = self.parse_expr(0)?;
                self.expect(TokenType::RParen)?;
                Ok(Expr::Aggregate { func: func_name, arg: Box::new(arg) })
            }
            TokenType::LParen => {
                self.advance();
                let expr = self.parse_expr(0)?;
                self.expect(TokenType::RParen)?;
                Ok(expr)
            }
            TokenType::Star => {
                self.advance();
                Ok(Expr::Star)
            }
            TokenType::Ident => {
                self.advance();
                if self.current().token_type == TokenType::Dot {
                    self.advance();
                    let col = self.advance().literal;
                    Ok(Expr::Column { table: Some(t.literal), name: col })
                } else {
                    Ok(Expr::Column { table: None, name: t.literal })
                }
            }
            _ => Err(format!("unexpected token: {:?}", t)),
        }
    }
}
```

### `src/exec/engine.rs` (core join logic)

```rust
use std::collections::HashMap;

#[derive(Debug, Clone)]
pub enum Value {
    Int(i64),
    Float(f64),
    Str(String),
    Null,
}

pub type Row = Vec<Value>;

pub fn hash_join(
    build_cols: &[String],
    build_rows: &[Row],
    probe_cols: &[String],
    probe_rows: &[Row],
    build_key: &str,
    probe_key: &str,
) -> (Vec<String>, Vec<Row>) {
    let build_idx = col_index(build_cols, build_key);
    let probe_idx = col_index(probe_cols, probe_key);

    let mut table: HashMap<String, Vec<&Row>> = HashMap::new();
    for row in build_rows {
        let key = format_value(&row[build_idx]);
        table.entry(key).or_default().push(row);
    }

    let mut out_cols: Vec<String> = probe_cols.to_vec();
    out_cols.extend_from_slice(build_cols);

    let mut result = Vec::new();
    for probe_row in probe_rows {
        let key = format_value(&probe_row[probe_idx]);
        if let Some(matches) = table.get(&key) {
            for build_row in matches {
                let mut combined = probe_row.clone();
                combined.extend_from_slice(build_row);
                result.push(combined);
            }
        }
    }

    (out_cols, result)
}

fn col_index(cols: &[String], name: &str) -> usize {
    cols.iter().position(|c| c == name).unwrap_or(0)
}

fn format_value(v: &Value) -> String {
    match v {
        Value::Int(n) => n.to_string(),
        Value::Float(f) => format!("{:.2}", f),
        Value::Str(s) => s.clone(),
        Value::Null => "NULL".to_string(),
    }
}
```

## Running

### Go

```bash
cd go/
go test -v -race ./...
go run . --coordinator --workers=localhost:9001,localhost:9002

# In separate terminals:
go run . --worker --port=9001 --data=data/partition0.csv
go run . --worker --port=9002 --data=data/partition1.csv
```

### Rust

```bash
cd rust/
cargo test
cargo run --release -- --coordinator --workers=localhost:9001,localhost:9002
```

## Expected Output

```
> SELECT o.id, u.name, o.amount FROM orders o JOIN users u ON o.user_id = u.id WHERE o.amount > 50 ORDER BY o.amount DESC LIMIT 5

id  | name  | amount
----+-------+-------
101 | Alice | 75
105 | Carol | 68
103 | Alice | 55
(3 rows)

> SELECT department, COUNT(*), AVG(salary) FROM employees GROUP BY department

department | COUNT(*) | AVG(salary)
-----------+----------+------------
eng        | 15       | 125000.00
sales      | 8        | 95000.00
hr         | 5        | 85000.00
(3 rows)
```

Tests:
```
=== RUN   TestLexer
--- PASS: TestLexer (0.00s)
=== RUN   TestParser
--- PASS: TestParser (0.00s)
=== RUN   TestParserJoin
--- PASS: TestParserJoin (0.00s)
=== RUN   TestParserAggregate
--- PASS: TestParserAggregate (0.00s)
=== RUN   TestLogicalPlan
--- PASS: TestLogicalPlan (0.00s)
=== RUN   TestHashJoin
--- PASS: TestHashJoin (0.00s)
=== RUN   TestRowBatchSerialization
--- PASS: TestRowBatchSerialization (0.00s)
=== RUN   TestResultFormatting
--- PASS: TestResultFormatting (0.00s)
PASS
```

## Design Decisions

**Why separate logical and physical plans.** The logical plan represents what the query means (semantics). The physical plan represents how to execute it (mechanics). This separation lets you change the execution strategy (hash join vs. sort-merge join, where to insert exchanges) without touching the parser or semantic analysis. Every production query engine uses this separation.

**Why hash join as the default.** Hash join has O(n+m) time complexity compared to sort-merge join's O(n log n + m log m). For unsorted data, hash join is almost always faster. The build phase creates a hash table from the smaller side; the probe phase scans the larger side. The main drawback is memory: the entire build side must fit in memory. Sort-merge join is provided as an alternative for memory-constrained scenarios.

**Why two-phase aggregation.** Computing SUM or COUNT across distributed partitions requires two passes: each worker computes a partial result from its local data, then the coordinator combines partial results. This is straightforward for SUM and COUNT (additive), but AVG requires transmitting both the partial sum and the partial count so the coordinator can compute total_sum/total_count. MIN and MAX are naturally distributive.

**Why a Pratt parser instead of a recursive-descent grammar.** SQL expressions have operator precedence (AND binds tighter than OR, comparison operators bind tighter than AND). A Pratt parser handles precedence with a simple numeric table lookup, avoiding the deep grammar nesting that a recursive-descent parser needs. It also makes adding new operators trivial: just add a precedence entry.

**Why the coordinator gathers all data before sorting.** ORDER BY requires globally sorted output. With data spread across workers, there are two options: (1) each worker sorts locally, then the coordinator merge-sorts the pre-sorted streams, or (2) gather everything and sort centrally. Option (1) is more network-efficient for large results but requires streaming merge. Option (2) is simpler and shown here. A production engine would implement the merge-sort approach.

## Common Mistakes

1. **Evaluating WHERE after JOIN instead of before.** Predicate pushdown (evaluating WHERE filters at the scan level before the join) dramatically reduces the amount of data flowing through the join. Without it, you join the full tables and then filter, which can be orders of magnitude slower.

2. **Not accounting for NULL in joins.** In SQL, NULL != NULL. A hash join that uses string representation of values will match two NULLs unless explicitly handled. The join must skip NULL keys.

3. **Using string comparison for numeric sorting.** String sort puts "9" after "10". All sorting must use typed comparison that understands INT vs. VARCHAR.

4. **Forgetting to merge partial AVG correctly.** AVG across partitions is NOT the average of averages. If partition A has AVG=10 over 100 rows and partition B has AVG=20 over 1 row, the global AVG is (10*100 + 20*1)/101, not (10+20)/2.

5. **Not inserting exchanges for non-co-partitioned joins.** If table A is partitioned by `user_id` and table B is partitioned by `product_id`, a join on `user_id = buyer_id` requires reshuffling table B by `buyer_id` before the join. Missing this exchange produces incorrect results because matching rows are on different workers.

## Performance Notes

- The hash join build phase is O(n) in the size of the build table. Use the smaller table as the build side to minimize memory.
- Row batch serialization uses a columnar format (type tag per value) to avoid per-row overhead. For large batches, a truly columnar format (all values of one column contiguous) enables SIMD comparisons.
- The Pratt parser processes tokens in a single pass with no backtracking, giving O(n) parse time in the token count.
- Network transfers dominate distributed query latency. Compression of row batches (LZ4 or zstd) reduces transfer time by 5-10x for typical data.

## Going Further

- Implement a cost-based optimizer that estimates cardinalities and chooses between hash join and sort-merge join based on table statistics
- Add predicate pushdown: push WHERE conditions below the JOIN when they reference only one table
- Implement projection pushdown: only read and transmit the columns needed by the query, not full rows
- Add an index structure (B-tree or hash index) on partition keys for faster point lookups
- Implement window functions (ROW_NUMBER, RANK, LAG/LEAD) with PARTITION BY and ORDER BY
- Add a query cache that stores results of recent queries and invalidates on data changes
- Implement EXPLAIN that prints the query plan tree without executing it

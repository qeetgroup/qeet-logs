// Package query implements LogQL++ — a SQL-like log query language (TAD §10.2)
// that compiles to ClickHouse SQL. The compiler always injects the tenant
// predicate from the authenticated identity, never from user input (TAD §7.2).
package query

// Kind is the statement type.
type Kind int

const (
	KindSearch Kind = iota // SEARCH "text" FROM logs WHERE ...
	KindSelect             // SELECT ... FROM ... WHERE ... GROUP BY ...
	KindTail               // TAIL FROM logs WHERE ...   (routed to live tail, M4)
)

// Stmt is a parsed LogQL++ statement.
type Stmt struct {
	Kind    Kind
	Search  string // SEARCH text
	Table   string // logs | auth_events
	Select  []SelectItem
	Where   Expr // optional
	GroupBy []Field
	OrderBy *OrderItem
	Limit   int // 0 = unset
}

// SelectItem is one element of a SELECT list.
type SelectItem struct {
	Agg   string // "" | count | sum | avg | min | max
	Star  bool   // "*" or count(*)
	Field Field  // when not Star
	Alias string // explicit or auto-generated
}

// Field is a (possibly dotted) field reference: service, body.x, auth.event_type, time.
type Field struct {
	Parts []string
}

// OrderItem is an ORDER BY term.
type OrderItem struct {
	Field Field
	Desc  bool
}

// Expr is a WHERE expression.
type Expr interface{ isExpr() }

// Binary is an AND/OR combination.
type Binary struct {
	Op    string // AND | OR
	Left  Expr
	Right Expr
}

// Paren wraps a parenthesised sub-expression.
type Paren struct{ Inner Expr }

// Comparison is `field OP value`.
type Comparison struct {
	Field Field
	Op    string // = != < > <= >=
	Value Value
}

func (Binary) isExpr()     {}
func (Paren) isExpr()      {}
func (Comparison) isExpr() {}

// ValueKind discriminates a comparison value.
type ValueKind int

const (
	ValString ValueKind = iota
	ValNumber
	ValTime // now() [- duration]
)

// Value is the right-hand side of a comparison.
type Value struct {
	Kind        ValueKind
	Str         string
	Num         string
	TimeSeconds int64 // offset magnitude for now() ± duration
	TimeSub     bool  // true for now() - dur
}

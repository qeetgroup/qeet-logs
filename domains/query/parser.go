package query

import (
	"fmt"
	"strconv"
	"strings"
)

type parser struct {
	toks []token
	pos  int
}

// Parse turns a LogQL++ string into a Stmt.
func Parse(input string) (*Stmt, error) {
	toks, err := lex(input)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	stmt, err := p.statement()
	if err != nil {
		return nil, err
	}
	if p.cur().kind != tEOF {
		return nil, fmt.Errorf("unexpected trailing input near %q", p.cur().text)
	}
	return stmt, nil
}

func (p *parser) cur() token  { return p.toks[p.pos] }
func (p *parser) next() token { t := p.toks[p.pos]; p.pos++; return t }

func (p *parser) isKw(s string) bool {
	return p.cur().kind == tIdent && strings.EqualFold(p.cur().text, s)
}

func (p *parser) expectKw(s string) error {
	if !p.isKw(s) {
		return fmt.Errorf("expected %q, got %q", s, p.cur().text)
	}
	p.next()
	return nil
}

func (p *parser) statement() (*Stmt, error) {
	switch {
	case p.isKw("SEARCH"):
		return p.searchStmt()
	case p.isKw("SELECT"):
		return p.selectStmt()
	case p.isKw("TAIL"):
		return p.tailStmt()
	default:
		return nil, fmt.Errorf("expected SEARCH, SELECT or TAIL, got %q", p.cur().text)
	}
}

func (p *parser) searchStmt() (*Stmt, error) {
	p.next() // SEARCH
	if p.cur().kind != tString {
		return nil, fmt.Errorf("SEARCH must be followed by a quoted string")
	}
	s := &Stmt{Kind: KindSearch, Search: p.next().text}
	if err := p.fromClause(s); err != nil {
		return nil, err
	}
	if err := p.optWhere(s); err != nil {
		return nil, err
	}
	if err := p.optLimit(s); err != nil {
		return nil, err
	}
	return s, nil
}

func (p *parser) tailStmt() (*Stmt, error) {
	p.next() // TAIL
	s := &Stmt{Kind: KindTail}
	if err := p.fromClause(s); err != nil {
		return nil, err
	}
	if err := p.optWhere(s); err != nil {
		return nil, err
	}
	return s, nil
}

func (p *parser) selectStmt() (*Stmt, error) {
	p.next() // SELECT
	s := &Stmt{Kind: KindSelect}
	items, err := p.selectList()
	if err != nil {
		return nil, err
	}
	s.Select = items
	if err := p.fromClause(s); err != nil {
		return nil, err
	}
	if err := p.optWhere(s); err != nil {
		return nil, err
	}
	if p.isKw("GROUP") {
		p.next()
		if err := p.expectKw("BY"); err != nil {
			return nil, err
		}
		for {
			f, err := p.field()
			if err != nil {
				return nil, err
			}
			s.GroupBy = append(s.GroupBy, f)
			if p.cur().kind != tComma {
				break
			}
			p.next()
		}
	}
	if p.isKw("ORDER") {
		p.next()
		if err := p.expectKw("BY"); err != nil {
			return nil, err
		}
		f, err := p.field()
		if err != nil {
			return nil, err
		}
		oi := &OrderItem{Field: f}
		if p.isKw("DESC") {
			oi.Desc = true
			p.next()
		} else if p.isKw("ASC") {
			p.next()
		}
		s.OrderBy = oi
	}
	if err := p.optLimit(s); err != nil {
		return nil, err
	}
	return s, nil
}

func (p *parser) selectList() ([]SelectItem, error) {
	var items []SelectItem
	for {
		if p.cur().kind == tStar {
			p.next()
			items = append(items, SelectItem{Star: true})
		} else {
			it, err := p.selectItem()
			if err != nil {
				return nil, err
			}
			items = append(items, it)
		}
		if p.cur().kind != tComma {
			break
		}
		p.next()
	}
	return items, nil
}

func (p *parser) selectItem() (SelectItem, error) {
	// aggregate? IDENT '(' ...
	if p.cur().kind == tIdent && p.toks[p.pos+1].kind == tLParen {
		agg := strings.ToLower(p.next().text)
		if !isAgg(agg) {
			return SelectItem{}, fmt.Errorf("unknown function %q", agg)
		}
		p.next() // (
		var it SelectItem
		it.Agg = agg
		if p.cur().kind == tStar {
			p.next()
			it.Star = true
		} else {
			f, err := p.field()
			if err != nil {
				return SelectItem{}, err
			}
			it.Field = f
		}
		if p.cur().kind != tRParen {
			return SelectItem{}, fmt.Errorf("expected ) after %s(", agg)
		}
		p.next() // )
		p.optAlias(&it)
		return it, nil
	}
	f, err := p.field()
	if err != nil {
		return SelectItem{}, err
	}
	it := SelectItem{Field: f}
	p.optAlias(&it)
	return it, nil
}

func (p *parser) optAlias(it *SelectItem) {
	if p.isKw("AS") {
		p.next()
		if p.cur().kind == tIdent {
			it.Alias = p.next().text
		}
	}
}

func (p *parser) fromClause(s *Stmt) error {
	if err := p.expectKw("FROM"); err != nil {
		return err
	}
	if p.cur().kind != tIdent {
		return fmt.Errorf("expected table name after FROM")
	}
	s.Table = strings.ToLower(p.next().text)
	return nil
}

func (p *parser) optWhere(s *Stmt) error {
	if !p.isKw("WHERE") {
		return nil
	}
	p.next()
	e, err := p.orExpr()
	if err != nil {
		return err
	}
	s.Where = e
	return nil
}

func (p *parser) optLimit(s *Stmt) error {
	if !p.isKw("LIMIT") {
		return nil
	}
	p.next()
	if p.cur().kind != tNumber {
		return fmt.Errorf("LIMIT must be a number")
	}
	n, err := strconv.Atoi(p.next().text)
	if err != nil {
		return fmt.Errorf("invalid LIMIT: %w", err)
	}
	s.Limit = n
	return nil
}

// orExpr := andExpr (OR andExpr)*
func (p *parser) orExpr() (Expr, error) {
	left, err := p.andExpr()
	if err != nil {
		return nil, err
	}
	for p.isKw("OR") {
		p.next()
		right, err := p.andExpr()
		if err != nil {
			return nil, err
		}
		left = Binary{Op: "OR", Left: left, Right: right}
	}
	return left, nil
}

// andExpr := primary (AND primary)*
func (p *parser) andExpr() (Expr, error) {
	left, err := p.primary()
	if err != nil {
		return nil, err
	}
	for p.isKw("AND") {
		p.next()
		right, err := p.primary()
		if err != nil {
			return nil, err
		}
		left = Binary{Op: "AND", Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) primary() (Expr, error) {
	if p.cur().kind == tLParen {
		p.next()
		e, err := p.orExpr()
		if err != nil {
			return nil, err
		}
		if p.cur().kind != tRParen {
			return nil, fmt.Errorf("expected ) to close expression")
		}
		p.next()
		return Paren{Inner: e}, nil
	}
	return p.comparison()
}

func (p *parser) comparison() (Expr, error) {
	f, err := p.field()
	if err != nil {
		return nil, err
	}
	if p.cur().kind != tOp {
		return nil, fmt.Errorf("expected comparison operator after %q", strings.Join(f.Parts, "."))
	}
	op := p.next().text
	v, err := p.value()
	if err != nil {
		return nil, err
	}
	return Comparison{Field: f, Op: op, Value: v}, nil
}

func (p *parser) field() (Field, error) {
	if p.cur().kind != tIdent {
		return Field{}, fmt.Errorf("expected field name, got %q", p.cur().text)
	}
	parts := []string{p.next().text}
	for p.cur().kind == tDot {
		p.next()
		if p.cur().kind != tIdent {
			return Field{}, fmt.Errorf("expected field segment after '.'")
		}
		parts = append(parts, p.next().text)
	}
	return Field{Parts: parts}, nil
}

func (p *parser) value() (Value, error) {
	switch p.cur().kind {
	case tString:
		return Value{Kind: ValString, Str: p.next().text}, nil
	case tNumber:
		return Value{Kind: ValNumber, Num: p.next().text}, nil
	case tIdent:
		if strings.EqualFold(p.cur().text, "now") {
			return p.timeValue()
		}
		return Value{}, fmt.Errorf("unexpected identifier %q in value position", p.cur().text)
	default:
		return Value{}, fmt.Errorf("expected a value, got %q", p.cur().text)
	}
}

// timeValue := now() [ ('-'|'+') DURATION ]
func (p *parser) timeValue() (Value, error) {
	p.next() // now
	if p.cur().kind != tLParen {
		return Value{}, fmt.Errorf("expected ( after now")
	}
	p.next()
	if p.cur().kind != tRParen {
		return Value{}, fmt.Errorf("expected ) after now(")
	}
	p.next()
	v := Value{Kind: ValTime}
	if p.cur().kind == tMinus || p.cur().kind == tPlus {
		sub := p.next().kind == tMinus
		if p.cur().kind != tDuration {
			return Value{}, fmt.Errorf("expected a duration after now() %s", map[bool]string{true: "-", false: "+"}[sub])
		}
		secs, err := durationSeconds(p.next().text)
		if err != nil {
			return Value{}, err
		}
		v.TimeSeconds = secs
		v.TimeSub = sub
	}
	return v, nil
}

func durationSeconds(d string) (int64, error) {
	if len(d) < 2 {
		return 0, fmt.Errorf("invalid duration %q", d)
	}
	n, err := strconv.ParseInt(d[:len(d)-1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q", d)
	}
	switch d[len(d)-1] {
	case 's':
		return n, nil
	case 'm':
		return n * 60, nil
	case 'h':
		return n * 3600, nil
	case 'd':
		return n * 86400, nil
	}
	return 0, fmt.Errorf("invalid duration unit in %q", d)
}

func isAgg(s string) bool {
	switch s {
	case "count", "sum", "avg", "min", "max":
		return true
	}
	return false
}

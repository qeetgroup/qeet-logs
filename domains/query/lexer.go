package query

import (
	"fmt"
	"strings"
)

type tokKind int

const (
	tEOF tokKind = iota
	tIdent
	tString // '...' or "..."
	tNumber
	tDuration // 1h, 30m, 7d, 15s
	tOp       // = != < > <= >=
	tLParen
	tRParen
	tComma
	tStar
	tDot
	tMinus
	tPlus
)

type token struct {
	kind tokKind
	text string
}

// lex tokenises a LogQL++ statement. Keywords are not distinguished here; the
// parser interprets identifiers positionally (case-insensitively).
func lex(input string) ([]token, error) {
	var toks []token
	r := []rune(input)
	i := 0
	for i < len(r) {
		c := r[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '(':
			toks = append(toks, token{tLParen, "("})
			i++
		case c == ')':
			toks = append(toks, token{tRParen, ")"})
			i++
		case c == ',':
			toks = append(toks, token{tComma, ","})
			i++
		case c == '*':
			toks = append(toks, token{tStar, "*"})
			i++
		case c == '.':
			toks = append(toks, token{tDot, "."})
			i++
		case c == '-':
			toks = append(toks, token{tMinus, "-"})
			i++
		case c == '+':
			toks = append(toks, token{tPlus, "+"})
			i++
		case c == '\'' || c == '"':
			s, n, err := lexString(r[i:], c)
			if err != nil {
				return nil, err
			}
			toks = append(toks, token{tString, s})
			i += n
		case c == '=' || c == '!' || c == '<' || c == '>':
			op, n, err := lexOp(r[i:])
			if err != nil {
				return nil, err
			}
			toks = append(toks, token{tOp, op})
			i += n
		case isDigit(c):
			tok, n := lexNumber(r[i:])
			toks = append(toks, tok)
			i += n
		case isIdentStart(c):
			j := i + 1
			for j < len(r) && isIdentPart(r[j]) {
				j++
			}
			toks = append(toks, token{tIdent, string(r[i:j])})
			i = j
		default:
			return nil, fmt.Errorf("unexpected character %q", string(c))
		}
	}
	toks = append(toks, token{tEOF, ""})
	return toks, nil
}

func lexString(r []rune, quote rune) (string, int, error) {
	var b strings.Builder
	i := 1 // skip opening quote
	for i < len(r) {
		c := r[i]
		if c == '\\' && i+1 < len(r) {
			b.WriteRune(r[i+1])
			i += 2
			continue
		}
		if c == quote {
			return b.String(), i + 1, nil
		}
		b.WriteRune(c)
		i++
	}
	return "", 0, fmt.Errorf("unterminated string literal")
}

func lexOp(r []rune) (string, int, error) {
	if len(r) >= 2 && (r[1] == '=') {
		switch r[0] {
		case '!', '<', '>', '=':
			return string(r[0:2]), 2, nil
		}
	}
	switch r[0] {
	case '=', '<', '>':
		return string(r[0:1]), 1, nil
	}
	return "", 0, fmt.Errorf("invalid operator near %q", string(r[0]))
}

// lexNumber reads digits, then an optional time unit (s/m/h/d) → duration.
func lexNumber(r []rune) (token, int) {
	j := 0
	for j < len(r) && isDigit(r[j]) {
		j++
	}
	if j < len(r) {
		u := r[j]
		if u == 's' || u == 'm' || u == 'h' || u == 'd' {
			// Only a duration if not followed by more identifier chars (e.g. "1day").
			if j+1 >= len(r) || !isIdentPart(r[j+1]) {
				return token{tDuration, string(r[:j+1])}, j + 1
			}
		}
	}
	return token{tNumber, string(r[:j])}, j
}

func isDigit(c rune) bool      { return c >= '0' && c <= '9' }
func isIdentStart(c rune) bool { return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isIdentPart(c rune) bool  { return isIdentStart(c) || isDigit(c) }

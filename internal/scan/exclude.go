package scan

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Exclude holds compiled exclude patterns; the zero value excludes nothing.
type Exclude struct {
	patterns []string
	tests    []patternTest
	every    bool
}

// Verdict is what an exclude test decided for one path.
type Verdict uint8

const (
	Kept Verdict = iota
	Excluded
)

// PatternError is a refused pattern; Index is its position in NewExclude's input.
type PatternError struct {
	Index   int
	Pattern string
	Reason  string
}

func (e *PatternError) Error() string {
	return fmt.Sprintf("scan: exclude pattern %q %s", e.Pattern, e.Reason)
}

// NewExclude compiles patterns as ncdu 1.x matches them (architecture/cli.md).
func NewExclude(patterns []string) (Exclude, error) {
	if len(patterns) == 0 {
		return Exclude{}, nil
	}
	e := Exclude{patterns: append([]string(nil), patterns...)}
	for i, pattern := range patterns {
		if err := refuse(pattern); err != nil {
			return Exclude{}, &PatternError{Index: i, Pattern: pattern, Reason: err.Error()}
		}
		test := compile(pattern)
		e.tests = append(e.tests, test)
		e.every = e.every || test.kind == testEvery
	}
	return e, nil
}

// Match tests a slash-form absolute path and its suffixes (architecture/scan.md).
func (e Exclude) Match(abs string) Verdict {
	if e.every {
		var buf [32]int
		return e.match(abs, suffixStarts(abs, buf[:0]))
	}
	return e.match(abs, nil)
}

func (e Exclude) match(abs string, starts []int) Verdict {
	for _, test := range e.tests {
		if test.match(abs, starts) == matched {
			return Excluded
		}
	}
	return Kept
}

// suffixStarts appends 0 and the offset after every '/' that begins a suffix.
func suffixStarts(abs string, starts []int) []int {
	starts = append(starts, 0)
	for i := strings.IndexByte(abs, '/'); i >= 0 && i < len(abs)-1; {
		if abs[i+1] != '/' {
			starts = append(starts, i+1)
		}
		next := strings.IndexByte(abs[i+1:], '/')
		if next < 0 {
			break
		}
		i += 1 + next
	}
	return starts
}

func (e Exclude) Patterns() []string {
	return append([]string(nil), e.patterns...)
}

// outcome is what matching one pattern against one string decided.
type outcome uint8

const (
	mismatch outcome = iota
	matched
)

func outcomeOf(equal bool) outcome {
	if equal {
		return matched
	}
	return mismatch
}

// testKind is the cheapest test equivalent to fnmatch over every suffix.
type testKind uint8

const (
	testName  testKind = iota // no metacharacter: the path or a suffix equals it
	testTail                  // '*' then no metacharacter: the path ends in the rest
	testWhole                 // a leading '*' or '/': only the whole path can match
	testEvery                 // the whole path and every suffix after a '/'
)

type patternTest struct {
	kind    testKind
	pattern string
	text    string // "/"+name, the literal tail, or the literal head, by kind
}

func compile(pattern string) patternTest {
	switch {
	case !strings.ContainsAny(pattern, `*?[\`):
		return patternTest{kind: testName, pattern: pattern, text: "/" + pattern}
	case pattern[0] == '*' && !strings.ContainsAny(pattern[1:], `*?[\`):
		return patternTest{kind: testTail, pattern: pattern, text: pattern[1:]}
	case pattern[0] == '*' || pattern[0] == '/':
		return patternTest{kind: testWhole, pattern: pattern}
	}
	head := pattern[:strings.IndexAny(pattern, `*?[\`)]
	return patternTest{kind: testEvery, pattern: pattern, text: head}
}

// match tests abs; only testEvery reads starts, the suffix offsets.
func (t patternTest) match(abs string, starts []int) outcome {
	switch t.kind {
	case testName:
		return outcomeOf(abs == t.pattern || strings.HasSuffix(abs, t.text))
	case testTail:
		return outcomeOf(strings.HasSuffix(abs, t.text))
	case testWhole:
		return fnmatch(t.pattern, abs)
	}
	for _, at := range starts {
		if strings.HasPrefix(abs[at:], t.text) && fnmatch(t.pattern, abs[at:]) == matched {
			return matched
		}
	}
	return mismatch
}

// fnmatch matches as glibc fnmatch(pattern, name, 0) does: per rune, or per
// byte when either string is not valid UTF-8 (architecture/cli.md).
func fnmatch(pattern, name string) outcome {
	m := matcher{pattern: pattern, name: name, bytewise: !utf8.ValidString(pattern) || !utf8.ValidString(name)}
	return m.run()
}

type matcher struct {
	pattern  string
	name     string
	bytewise bool
}

// cursor is a position in the pattern and in the name.
type cursor struct{ p, n int }

func (m matcher) decode(s string, i int) (rune, int) {
	if m.bytewise {
		return rune(s[i]), 1
	}
	return utf8.DecodeRuneInString(s[i:])
}

func (m matcher) run() outcome {
	at := cursor{}
	star, mark := -1, 0
	for at.p < len(m.pattern) || at.n < len(m.name) {
		if at.p < len(m.pattern) && m.pattern[at.p] == '*' {
			star, mark = at.p, at.n
			at.p++
			continue
		}
		if at.p < len(m.pattern) && at.n < len(m.name) {
			if next, result := m.step(at); result == matched {
				at = next
				continue
			}
		}
		if star < 0 || mark >= len(m.name) {
			return mismatch
		}
		_, w := m.decode(m.name, mark)
		mark += w
		at = cursor{p: star + 1, n: mark}
	}
	return matched
}

// step matches the pattern element at at.p against the character at at.n.
func (m matcher) step(at cursor) (cursor, outcome) {
	r, w := m.decode(m.name, at.n)
	next := cursor{n: at.n + w}
	switch m.pattern[at.p] {
	case '?':
		next.p = at.p + 1
		return next, matched
	case '\\':
		if at.p+1 == len(m.pattern) {
			return at, mismatch
		}
		c, cw := m.decode(m.pattern, at.p+1)
		next.p = at.p + 1 + cw
		return next, outcomeOf(c == r)
	case '[':
		b := m.bracket(at.p, r)
		switch b.form {
		case bracketHit:
			next.p = b.end
			return next, matched
		case bracketLiteral:
			next.p = at.p + 1
			return next, outcomeOf(r == '[')
		}
		return at, mismatch
	}
	c, cw := m.decode(m.pattern, at.p)
	next.p = at.p + cw
	return next, outcomeOf(c == r)
}

// bracketForm is what a bracket expression decided for one character.
type bracketForm uint8

const (
	bracketHit          bracketForm = iota // the character is in the set
	bracketMiss                            // the character is not in the set
	bracketLiteral                         // no closing ']': the '[' is a literal
	bracketDead                            // a range, an escape or the skip runs off the end
	bracketUnknownClass                    // [:name:] names no class
	bracketCollating                       // [.x.] or [=x=], which duls does not implement
)

type bracketResult struct {
	end   int // just past the closing ']'
	form  bracketForm
	class string // the unknown [:name:]
}

// noRune tests no character: the bracket is only parsed.
const noRune rune = -1

// bracket evaluates the bracket at pattern[p] for r the way glibc does: members
// up to the first hit, then glibc's skip to the closing ']'.
func (m matcher) bracket(p int, r rune) bracketResult {
	pat := m.pattern
	i := p + 1
	negate := i < len(pat) && (pat[i] == '!' || pat[i] == '^')
	if negate {
		i++
	}
	for first := true; ; first = false {
		if i >= len(pat) {
			return bracketResult{form: bracketLiteral}
		}
		if pat[i] == ']' && !first {
			return bracketResult{end: i + 1, form: hitIf(negate)}
		}
		if opens(pat, i, ':') {
			if span := classSpan(pat, i); span.form == spanClass {
				in, err := classOf(span.name)
				if err != nil {
					return bracketResult{form: bracketUnknownClass, class: pat[i:span.end]}
				}
				if r != noRune && (!m.bytewise || r < utf8.RuneSelf) && in(r) {
					return m.skip(span.end, negate)
				}
				i = span.end
				continue
			}
		}
		if opens(pat, i, '.') || opens(pat, i, '=') {
			return bracketResult{form: bracketCollating}
		}
		lo, next, err := m.member(i)
		if err != nil {
			return bracketResult{form: bracketDead}
		}
		i = next
		hi := lo
		if i < len(pat) && pat[i] == '-' && (i+1 == len(pat) || pat[i+1] != ']') {
			if opens(pat, i+1, '.') || opens(pat, i+1, '=') {
				return bracketResult{form: bracketCollating}
			}
			if hi, next, err = m.member(i + 1); err != nil {
				return bracketResult{form: bracketDead}
			}
			i = next
		}
		if lo <= r && r <= hi {
			return m.skip(i, negate)
		}
	}
}

func hitIf(in bool) bracketForm {
	if in {
		return bracketHit
	}
	return bracketMiss
}

// opens reports whether pattern[i:] begins with '[' and then delim.
func opens(pattern string, i int, delim byte) bool {
	return i+1 < len(pattern) && pattern[i] == '[' && pattern[i+1] == delim
}

// member reads one bracket member at i and its escape; glibc never matches a
// bracket that runs off the end here.
func (m matcher) member(i int) (rune, int, error) {
	if i >= len(m.pattern) || m.pattern[i] == '\\' && i+1 == len(m.pattern) {
		return 0, i, errors.New("the bracket runs off the end of the pattern")
	}
	if m.pattern[i] == '\\' {
		c, w := m.decode(m.pattern, i+1)
		return c, i + 1 + w, nil
	}
	c, w := m.decode(m.pattern, i)
	return c, i + w, nil
}

// skip moves past the rest of a bracket that already matched, as glibc does:
// an escape and a [:class:] are stepped over whole.
func (m matcher) skip(i int, negate bool) bracketResult {
	pat := m.pattern
	for {
		if i >= len(pat) {
			return bracketResult{form: bracketDead}
		}
		c := pat[i]
		i++
		switch {
		case c == '\\':
			if i >= len(pat) {
				return bracketResult{form: bracketDead}
			}
			_, w := m.decode(pat, i)
			i += w
			continue
		case c == '[' && i < len(pat) && pat[i] == ':':
			if span := classSpan(pat, i-1); span.form == spanClass {
				i = span.end
				continue
			}
		}
		if c == ']' {
			return bracketResult{end: i, form: hitIf(!negate)}
		}
	}
}

// spanForm says whether "[:" at a position opens a class name.
type spanForm uint8

const (
	spanMember spanForm = iota // not a class: the '[' is an ordinary member
	spanClass
)

type span struct {
	name string
	end  int // just past ":]"
	form spanForm
}

// classSpan reads the class name after "[:" at i, as glibc does: letters from
// 'a' to 'y' up to ":]"; anything else leaves the '[' an ordinary member.
func classSpan(pattern string, i int) span {
	for j := i + 2; j < len(pattern); j++ {
		c := pattern[j]
		if c == ':' && j+1 < len(pattern) && pattern[j+1] == ']' {
			return span{name: pattern[i+2 : j], end: j + 2, form: spanClass}
		}
		if c < 'a' || c >= 'z' {
			break
		}
	}
	return span{form: spanMember}
}

func classOf(name string) (func(rune) bool, error) {
	switch name {
	case "alnum":
		return func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }, nil
	case "alpha":
		return unicode.IsLetter, nil
	case "blank":
		return func(r rune) bool { return r == ' ' || r == '\t' }, nil
	case "cntrl":
		return unicode.IsControl, nil
	case "digit":
		return func(r rune) bool { return '0' <= r && r <= '9' }, nil
	case "graph":
		return func(r rune) bool { return unicode.IsGraphic(r) && !unicode.IsSpace(r) }, nil
	case "lower":
		return unicode.IsLower, nil
	case "print":
		return func(r rune) bool { return unicode.IsGraphic(r) && !unicode.IsControl(r) }, nil
	case "punct":
		return func(r rune) bool {
			return unicode.IsGraphic(r) && !unicode.IsSpace(r) && !unicode.IsLetter(r) && !unicode.IsDigit(r)
		}, nil
	case "space":
		return unicode.IsSpace, nil
	case "upper":
		return unicode.IsUpper, nil
	case "xdigit":
		return func(r rune) bool {
			return '0' <= r && r <= '9' || 'a' <= r && r <= 'f' || 'A' <= r && r <= 'F'
		}, nil
	}
	return nil, fmt.Errorf("names no class: %s", name)
}

// tokenKind is what one pattern element means to the refusal rules.
type tokenKind uint8

const (
	tokenLiteral tokenKind = iota
	tokenSlash
	tokenGlob
)

type token struct {
	kind tokenKind
	r    rune
	end  int
}

// refuse says why no cleaned path can match pattern, or which syntax duls does
// not implement (architecture/cli.md).
func refuse(pattern string) error {
	if pattern == "" {
		return errors.New("is empty")
	}
	m := matcher{pattern: pattern, bytewise: !utf8.ValidString(pattern)}
	var seg segment
	sawSlash := false
	for i := 0; i < len(pattern); {
		tok, err := m.token(i)
		if err != nil {
			return err
		}
		switch tok.kind {
		case tokenSlash:
			if err := seg.close(sawSlash); err != nil {
				return err
			}
			seg, sawSlash = segment{}, true
		case tokenGlob:
			seg.globs = true
		default:
			seg.text.WriteRune(tok.r)
		}
		i = tok.end
	}
	if sawSlash && seg.empty() {
		return errors.New("can never match a path; drop the trailing '/'")
	}
	return seg.dots()
}

// token reads the pattern element at i. A bracket that holds only '/' or '.'
// is that literal, because it matches nothing else.
func (m matcher) token(i int) (token, error) {
	pat := m.pattern
	switch pat[i] {
	case '\\':
		if i+1 == len(pat) {
			return token{}, errors.New(`can never match a path; escape the trailing '\' as '\\'`)
		}
		r, w := m.decode(pat, i+1)
		return literalToken(r, i+1+w), nil
	case '*', '?':
		return token{kind: tokenGlob, end: i + 1}, nil
	case '[':
		b := m.bracket(i, noRune)
		switch b.form {
		case bracketLiteral:
			return token{kind: tokenLiteral, r: '[', end: i + 1}, nil
		case bracketUnknownClass:
			return token{}, errors.New("names an unknown character class " + b.class)
		case bracketCollating:
			return token{}, errors.New("uses a collating symbol or an equivalence class ([.x.] or [=x=]), which duls does not implement")
		case bracketDead:
			return token{}, errors.New("can never match a path; its bracket runs off the end of the pattern")
		}
		switch pat[i:b.end] {
		case "[/]", `[\/]`:
			return token{kind: tokenSlash, end: b.end}, nil
		case "[.]", `[\.]`:
			return token{kind: tokenLiteral, r: '.', end: b.end}, nil
		}
		return token{kind: tokenGlob, end: b.end}, nil
	}
	r, w := m.decode(pat, i)
	return literalToken(r, i+w), nil
}

func literalToken(r rune, end int) token {
	if r == '/' {
		return token{kind: tokenSlash, end: end}
	}
	return token{kind: tokenLiteral, r: r, end: end}
}

// segment is the pattern text between two literal '/'.
type segment struct {
	text  strings.Builder
	globs bool
}

func (s *segment) empty() bool { return !s.globs && s.text.Len() == 0 }

func (s *segment) dots() error {
	if text := s.text.String(); !s.globs && (text == "." || text == "..") {
		return errors.New("can never match a path; drop its '.' and '..' segments")
	}
	return nil
}

func (s *segment) close(sawSlash bool) error {
	if sawSlash && s.empty() {
		return errors.New("can never match a path; use a single '/'")
	}
	return s.dots()
}

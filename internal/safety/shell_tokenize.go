package safety

import (
	"fmt"
	"strings"
)

// QuoteKind records the quoting context of a run of source bytes within a
// shell word. Bytes produced by a bare backslash escape in unquoted context
// are reported as QuoteNone: they are literal, but not quote-protected.
type QuoteKind uint8

const (
	QuoteNone QuoteKind = iota
	QuoteSingle
	QuoteDouble
)

// Segment is a run of decoded bytes that share one quoting context. A word
// built from adjacent quoting (e.g. "a"'b'c) yields several segments.
type Segment struct {
	Text  string
	Quote QuoteKind
}

// TokenKind distinguishes words from shell operator tokens.
type TokenKind uint8

const (
	TokenWord TokenKind = iota
	TokenOp
)

// Token is one lexical unit of a shell command line.
//
// For words, Value is the decoded text (quotes and escapes resolved, no
// expansions performed) and Segments preserves which bytes came from which
// quoting context. Raw is the exact source slice. For operators, Value is the
// literal operator text.
type Token struct {
	Kind     TokenKind
	Value    string
	Raw      string
	Segments []Segment
}

// Tokenize lexically splits a shell command line into words and operators.
//
// It is a pure scanner: it performs no expansion, no execution, and never
// touches the filesystem or spawns a process. The result is deterministic for
// a given input. Unterminated quotes are returned as an error rather than
// being swallowed.
func Tokenize(src string) ([]Token, error) {
	s := &shellScanner{src: src}
	return s.run()
}

type shellScanner struct {
	src string
	i   int
}

func (s *shellScanner) run() ([]Token, error) {
	toks := []Token{}
	for {
		s.skipSpace()
		if s.i >= len(s.src) {
			return toks, nil
		}
		if op, ok := s.scanFdRedirect(); ok {
			toks = append(toks, Token{Kind: TokenOp, Value: op, Raw: op})
			continue
		}
		if op, ok := s.scanOp(); ok {
			toks = append(toks, Token{Kind: TokenOp, Value: op, Raw: op})
			continue
		}
		tok, err := s.scanWord()
		if err != nil {
			return nil, err
		}
		toks = append(toks, tok)
	}
}

// skipSpace consumes unquoted whitespace. A backslash-newline pair is a line
// continuation: it is consumed here so it never starts a token.
func (s *shellScanner) skipSpace() {
	for s.i < len(s.src) {
		c := s.src[s.i]
		switch {
		case isSpaceByte(c):
			s.i++
		case c == '\\' && s.i+1 < len(s.src) && s.src[s.i+1] == '\n':
			s.i += 2
		default:
			return
		}
	}
}

// scanFdRedirect recognizes a file-descriptor-prefixed redirection such as 2>.
// It only fires when the scan starts on digits immediately followed by < or >,
// so a bare number stays a word.
func (s *shellScanner) scanFdRedirect() (string, bool) {
	j := s.i
	for j < len(s.src) && s.src[j] >= '0' && s.src[j] <= '9' {
		j++
	}
	if j == s.i || j >= len(s.src) {
		return "", false
	}
	if s.src[j] != '<' && s.src[j] != '>' {
		return "", false
	}
	prefix := s.src[s.i:j]
	s.i = j
	op, _ := s.scanOp() // always true for < or >
	return prefix + op, true
}

// scanOp consumes an operator token if one starts at the cursor. Every
// operator byte except ; doubles up (&& || >> <<).
func (s *shellScanner) scanOp() (string, bool) {
	if s.i >= len(s.src) || !isOpByte(s.src[s.i]) {
		return "", false
	}
	n := 1
	if c := s.src[s.i]; c != ';' && s.i+1 < len(s.src) && s.src[s.i+1] == c {
		n = 2
	}
	op := s.src[s.i : s.i+n]
	s.i += n
	return op, true
}

func isOpByte(b byte) bool {
	switch b {
	case ';', '&', '|', '<', '>':
		return true
	}
	return false
}

func isSpaceByte(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }

// scanWord consumes a word, resolving quotes and escapes. It appends at least
// one token even for an empty word ("" produces a word with empty Value).
func (s *shellScanner) scanWord() (Token, error) {
	start := s.i
	var segs []Segment
	addSeg := func(text string, q QuoteKind) {
		if text == "" {
			return
		}
		if n := len(segs); n > 0 && segs[n-1].Quote == q {
			segs[n-1].Text += text
			return
		}
		segs = append(segs, Segment{Text: text, Quote: q})
	}

	for s.i < len(s.src) {
		c := s.src[s.i]
		if isSpaceByte(c) || isOpByte(c) {
			break
		}
		switch c {
		case '\\':
			if s.i+1 >= len(s.src) {
				addSeg("\\", QuoteNone)
				s.i++
				continue
			}
			if s.src[s.i+1] == '\n' {
				s.i += 2
				continue
			}
			// a byte slice, not string(byte): that would re-encode a UTF-8
			// lead byte as a rune and garble the segment text
			addSeg(s.src[s.i+1:s.i+2], QuoteNone)
			s.i += 2
		case '\'':
			open := s.i
			s.i++
			segStart := s.i
			for s.i < len(s.src) && s.src[s.i] != '\'' {
				s.i++
			}
			if s.i >= len(s.src) {
				return Token{}, fmt.Errorf("safety: unterminated single quote at byte %d", open)
			}
			addSeg(s.src[segStart:s.i], QuoteSingle)
			s.i++ // closing quote
		case '"':
			open := s.i
			s.i++
			var dq strings.Builder
			for s.i < len(s.src) {
				ch := s.src[s.i]
				if ch == '"' {
					break
				}
				if ch == '\\' && s.i+1 < len(s.src) {
					nc := s.src[s.i+1]
					if nc == '"' || nc == '\\' || nc == '$' || nc == '`' {
						dq.WriteByte(nc)
						s.i += 2
						continue
					}
					if nc == '\n' {
						s.i += 2
						continue
					}
				}
				dq.WriteByte(ch)
				s.i++
			}
			if s.i >= len(s.src) {
				return Token{}, fmt.Errorf("safety: unterminated double quote at byte %d", open)
			}
			addSeg(dq.String(), QuoteDouble)
			s.i++ // closing quote
		default:
			runStart := s.i
			for s.i < len(s.src) {
				bc := s.src[s.i]
				if isSpaceByte(bc) || isOpByte(bc) || bc == '\\' || bc == '\'' || bc == '"' {
					break
				}
				s.i++
			}
			addSeg(s.src[runStart:s.i], QuoteNone)
		}
	}
	var val strings.Builder
	for _, sg := range segs {
		val.WriteString(sg.Text)
	}
	return Token{Kind: TokenWord, Value: val.String(), Raw: s.src[start:s.i], Segments: segs}, nil
}

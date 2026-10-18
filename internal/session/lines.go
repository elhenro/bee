package session

import (
	"bufio"
	"bytes"
	"io"
)

// lineScanner reads JSONL like bufio.Scanner but skips a line longer than
// maxLineBytes instead of failing the whole read: one oversized message (an
// inlined screenshot) must not make the session unreadable.
type lineScanner struct {
	r    *bufio.Reader
	line []byte
	err  error
}

func newLineScanner(r io.Reader) *lineScanner {
	return &lineScanner{r: bufio.NewReaderSize(r, 64*1024)}
}

// Scan advances to the next line that fits. A final line without a newline
// (a torn write) is returned like any other.
func (s *lineScanner) Scan() bool {
	for {
		s.line = s.line[:0]
		tooLong := false
		for {
			chunk, err := s.r.ReadSlice('\n')
			if !tooLong {
				if len(s.line)+len(chunk) > maxLineBytes {
					tooLong = true
					s.line = s.line[:0]
				} else {
					s.line = append(s.line, chunk...)
				}
			}
			if err == bufio.ErrBufferFull {
				continue
			}
			if err != nil {
				if err != io.EOF {
					s.err = err
					return false
				}
				if tooLong || len(s.line) == 0 {
					return false
				}
			}
			break
		}
		if tooLong {
			continue
		}
		s.line = bytes.TrimRight(s.line, "\r\n")
		return true
	}
}

func (s *lineScanner) Bytes() []byte { return s.line }
func (s *lineScanner) Err() error    { return s.err }

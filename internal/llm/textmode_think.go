package llm

import "strings"

var (
	thinkOpenTags  = []string{"<think>", "<thinking>"}
	thinkCloseTags = []string{"</think>", "</thinking>"}
)

// thinkSplitter separates inline reasoning blocks from answer text in a
// streamed content channel. Models served without a reasoning parser put
// their chain of thought in the content itself; a tool-call tag quoted in
// there is a thought, not a call. A tag split across deltas is held back
// until the next feed so it never leaks into either side.
type thinkSplitter struct {
	inThink bool
	hold    string
}

// feed consumes one delta and returns the answer and reasoning parts of
// everything resolvable so far.
func (s *thinkSplitter) feed(delta string) (text, think string) {
	in := s.hold + delta
	s.hold = ""
	var tb, kb strings.Builder
	for in != "" {
		tags, dst := thinkOpenTags, &tb
		if s.inThink {
			tags, dst = thinkCloseTags, &kb
		}
		idx, n := indexAnyTag(in, tags)
		if idx < 0 {
			keep := partialTagSuffix(in, tags)
			dst.WriteString(in[:len(in)-keep])
			s.hold = in[len(in)-keep:]
			break
		}
		dst.WriteString(in[:idx])
		in = in[idx+n:]
		s.inThink = !s.inThink
	}
	return tb.String(), kb.String()
}

// flush releases a held fragment at end of stream. A fragment that never
// completed into a tag is ordinary content of the current mode.
func (s *thinkSplitter) flush() (text, think string) {
	h := s.hold
	s.hold = ""
	if s.inThink {
		return "", h
	}
	return h, ""
}

// indexAnyTag returns the earliest case-insensitive match of any tag in s
// and its length, or -1.
func indexAnyTag(s string, tags []string) (idx, n int) {
	low := asciiLower(s)
	idx = -1
	for _, t := range tags {
		if i := strings.Index(low, t); i >= 0 && (idx < 0 || i < idx) {
			idx, n = i, len(t)
		}
	}
	return idx, n
}

// partialTagSuffix returns the length of the longest suffix of s that is a
// proper prefix of one of tags.
func partialTagSuffix(s string, tags []string) int {
	low := asciiLower(s)
	longest := 0
	for _, t := range tags {
		for k := min(len(low), len(t)-1); k > longest; k-- {
			if strings.HasSuffix(low, t[:k]) {
				longest = k
				break
			}
		}
	}
	return longest
}

// asciiLower folds A-Z only so byte offsets stay valid for non-ASCII input.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

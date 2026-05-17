// Package apply_patch provides patch application primitives plus the
// LINE#ID anchor system used to reference specific source lines.
//
// LINE#ID format: "<lineNumber>#<2-char tag>", e.g. "42#VK". The tag is
// a deterministic content hash of the (normalized) line. Edits that cite
// a tag which no longer matches the live content are rejected, which
// catches stale or hallucinated line numbers before they corrupt files.
//
// Algorithm (ported from oh-my-openagent's hashline-edit):
//  1. normalize: strip \r, trim trailing space/tab.
//  2. seed: 0 if the line contains any Unicode letter or digit, else
//     the 1-based line number (so blanks and pure-punctuation lines
//     still vary by position).
//  3. h = xxhash32(normalized, seed); idx = h % 256.
//  4. tag = alphabet[idx/16] + alphabet[idx%16] using the 16-char set
//     "ZPMQVRWSNKTXJBYH".
package apply_patch

import (
	"regexp"
	"strconv"
	"strings"
)

const hashAlphabet = "ZPMQVRWSNKTXJBYH"

var alnumRe = regexp.MustCompile(`[\p{L}\p{N}]`)

// Tag returns the 2-character content-hash tag for one source line.
// lineNumber is 1-based and used as the xxh seed when the line has no
// alphanumerics (so blank lines and pure-punctuation lines still vary).
func Tag(line string, lineNumber int) string {
	stripped := normalizeLine(line)
	var seed uint32
	if !alnumRe.MatchString(stripped) {
		if lineNumber > 0 {
			seed = uint32(lineNumber)
		}
	}
	h := xxh32([]byte(stripped), seed)
	idx := h % 256
	return string(hashAlphabet[idx/16]) + string(hashAlphabet[idx%16])
}

// Ref renders the standard "<lineNumber>#<tag>" anchor format.
func Ref(line string, lineNumber int) string {
	return strconv.Itoa(lineNumber) + "#" + Tag(line, lineNumber)
}

// TagAll returns Tag for every line in src (split on \n). Index i+1 is
// the 1-based line number passed to Tag.
func TagAll(src string) []string {
	lines := strings.Split(src, "\n")
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = Tag(l, i+1)
	}
	return out
}

func normalizeLine(line string) string {
	if strings.IndexByte(line, '\r') >= 0 {
		line = strings.ReplaceAll(line, "\r", "")
	}
	return strings.TrimRight(line, " \t")
}

// xxh32 is a pure-Go implementation of xxHash32 to match the OMO
// reference exactly (cespare/xxhash/v2 is 64-bit and would produce
// different bytes; only `% 256` would survive identical).
const (
	xxh32Prime1 uint32 = 2654435761
	xxh32Prime2 uint32 = 2246822519
	xxh32Prime3 uint32 = 3266489917
	xxh32Prime4 uint32 = 668265263
	xxh32Prime5 uint32 = 374761393
)

func rotl32(x uint32, r uint) uint32 { return (x << r) | (x >> (32 - r)) }

func xxh32(data []byte, seed uint32) uint32 {
	var h uint32
	n := len(data)
	p := 0

	if n >= 16 {
		v1 := seed + xxh32Prime1 + xxh32Prime2
		v2 := seed + xxh32Prime2
		v3 := seed
		v4 := seed - xxh32Prime1
		for n-p >= 16 {
			v1 = xxh32Round(v1, readU32(data, p))
			v2 = xxh32Round(v2, readU32(data, p+4))
			v3 = xxh32Round(v3, readU32(data, p+8))
			v4 = xxh32Round(v4, readU32(data, p+12))
			p += 16
		}
		h = rotl32(v1, 1) + rotl32(v2, 7) + rotl32(v3, 12) + rotl32(v4, 18)
	} else {
		h = seed + xxh32Prime5
	}

	h += uint32(n)

	for n-p >= 4 {
		h += readU32(data, p) * xxh32Prime3
		h = rotl32(h, 17) * xxh32Prime4
		p += 4
	}
	for p < n {
		h += uint32(data[p]) * xxh32Prime5
		h = rotl32(h, 11) * xxh32Prime1
		p++
	}

	h ^= h >> 15
	h *= xxh32Prime2
	h ^= h >> 13
	h *= xxh32Prime3
	h ^= h >> 16
	return h
}

func xxh32Round(acc, input uint32) uint32 {
	acc += input * xxh32Prime2
	acc = rotl32(acc, 13)
	acc *= xxh32Prime1
	return acc
}

func readU32(b []byte, off int) uint32 {
	return uint32(b[off]) | uint32(b[off+1])<<8 | uint32(b[off+2])<<16 | uint32(b[off+3])<<24
}

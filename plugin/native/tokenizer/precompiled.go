package tokenizer

import (
	"encoding/binary"
	"errors"
	"strings"
	"unicode/utf8"
)

type charsmap struct {
	units      []uint32
	normalized []byte
}

func parseCharsmap(b []byte) (*charsmap, error) {
	if len(b) < 4 {
		return nil, errors.New("the precompiled charsmap is shorter than its own length field")
	}
	n := int(binary.LittleEndian.Uint32(b))
	if n == 0 || n%4 != 0 || 4+n > len(b) {
		return nil, errors.New("the precompiled charsmap's trie length does not fit the table")
	}
	units := make([]uint32, n/4)
	for i := range units {
		units[i] = binary.LittleEndian.Uint32(b[4+4*i:])
	}
	return &charsmap{units: units, normalized: b[4+n:]}, nil
}

func unitHasLeaf(u uint32) bool  { return (u>>8)&1 == 1 }
func unitValue(u uint32) int     { return int(u & (1<<31 - 1)) }
func unitLabel(u uint32) uint32  { return u & (1<<31 | 0xFF) }
func unitOffset(u uint32) uint32 { return (u >> 10) << ((u & (1 << 9)) >> 6) }

func (c *charsmap) shortestPrefix(key string) int {
	pos := uint32(0)
	pos ^= unitOffset(c.units[pos])
	for i := 0; i < len(key); i++ {
		ch := key[i]
		if ch == 0 {
			break
		}
		pos ^= uint32(ch)
		if int(pos) >= len(c.units) {
			return -1
		}
		unit := c.units[pos]
		if unitLabel(unit) != uint32(ch) {
			return -1
		}
		pos ^= unitOffset(unit)
		if unitHasLeaf(unit) {
			if int(pos) >= len(c.units) {
				return -1
			}
			return unitValue(c.units[pos])
		}
	}
	return -1
}

func (c *charsmap) transform(chunk string) (string, bool) {
	at := c.shortestPrefix(chunk)
	if at < 0 || at > len(c.normalized) {
		return "", false
	}
	end := at
	for end < len(c.normalized) && c.normalized[end] != 0 {
		end++
	}
	return string(c.normalized[at:end]), true
}

func (c *charsmap) normalize(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	for len(s) > 0 {
		var g string
		g, s = firstCluster(s)
		if len(g) < 6 {
			if norm, ok := c.transform(g); ok {
				out.WriteString(norm)
				continue
			}
		}
		for len(g) > 0 {
			_, size := utf8.DecodeRuneInString(g)
			if norm, ok := c.transform(g[:size]); ok {
				out.WriteString(norm)
			} else {
				out.WriteString(g[:size])
			}
			g = g[size:]
		}
	}
	return out.String()
}

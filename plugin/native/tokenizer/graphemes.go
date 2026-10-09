package tokenizer

import (
	"sort"
	"unicode/utf8"
)

type runeRange struct{ lo, hi rune }

type graphemeRange struct {
	lo, hi rune
	class  uint8
}

const (
	gcbOther uint8 = iota
	gcbCR
	gcbLF
	gcbControl
	gcbExtend
	gcbZWJ
	gcbRegionalIndicator
	gcbPrepend
	gcbSpacingMark
	gcbL
	gcbV
	gcbT
	gcbLV
	gcbLVT
)

const (
	incbNone uint8 = iota
	incbLinker
	incbConsonant
	incbExtend
)

func classIn(table []graphemeRange, r rune) uint8 {
	i := sort.Search(len(table), func(i int) bool { return table[i].hi >= r })
	if i < len(table) && table[i].lo <= r {
		return table[i].class
	}
	return 0
}

func breakClass(r rune) uint8 {
	if r >= 0xAC00 && r <= 0xD7A3 {
		if (r-0xAC00)%28 == 0 {
			return gcbLV
		}
		return gcbLVT
	}
	return classIn(graphemeBreakRanges, r)
}

func pictographic(r rune) bool {
	t := extendedPictographicRanges
	i := sort.Search(len(t), func(i int) bool { return t[i].hi >= r })
	return i < len(t) && t[i].lo <= r
}

func firstCluster(s string) (cluster, rest string) {
	if s == "" {
		return "", ""
	}
	r, size := utf8.DecodeRuneInString(s)
	end := size
	prev := breakClass(r)

	ri, pict, conj := 0, 0, 0
	note := func(r rune, class uint8) {
		if class == gcbRegionalIndicator {
			ri++
		} else {
			ri = 0
		}
		switch {
		case pictographic(r):
			pict = 1
		case class == gcbExtend && pict == 1:
		case class == gcbZWJ && pict == 1:
			pict = 2
		default:
			pict = 0
		}
		switch classIn(incbRanges, r) {
		case incbConsonant:
			conj = 1
		case incbLinker:
			if conj >= 1 {
				conj = 2
			}
		case incbExtend:

		default:
			conj = 0
		}
	}
	note(r, prev)
	for end < len(s) {
		r, size = utf8.DecodeRuneInString(s[end:])
		class := breakClass(r)
		if breakBetween(prev, class, r, ri, pict, conj) {
			break
		}
		note(r, class)
		prev = class
		end += size
	}
	return s[:end], s[end:]
}

func breakBetween(a, b uint8, r rune, ri, pict, conj int) bool {
	switch {
	case a == gcbCR && b == gcbLF:
		return false
	case a == gcbControl || a == gcbCR || a == gcbLF:
		return true
	case b == gcbControl || b == gcbCR || b == gcbLF:
		return true
	case a == gcbL && (b == gcbL || b == gcbV || b == gcbLV || b == gcbLVT):
		return false
	case (a == gcbLV || a == gcbV) && (b == gcbV || b == gcbT):
		return false
	case (a == gcbLVT || a == gcbT) && b == gcbT:
		return false
	case b == gcbExtend || b == gcbZWJ:
		return false
	case b == gcbSpacingMark:
		return false
	case a == gcbPrepend:
		return false
	case conj == 2 && classIn(incbRanges, r) == incbConsonant:
		return false
	case pict == 2 && pictographic(r):
		return false
	case a == gcbRegionalIndicator && b == gcbRegionalIndicator && ri%2 == 1:
		return false
	}
	return true
}

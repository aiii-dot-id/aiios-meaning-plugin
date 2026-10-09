package tokenizer

import (
	"sort"
	"unicode/utf8"
)

const unkPenalty = 10.0

type pieceTrie struct {
	first []int32
	count []int32
	value []int32
	label []byte
	child []int32
}

func buildTrie(ids map[string]int32) *pieceTrie {
	keys := make([]string, 0, len(ids))
	for k := range ids {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	t := &pieceTrie{}
	t.newNode()
	t.fill(0, keys, ids, 0)
	return t
}

func (t *pieceTrie) newNode() int32 {
	t.first = append(t.first, 0)
	t.count = append(t.count, 0)
	t.value = append(t.value, 0)
	return int32(len(t.first) - 1)
}

func (t *pieceTrie) fill(node int32, keys []string, ids map[string]int32, depth int) {
	if len(keys) > 0 && len(keys[0]) == depth {
		t.value[node] = ids[keys[0]] + 1
		keys = keys[1:]
	}
	type group struct {
		b      byte
		lo, hi int
	}
	var groups []group
	for i := 0; i < len(keys); {
		j := i + 1
		for j < len(keys) && keys[j][depth] == keys[i][depth] {
			j++
		}
		groups = append(groups, group{keys[i][depth], i, j})
		i = j
	}
	t.first[node] = int32(len(t.label))
	t.count[node] = int32(len(groups))
	base := len(t.label)
	for _, g := range groups {
		t.label = append(t.label, g.b)
		t.child = append(t.child, 0)
	}
	for n, g := range groups {
		c := t.newNode()
		t.child[base+n] = c
		t.fill(c, keys[g.lo:g.hi], ids, depth+1)
	}
}

func (t *pieceTrie) step(node int32, b byte) int32 {
	lo, hi := int(t.first[node]), int(t.first[node]+t.count[node])
	for lo < hi {
		mid := (lo + hi) / 2
		switch {
		case t.label[mid] < b:
			lo = mid + 1
		case t.label[mid] > b:
			hi = mid
		default:
			return t.child[mid]
		}
	}
	return -1
}

type unigram struct {
	trie     *pieceTrie
	ids      map[string]int32
	scores   []float64
	unkID    int32
	unkScore float64
}

type pathNode struct {
	id       int32
	score    float64
	startsAt int
}

func (u *unigram) encode(word string, out []int32) []int32 {
	if word == "" {
		return out
	}
	size := len(word)
	best := make([]pathNode, size+1)
	for i := range best {
		best[i].startsAt = -1
	}
	for start := 0; start < size; {
		here := best[start].score
		_, mblen := utf8.DecodeRuneInString(word[start:])
		single := false
		node := int32(0)
		for end := start; end < size; end++ {
			node = u.trie.step(node, word[end])
			if node < 0 {
				break
			}
			if v := u.trie.value[node]; v != 0 {
				id := v - 1
				cand := u.scores[id] + here
				t := &best[end+1]
				if t.startsAt < 0 || cand > t.score {
					t.score, t.startsAt, t.id = cand, start, id
				}
				if end+1-start == mblen {
					single = true
				}
			}
		}
		if !single {
			cand := u.unkScore + here
			t := &best[start+mblen]
			if t.startsAt < 0 || cand > t.score {
				t.score, t.startsAt, t.id = cand, start, u.unkID
			}
		}
		start += mblen
	}

	type span struct {
		lo, hi int
		unk    bool
	}
	var spans []span
	for end := size; end > 0; {
		n := best[end]
		spans = append(spans, span{n.startsAt, end, n.id == u.unkID})
		end = n.startsAt
	}
	for i := len(spans) - 1; i >= 0; i-- {
		s := spans[i]
		if s.unk {
			lo := s.lo
			for i > 0 && spans[i-1].unk {
				i--
			}
			out = append(out, u.lookup(word[lo:spans[i].hi]))
			continue
		}
		out = append(out, u.lookup(word[s.lo:s.hi]))
	}
	return out
}

func (u *unigram) lookup(piece string) int32 {
	if id, ok := u.ids[piece]; ok {
		return id
	}
	return u.unkID
}

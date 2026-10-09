package tokenizer

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

const wordStart = "▁"

type Tokenizer struct {
	norm     *charsmap
	model    *unigram
	added    []addedToken
	bos, eos int32
}

type addedToken struct {
	content        string
	id             int32
	lstrip, rstrip bool
}

func Load(path string) (*Tokenizer, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}

func Parse(raw []byte) (*Tokenizer, error) {
	var f struct {
		AddedTokens []struct {
			ID         int32  `json:"id"`
			Content    string `json:"content"`
			SingleWord bool   `json:"single_word"`
			Lstrip     bool   `json:"lstrip"`
			Rstrip     bool   `json:"rstrip"`
			Normalized bool   `json:"normalized"`
			Special    bool   `json:"special"`
		} `json:"added_tokens"`
		Normalizer *struct {
			Type     string `json:"type"`
			Charsmap string `json:"precompiled_charsmap"`
		} `json:"normalizer"`
		PreTokenizer *struct {
			Type          string `json:"type"`
			Pretokenizers []struct {
				Type          string `json:"type"`
				Replacement   string `json:"replacement"`
				PrependScheme string `json:"prepend_scheme"`
				Split         *bool  `json:"split"`
			} `json:"pretokenizers"`
		} `json:"pre_tokenizer"`
		PostProcessor *struct {
			Type   string            `json:"type"`
			Single []json.RawMessage `json:"single"`
		} `json:"post_processor"`
		Model *struct {
			Type         string              `json:"type"`
			UnkID        *int32              `json:"unk_id"`
			Vocab        [][]json.RawMessage `json:"vocab"`
			ByteFallback bool                `json:"byte_fallback"`
		} `json:"model"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("tokenizer.json: %w", err)
	}
	t := &Tokenizer{}

	if f.Normalizer == nil || f.Normalizer.Type != "Precompiled" {
		return nil, errors.New("tokenizer.json: the normalizer is not a Precompiled table, the only kind read here")
	}
	table, err := base64.StdEncoding.DecodeString(f.Normalizer.Charsmap)
	if err != nil {
		return nil, fmt.Errorf("tokenizer.json: the precompiled charsmap is not base64: %w", err)
	}
	if t.norm, err = parseCharsmap(table); err != nil {
		return nil, fmt.Errorf("tokenizer.json: %w", err)
	}

	p := f.PreTokenizer
	if p == nil || p.Type != "Sequence" || len(p.Pretokenizers) != 2 ||
		p.Pretokenizers[0].Type != "WhitespaceSplit" || p.Pretokenizers[1].Type != "Metaspace" {
		return nil, errors.New("tokenizer.json: the pre-tokenizer is not WhitespaceSplit then Metaspace, the only sequence read here")
	}
	if m := p.Pretokenizers[1]; m.Replacement != wordStart || m.PrependScheme != "always" || (m.Split != nil && !*m.Split) {
		return nil, errors.New("tokenizer.json: the Metaspace step is not the word-start mark prepended always and split on, the only form read here")
	}

	m := f.Model
	if m == nil || m.Type != "Unigram" || m.ByteFallback || m.UnkID == nil {
		return nil, errors.New("tokenizer.json: the model is not a Unigram vocabulary with an unknown id and no byte fallback, the only kind read here")
	}
	u := &unigram{ids: make(map[string]int32, len(m.Vocab)), scores: make([]float64, len(m.Vocab)), unkID: *m.UnkID}
	if len(m.Vocab) == 0 || int(u.unkID) < 0 || int(u.unkID) >= len(m.Vocab) {
		return nil, errors.New("tokenizer.json: the vocabulary is empty or does not hold its own unknown id")
	}
	lowest := 0.0
	for i, entry := range m.Vocab {
		var piece string
		if len(entry) != 2 || json.Unmarshal(entry[0], &piece) != nil || json.Unmarshal(entry[1], &u.scores[i]) != nil {
			return nil, fmt.Errorf("tokenizer.json: vocabulary entry %d is not a piece and its score", i)
		}
		u.ids[piece] = int32(i)
		if i == 0 || u.scores[i] < lowest {
			lowest = u.scores[i]
		}
	}
	u.unkScore = lowest - unkPenalty
	u.trie = buildTrie(u.ids)
	t.model = u

	special := map[string]int32{}
	for _, a := range f.AddedTokens {
		if !a.Special || a.Normalized || a.SingleWord || a.Content == "" {
			return nil, fmt.Errorf("tokenizer.json: added token %q is not a special token matched anywhere in the text as written, the only kind read here", a.Content)
		}
		t.added = append(t.added, addedToken{a.Content, a.ID, a.Lstrip, a.Rstrip})
		special[a.Content] = a.ID
	}

	pp := f.PostProcessor
	if pp == nil || pp.Type != "TemplateProcessing" || len(pp.Single) != 3 {
		return nil, errors.New("tokenizer.json: the post-processor is not a template of a start token, the sequence and an end token, the only form read here")
	}
	frame := func(raw json.RawMessage) (string, bool) {
		var item struct {
			SpecialToken *struct {
				ID string `json:"id"`
			} `json:"SpecialToken"`
			Sequence *struct {
				ID string `json:"id"`
			} `json:"Sequence"`
		}
		if json.Unmarshal(raw, &item) != nil {
			return "", false
		}
		if item.SpecialToken != nil {
			return item.SpecialToken.ID, true
		}
		return "", item.Sequence != nil && item.Sequence.ID == "A"
	}
	first, ok1 := frame(pp.Single[0])
	middle, ok2 := frame(pp.Single[1])
	last, ok3 := frame(pp.Single[2])
	bos, haveBos := special[first]
	eos, haveEos := special[last]
	if !ok1 || !ok2 || !ok3 || middle != "" || !haveBos || !haveEos {
		return nil, errors.New("tokenizer.json: the post-processor's template does not frame one sequence with two of the file's special tokens")
	}
	t.bos, t.eos = bos, eos
	return t, nil
}

func (t *Tokenizer) Encode(text string, maxTokens int) ([]int32, error) {
	if maxTokens < 3 {
		return nil, fmt.Errorf("a window of %d tokens holds no text between its two framing tokens", maxTokens)
	}
	ids := make([]int32, 1, 64)
	ids[0] = t.bos
	for len(text) > 0 {

		at, which := -1, -1
		for n, a := range t.added {
			i := strings.Index(text, a.content)
			if i < 0 {
				continue
			}
			if at < 0 || i < at || (i == at && len(a.content) > len(t.added[which].content)) {
				at, which = i, n
			}
		}
		if at < 0 {
			ids = t.ordinary(text, ids)
			break
		}
		a := t.added[which]
		before, after := text[:at], text[at+len(a.content):]
		if a.lstrip {
			before = strings.TrimRightFunc(before, unicode.IsSpace)
		}
		if a.rstrip {
			after = strings.TrimLeftFunc(after, unicode.IsSpace)
		}
		ids = t.ordinary(before, ids)
		ids = append(ids, a.id)
		text = after
	}
	if len(ids) > maxTokens-1 {
		ids = ids[:maxTokens-1]
	}
	return append(ids, t.eos), nil
}

func (t *Tokenizer) ordinary(text string, ids []int32) []int32 {
	if text == "" {
		return ids
	}
	for _, word := range strings.FieldsFunc(t.norm.normalize(text), unicode.IsSpace) {
		if !strings.HasPrefix(word, wordStart) {
			word = wordStart + word
		}
		for _, part := range splitBeforeMark(word) {
			ids = t.model.encode(part, ids)
		}
	}
	return ids
}

func splitBeforeMark(word string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(word); {
		if strings.HasPrefix(word[i:], wordStart) {
			if i > start {
				parts = append(parts, word[start:i])
			}
			start = i
			i += len(wordStart)
			continue
		}
		_, size := utf8.DecodeRuneInString(word[i:])
		i += size
	}
	return append(parts, word[start:])
}

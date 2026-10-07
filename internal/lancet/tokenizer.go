package lancet

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"unicode"
)

// splitSpace is the whitespace set of the tokenizer's pre-split pattern (the JS `\s` set).
func splitSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x85, 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// pythonSpace is the whitespace set Python's str.strip() uses; the classifier rejects commands made only of it.
func pythonSpace(r rune) bool {
	return splitSpace(r) || (r >= 0x1c && r <= 0x1f)
}

// preSplit reproduces the GPT-2 style pre-tokenizer pattern of the shipped tokenizer:
//
//	's|'t|'re|'ve|'m|'ll|'d| ?\p{L}+| ?\p{N}+| ?[^\s\p{L}\p{N}]+|\s+(?!\S)|\s+
//
// Go's regexp has no lookahead, so the alternatives are tried in order by hand. For `\s+(?!\S)` a whitespace run
// that is followed by a non-space gives back its last character (it must be followed by whitespace); a run at the
// end of the text matches whole.
func preSplit(text string) []string {
	runes := []rune(text)
	n := len(runes)
	var pieces []string
	isOther := func(r rune) bool { return !splitSpace(r) && !unicode.IsLetter(r) && !unicode.IsNumber(r) }
	run := func(from int, in func(rune) bool) int {
		end := from
		for end < n && in(runes[end]) {
			end++
		}
		return end
	}
	for i := 0; i < n; {
		end := -1
		if runes[i] == '\'' {
			rest := string(runes[i+1 : min(i+3, n)])
			switch {
			case strings.HasPrefix(rest, "s"), strings.HasPrefix(rest, "t"), strings.HasPrefix(rest, "m"), strings.HasPrefix(rest, "d"):
				end = i + 2
			case strings.HasPrefix(rest, "re"), strings.HasPrefix(rest, "ve"), strings.HasPrefix(rest, "ll"):
				end = i + 3
			}
		}
		if end < 0 {
			start := i
			if runes[i] == ' ' {
				start = i + 1
			}
			for _, class := range []func(rune) bool{unicode.IsLetter, unicode.IsNumber, isOther} {
				if e := run(start, class); e > start {
					end = e
					break
				}
			}
		}
		if end < 0 {
			wsEnd := run(i, splitSpace)
			switch {
			case wsEnd == n:
				end = wsEnd
			case wsEnd-i >= 2:
				end = wsEnd - 1
			default:
				end = wsEnd
			}
		}
		pieces = append(pieces, string(runes[i:end]))
		i = end
	}
	return pieces
}

// byteChars maps each byte to the printable character the byte-level BPE vocabulary uses for it.
var byteChars = func() [256]string {
	var table [256]string
	shifted := 0
	for b := range 256 {
		printable := (b >= 0x21 && b <= 0x7e) || (b >= 0xa1 && b <= 0xac) || b >= 0xae
		if printable {
			table[b] = string(rune(b))
		} else {
			table[b] = string(rune(256 + shifted))
			shifted++
		}
	}
	return table
}()

// byteLevelBPE encodes text with the exact tokenizer configuration LANCET ships.
type byteLevelBPE struct {
	vocab map[string]int
	ranks map[string]int

	mu    sync.Mutex
	cache map[string][]string
}

func newByteLevelBPE(vocabJSON, mergesTxt []byte) (*byteLevelBPE, error) {
	var vocab map[string]int
	if err := json.Unmarshal(vocabJSON, &vocab); err != nil {
		return nil, fmt.Errorf("parse vocab.json: %w", err)
	}
	if len(vocab) == 0 {
		return nil, fmt.Errorf("unsupported LANCET tokenizer: vocabulary")
	}
	ranks := map[string]int{}
	rank := 0
	for _, line := range strings.Split(string(mergesTxt), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, " ")
		if len(parts) < 2 {
			return nil, fmt.Errorf("unsupported LANCET tokenizer: merge entry %q", line)
		}
		key := parts[0] + " " + parts[1]
		if _, seen := ranks[key]; !seen {
			ranks[key] = rank
		}
		rank++
	}
	return &byteLevelBPE{vocab: vocab, ranks: ranks, cache: map[string][]string{}}, nil
}

func (t *byteLevelBPE) tokenID(token string) (int, error) {
	id, ok := t.vocab[token]
	if !ok {
		return 0, fmt.Errorf("unsupported LANCET tokenizer: missing token %s", token)
	}
	return id, nil
}

// encode returns token ids for text, with no special tokens added.
func (t *byteLevelBPE) encode(text string) ([]int, error) {
	ids := []int{}
	for _, piece := range preSplit(text) {
		var encoded strings.Builder
		for i := range len(piece) {
			encoded.WriteString(byteChars[piece[i]])
		}
		for _, part := range t.bpe(encoded.String()) {
			id, ok := t.vocab[part]
			if !ok {
				return nil, fmt.Errorf("unsupported LANCET tokenizer: piece outside vocabulary")
			}
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (t *byteLevelBPE) bpe(word string) []string {
	t.mu.Lock()
	if cached, ok := t.cache[word]; ok {
		t.mu.Unlock()
		return cached
	}
	t.mu.Unlock()

	var parts []string
	for _, r := range word {
		parts = append(parts, string(r))
	}
	for len(parts) > 1 {
		best, bestRank := -1, int(^uint(0)>>1)
		for i := range len(parts) - 1 {
			if rank, ok := t.ranks[parts[i]+" "+parts[i+1]]; ok && rank < bestRank {
				best, bestRank = i, rank
			}
		}
		if best < 0 {
			break
		}
		left, right := parts[best], parts[best+1]
		merged := make([]string, 0, len(parts))
		for i := 0; i < len(parts); i++ {
			if i < len(parts)-1 && parts[i] == left && parts[i+1] == right {
				merged = append(merged, left+right)
				i++
			} else {
				merged = append(merged, parts[i])
			}
		}
		parts = merged
	}

	t.mu.Lock()
	if len(t.cache) > 20_000 {
		clear(t.cache)
	}
	t.cache[word] = parts
	t.mu.Unlock()
	return parts
}

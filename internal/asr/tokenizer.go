// Minimal HuggingFace tokenizer decoder for moonshine's tokenizer.json.
// Supports the specific decoder pipeline: Replace("▁" -> " "), ByteFallback,
// Fuse, Strip(content=" ", start=1). Encoding is not implemented since
// moonshine takes raw audio.
package asr

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type tokenizerJSON struct {
	AddedTokens []struct {
		ID      int    `json:"id"`
		Content string `json:"content"`
		Special bool   `json:"special"`
	} `json:"added_tokens"`
	Model struct {
		Vocab map[string]int `json:"vocab"`
	} `json:"model"`
}

// Tokenizer decodes moonshine token IDs to text.
type Tokenizer struct {
	idToToken map[int]string
	specialID map[int]bool
}

// LoadTokenizer reads a moonshine tokenizer.json.
func LoadTokenizer(path string) (*Tokenizer, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tj tokenizerJSON
	if err := json.Unmarshal(raw, &tj); err != nil {
		return nil, fmt.Errorf("parse tokenizer.json: %w", err)
	}
	t := &Tokenizer{
		idToToken: make(map[int]string, len(tj.Model.Vocab)+len(tj.AddedTokens)),
		specialID: make(map[int]bool),
	}
	for tok, id := range tj.Model.Vocab {
		t.idToToken[id] = tok
	}
	for _, added := range tj.AddedTokens {
		t.idToToken[added.ID] = added.Content
		if added.Special {
			t.specialID[added.ID] = true
		}
	}
	return t, nil
}

// Decode applies the moonshine decoder chain: per-token Replace, then
// ByteFallback grouping with UTF-8 reassembly, Fuse, and a 1-char leading
// space strip. Skips special tokens.
func (t *Tokenizer) Decode(ids []int64) string {
	var out strings.Builder
	var byteRun []byte
	flushBytes := func() {
		if len(byteRun) > 0 {
			out.Write(byteRun)
			byteRun = byteRun[:0]
		}
	}
	for _, id := range ids {
		if t.specialID[int(id)] {
			flushBytes()
			continue
		}
		tok, ok := t.idToToken[int(id)]
		if !ok {
			flushBytes()
			continue
		}
		// ByteFallback: <0xHH>
		if b, isByte := parseByteFallback(tok); isByte {
			byteRun = append(byteRun, b)
			continue
		}
		flushBytes()
		// Replace ▁ (U+2581) with " "
		out.WriteString(strings.ReplaceAll(tok, "▁", " "))
	}
	flushBytes()
	// Strip {content: " ", start: 1, stop: 0}: drop one leading space.
	s := out.String()
	if strings.HasPrefix(s, " ") {
		s = s[1:]
	}
	return s
}

// parseByteFallback returns (b, true) when tok is "<0xHH>" with HH a valid
// two-digit hex byte. Otherwise (0, false).
func parseByteFallback(tok string) (byte, bool) {
	if len(tok) != 6 || tok[0] != '<' || tok[1] != '0' || tok[2] != 'x' || tok[5] != '>' {
		return 0, false
	}
	hi, ok1 := hexNibble(tok[3])
	lo, ok2 := hexNibble(tok[4])
	if !ok1 || !ok2 {
		return 0, false
	}
	return hi<<4 | lo, true
}

func hexNibble(c byte) (byte, bool) {
	switch {
	case '0' <= c && c <= '9':
		return c - '0', true
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10, true
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10, true
	}
	return 0, false
}

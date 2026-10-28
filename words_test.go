package tsnp

import (
	"strings"
	"testing"
)

func TestWordlist(t *testing.T) {
	if len(words) < 200 {
		t.Fatalf("wordlist too small: %d", len(words))
	}
	seen := map[string]bool{}
	for _, w := range words {
		if len(w) < 4 || len(w) > 8 {
			t.Errorf("bad length: %q", w)
		}
		if w != strings.ToLower(w) {
			t.Errorf("not lowercase: %q", w)
		}
		if strings.ContainsAny(w, "-_'\".") {
			t.Errorf("non-alpha: %q", w)
		}
		if seen[w] {
			t.Errorf("duplicate: %q", w)
		}
		seen[w] = true
	}
}

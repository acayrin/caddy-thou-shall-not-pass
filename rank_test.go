package tsnp

import (
	"bytes"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestRankSceneDeterministic(t *testing.T) {
	for seed := uint64(0); seed < 40; seed++ {
		a, b := rankSceneFor(seed), rankSceneFor(seed)
		if a.answer() != b.answer() || a.variant != b.variant {
			t.Fatalf("seed %d not deterministic", seed)
		}
		if len(strings.Fields(a.answer())) != 4 {
			t.Fatalf("answer %q is not four words", a.answer())
		}
		// the answer must be a permutation of the four drawn words
		words := map[string]bool{}
		for _, w := range a.words {
			words[w] = true
		}
		for _, w := range strings.Fields(a.answer()) {
			if !words[w] {
				t.Fatalf("answer word %q not in scene", w)
			}
		}
	}
}

func TestRankVariantsRotate(t *testing.T) {
	seen := map[int]bool{}
	for seed := uint64(0); seed < 200; seed++ {
		seen[rankSceneFor(seed).variant] = true
	}
	if len(seen) != 3 {
		t.Fatalf("expected 3 rank variants, saw %v", seen)
	}
}

func TestRenderRank(t *testing.T) {
	a := RenderRank(1234)
	if !bytes.Equal(a, RenderRank(1234)) {
		t.Fatal("rank render not deterministic")
	}
	if bytes.Equal(a, RenderRank(1235)) {
		t.Fatal("different seeds produced identical rank image")
	}
	img, err := png.Decode(bytes.NewReader(a))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != rankW || b.Dy() != rankH {
		t.Fatalf("unexpected size %v", b)
	}
}

func TestRankSolveFlow(t *testing.T) {
	app := testApp(t)
	e := &Endpoint{app: app}
	var kind byte
	var answer, tok string
	for i := 0; i < 200; i++ {
		k, _, tkn, err := app.NewChallenge()
		if err != nil {
			t.Fatal(err)
		}
		if k != KindRank {
			continue
		}
		kind, tok = k, tkn
		_, answer, _, _ = app.OpenChallenge(tok)
		break
	}
	if kind != KindRank {
		t.Fatal("no rank challenge generated in 200 tries")
	}
	// separators and case must not matter
	messy := strings.ToUpper(strings.ReplaceAll(answer, " ", ", "))
	rec := httptest.NewRecorder()
	r := newFormPost("/solve", url.Values{"d": {tok}, "a": {messy}, "r": {"/"}})
	r.Header.Set("User-Agent", "Mozilla/5.0 test")
	if err := e.ServeHTTP(rec, r, nil); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("rank solve rejected: %d %s", rec.Code, rec.Body.String()[:min(200, rec.Body.Len())])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

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

func TestMoralScene(t *testing.T) {
	seen := map[byte]bool{}
	for seed := uint64(0); seed < 60; seed++ {
		sc := moralSceneFor(seed)
		if len(sc.answer()) != 1 {
			t.Fatalf("bad answer %q", sc.answer())
		}
		seen[sc.animalSide] = true
		label := sc.branchA
		if sc.answer() == "b" {
			label = sc.branchB
		}
		if !strings.Contains(label, sc.animals) {
			t.Fatalf("seed %d: animals %q not on the answering branch (%q/%q)", seed, sc.animals, sc.branchA, sc.branchB)
		}
		if moralSceneFor(seed).answer() != sc.answer() {
			t.Fatalf("seed %d not deterministic", seed)
		}
	}
	if !seen['a'] || !seen['b'] {
		t.Fatal("animal side never rotates")
	}
}

func TestRenderMoral(t *testing.T) {
	a := RenderMoral(99)
	if !bytes.Equal(a, RenderMoral(99)) {
		t.Fatal("moral render not deterministic")
	}
	img, err := png.Decode(bytes.NewReader(a))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != moralW || b.Dy() != moralH {
		t.Fatalf("unexpected size %v", b)
	}
}

func TestMoralSolveFlow(t *testing.T) {
	app := testApp(t)
	e := &Endpoint{app: app}
	for i := 0; i < 500; i++ {
		kind, _, tok, err := app.NewChallenge()
		if err != nil {
			t.Fatal(err)
		}
		if kind != KindMoral {
			continue
		}
		_, answer, _, _ := app.OpenChallenge(tok)
		rec := httptest.NewRecorder()
		r := newFormPost("/solve", url.Values{"d": {tok}, "a": {strings.ToUpper(answer)}, "r": {"/"}})
		r.Header.Set("User-Agent", "Mozilla/5.0 test")
		if err := e.ServeHTTP(rec, r, nil); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("moral solve rejected: %d", rec.Code)
		}
		return
	}
	t.Fatal("no moral challenge generated")
}

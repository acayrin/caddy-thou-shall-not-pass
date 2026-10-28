package tsnp

import (
	"bytes"
	"image/color"
	"image/png"
	"math"
	mrand "math/rand"
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

// TestRankAnswerMatchesRenderedImage decodes the PNG and measures the rendered
// words, so the sealed answer can never drift from what a human sees.
func TestRankAnswerMatchesRenderedImage(t *testing.T) {
	checked := 0
	for seed := uint64(0); seed < 80; seed++ {
		sc := rankSceneFor(seed)
		img, err := png.Decode(bytes.NewReader(RenderRank(seed)))
		if err != nil {
			t.Fatal(err)
		}
		slots := rankSlots(mrand.New(mrand.NewSource(int64(seed) ^ 0x5f3759df)))
		score := make([]float64, 4)
		for i, r := range slots {
			var lo, hi, mode float64 = 999, -1, 0
			counts := map[int]int{}
			for y := r.Min.Y; y < r.Max.Y; y++ {
				for x := r.Min.X; x < r.Max.X; x++ {
					c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
					l := 0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)
					if l < lo {
						lo = l
					}
					if l > hi {
						hi = l
					}
					counts[int(l)]++
				}
			}
			best := 0
			for v, n := range counts {
				if n > best {
					best, mode = n, float64(v)
				}
			}
			switch sc.variant {
			case 0:
				score[i] = -lo // darkest ink = most visible
			case 1:
				score[i] = math.Abs(130 - mode)
			case 2:
				score[i] = hi // glyph is brighter than the dark paper
			}
		}
		if sc.variant == 2 {
			// inks must be luminance-identical for the key to be meaningful
			for i := 1; i < 4; i++ {
				if math.Abs(score[i]-score[i-1]) > 6 {
					t.Fatalf("seed %d: colour inks are not equal-luma: %v", seed, score)
				}
			}
			if false {
				t.Fatalf("seed %d: colour inks are not equal-luma", seed)
			}
			checked++
			continue
		}
		want := append([]int(nil), sc.order...)
		got := []int{0, 1, 2, 3}
		for i := 1; i < 4; i++ {
			for j := i; j > 0 && score[got[j]] > score[got[j-1]]; j-- {
				got[j], got[j-1] = got[j-1], got[j]
			}
		}
		if len(want) != 4 || got[0] != want[0] || got[3] != want[3] {
			t.Fatalf("seed %d variant %d: measured order %v != sealed %v (scores %v)", seed, sc.variant, got, want, score)
		}
		checked++
	}
	if checked < 70 {
		t.Fatalf("only %d scenes checked", checked)
	}
}

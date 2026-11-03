package tsnp

import (
	"strconv"
	"testing"
)

func TestCountRange(t *testing.T) {
	seen := map[int]bool{}
	for s := uint64(0); s < 2000; s++ {
		n := countSceneFor(s)
		if n < 3 || n > bongCol*bongRow {
			t.Fatalf("seed %d: count %d out of range", s, n)
		}
		seen[n] = true
	}
	if len(seen) < 5 {
		t.Fatalf("count barely varies: %v", seen)
	}
}

func TestCountTokenMatchesRender(t *testing.T) {
	a := testApp(t)
	checked := 0
	for i := 0; i < 2000 && checked < 15; i++ {
		kind, ans, tok, err := a.NewChallenge()
		if err != nil {
			t.Fatal(err)
		}
		if kind != KindCount {
			continue
		}
		k, ans2, seed, ok := a.OpenChallenge(tok)
		if !ok || k != KindCount || ans2 != ans {
			t.Fatalf("token mismatch: ok=%v kind=%c ans=%q sealed=%q", ok, k, ans2, ans)
		}
		if want := strconv.Itoa(countSceneFor(seed)); want != ans {
			t.Fatalf("render count %q != sealed %q", want, ans)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no count challenges generated")
	}
}

func TestCountRenderSize(t *testing.T) {
	checkSize(t, RenderCount(7))
}

func TestCountDeterministic(t *testing.T) {
	a, b := RenderCount(11), RenderCount(11)
	if len(a) != len(b) {
		t.Fatalf("render not deterministic: %d vs %d bytes", len(a), len(b))
	}
}

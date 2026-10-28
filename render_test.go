package tsnp

import (
	"bytes"
	"fmt"
	"image/png"
	mrand "math/rand"
	"testing"
)

func TestRenderDeterminism(t *testing.T) {
	b1 := Render(12345, "maple", 5)
	b2 := Render(12345, "maple", 5)
	if !bytes.Equal(b1, b2) {
		t.Fatal("same seed produced different PNGs")
	}
	if bytes.Equal(b1, Render(99999, "maple", 5)) {
		t.Fatal("different seed produced identical PNG")
	}
	img, err := png.Decode(bytes.NewReader(b1))
	if err != nil {
		t.Fatalf("png decode: %v", err)
	}
	if b := img.Bounds(); b.Dx() != imgW || b.Dy() != imgH {
		t.Fatalf("unexpected size %v", b)
	}
}

func TestGhostBand(t *testing.T) {
	rng := mrand.New(mrand.NewSource(7))
	for i := 0; i < 10000; i++ {
		d := ghostDelta(rng, profile{ghostMax: ghostMaxDelta})
		abs := d
		if abs < 0 {
			abs = -abs
		}
		if abs < ghostMinDelta || abs > ghostMaxDelta {
			t.Fatalf("ghost delta out of band: %d", d)
		}
	}
}

func TestGhostInvisibleOnNoise(t *testing.T) {
	// ghost color must stay close to the noisy background: max channel delta
	// of ghost color vs bg <= ghostMaxDelta.
	rng := mrand.New(mrand.NewSource(11))
	for i := 0; i < 2000; i++ {
		c := ghostColor(rng, profile{ghostMax: ghostMaxDelta})
		for _, v := range [3]int{int(c.R) - bgR, int(c.G) - bgG, int(c.B) - bgB} {
			a := v
			if a < 0 {
				a = -a
			}
			if a > ghostMaxDelta {
				t.Fatalf("ghost channel delta %d out of band", v)
			}
		}
	}
}

func TestNoSecretMeansNoKeys(t *testing.T) {
	a := &App{}
	if err := a.provision(); err == nil {
		t.Fatal("missing secret accepted")
	}
}

func TestIllusionRender(t *testing.T) {
	b1 := RenderIllusion(42)
	if !bytes.Equal(b1, RenderIllusion(42)) {
		t.Fatal("illusion render not deterministic")
	}
	if bytes.Equal(b1, RenderIllusion(43)) {
		t.Fatal("different seeds produced identical illusion image")
	}
	img, err := png.Decode(bytes.NewReader(b1))
	if err != nil {
		t.Fatalf("png decode: %v", err)
	}
	if b := img.Bounds(); b.Dx() != imgW || b.Dy() != imgH {
		t.Fatalf("unexpected size %v", b)
	}
}

func TestChallengeKindsRotate(t *testing.T) {
	a := testApp(t)
	seen := map[byte]bool{}
	for i := 0; i < 2000 && len(seen) < 3; i++ {
		k, _, _, err := a.NewChallenge()
		if err != nil {
			t.Fatal(err)
		}
		seen[k] = true
	}
	if !seen[KindWord] || !seen[KindIllusion] || !seen[KindMoral] {
		t.Fatalf("kinds not rotating: %v", seen)
	}
}

func TestIllusionAnswerMatchesRender(t *testing.T) {
	a := testApp(t)
	checked := 0
	for i := 0; i < 300 && checked < 20; i++ {
		kind, ans, tok, err := a.NewChallenge()
		if err != nil {
			t.Fatal(err)
		}
		if kind != KindIllusion {
			continue
		}
		k, ans2, seed, ok := a.OpenChallenge(tok)
		if !ok || k != KindIllusion || ans2 != ans {
			t.Fatalf("token mismatch: ok=%v kind=%c ans=%q sealed=%q", ok, k, ans2, ans)
		}
		// the renderer decides the outward-fin side from the same seed
		if IllusionAnswer(seed) != ans {
			t.Fatalf("render side %q != sealed answer %q", IllusionAnswer(seed), ans)
		}
		checked++
	}
	if checked < 20 {
		t.Fatalf("only %d illusion tokens generated", checked)
	}
}

func TestProfileVariety(t *testing.T) {
	facesOnce.Do(loadFonts)
	seen := map[string]bool{}
	for s := 0; s < 64; s++ {
		rng := mrand.New(mrand.NewSource(int64(s)))
		p := profileFor(rng, 5)
		seen[fmt.Sprintf("%p|%d|%d|%d|%d", p.font, p.overlap, p.waveAmp, p.texture, p.ghostMax)] = true
	}
	if len(seen) < 24 {
		t.Fatalf("render profile variety too low: %d distinct styles", len(seen))
	}
}

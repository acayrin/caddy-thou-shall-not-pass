package tsnp

import (
	"bytes"
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
		d := ghostDelta(rng)
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
		c := ghostColor(rng)
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

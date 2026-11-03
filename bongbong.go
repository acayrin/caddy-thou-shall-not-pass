package tsnp

import (
	"bytes"
	_ "embed"
	"image"
	"image/color"
	"image/png"
	mrand "math/rand"
	"sync"

	xdraw "golang.org/x/image/draw"
)

// The bong bong count challenge: a joke tier. It is not meant to stop a
// scripted solver - it exists because the gate should occasionally be silly.
// The sprite is user-supplied fan art, embedded and drawn at a few sizes.

//go:embed assets/bongbong.png
var bongRaw []byte

const (
	bongH   = 60 // drawn sprite height in px
	bongCol = 4  // 4x2 grid: 4x2 = 8 slots, largest count is 8
	bongRow = 2
)

var (
	bongOnce sync.Once
	bongBase *image.NRGBA
	bongErr  error
)

// loadBong decodes and downscales the sprite once per process.
func loadBong() *image.NRGBA {
	bongOnce.Do(func() {
		src, err := png.Decode(bytes.NewReader(bongRaw))
		if err != nil {
			bongErr = err
			return
		}
		b := src.Bounds()
		w := b.Dx() * bongH / b.Dy()
		dst := image.NewNRGBA(image.Rect(0, 0, w, bongH))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)
		bongBase = dst
	})
	return bongBase
}

// countSceneFor returns how many bong bongs RenderCount will draw. Derived
// from the render seed like every other kind, so the token stays stateless.
func countSceneFor(seed uint64) int {
	rng := mrand.New(mrand.NewSource(int64(seed)))
	return 3 + rng.Intn(6) // 3..8
}

// RenderCount draws the scene: same paper, noise and streaks as the word
// challenge, with the sprites scattered over a jittered 4x2 grid. Each one is
// flipped, tinted and offset independently so a template matcher cannot count
// them by looking for one exact bitmap.
func RenderCount(seed uint64) []byte {
	facesOnce.Do(loadFonts)
	rng := mrand.New(mrand.NewSource(int64(seed)))
	n := 3 + rng.Intn(6)

	img := image.NewNRGBA(image.Rect(0, 0, imgW, imgH))
	drawBackground(img, rng, profileFor(rng, 0))

	sprite := loadBong()
	if sprite != nil {
		order := rng.Perm(bongCol * bongRow)
		cw, ch := imgW/bongCol, imgH/bongRow
		for i := 0; i < n && i < len(order); i++ {
			cell := order[i]
			cx, cy := (cell%bongCol)*cw, (cell/bongCol)*ch
			x := cx + (cw-sprite.Bounds().Dx())/2 + rng.Intn(13) - 6
			y := cy + (ch-bongH)/2 + rng.Intn(7) - 3
			blitSprite(img, sprite, x, y, rng.Intn(2) == 0, 0.85+rng.Float64()*0.3)
		}
	}

	for i := 0; i < 10; i++ {
		streak(img, rng)
	}
	for i := 0; i < 200; i++ {
		img.SetNRGBA(rng.Intn(imgW), rng.Intn(imgH), lowDelta(rng, 10))
	}

	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// blitSprite composites src at (x0,y0), optionally mirrored and tinted.
func blitSprite(dst *image.NRGBA, src *image.NRGBA, x0, y0 int, flip bool, tint float64) {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	for y := 0; y < h; y++ {
		py := y0 + y
		if py < 0 || py >= imgH {
			continue
		}
		for x := 0; x < w; x++ {
			px := x0 + x
			if px < 0 || px >= imgW {
				continue
			}
			sx := x
			if flip {
				sx = w - 1 - x
			}
			c := src.NRGBAAt(sx, y)
			if c.A == 0 {
				continue
			}
			c = color.NRGBA{R: tune(c.R, tint), G: tune(c.G, tint), B: tune(c.B, tint), A: c.A}
			dst.SetNRGBA(px, py, over(c, dst.NRGBAAt(px, py)))
		}
	}
}

func tune(v uint8, f float64) uint8 {
	n := int(float64(v) * f)
	if n < 0 {
		return 0
	}
	if n > 255 {
		return 255
	}
	return uint8(n)
}

// over is src-over-dst for an opaque destination.
func over(src, dst color.NRGBA) color.NRGBA {
	a := uint32(src.A)
	inv := 255 - a
	mix := func(s, d uint8) uint8 {
		return uint8((uint32(s)*a + uint32(d)*inv) / 255)
	}
	return color.NRGBA{R: mix(src.R, dst.R), G: mix(src.G, dst.G), B: mix(src.B, dst.B), A: 255}
}

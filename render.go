package tsnp

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	mrand "math/rand"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	imgW, imgH    = 420, 132
	bgR, bgG, bgB = 247, 246, 242
	// ghost channel delta band: imperceptible on the noise floor (base noise
	// is +/-3), yet decoded as full-strength text by OCR/VLM encoders.
	ghostMinDelta, ghostMaxDelta = 3, 8
)

var (
	facesOnce sync.Once
	sansReg   *opentype.Font
	sansBold  *opentype.Font
	sansErr   error
)

func loadFonts() {
	sansReg, sansErr = opentype.Parse(goregular.TTF)
	b, err := opentype.Parse(gobold.TTF)
	if err == nil {
		sansBold = b
	}
	if sansErr == nil && sansBold == nil {
		sansBold = sansReg
	}
}

func faceFor(tt *opentype.Font, size int) font.Face {
	if tt == nil {
		return nil
	}
	f, err := opentype.NewFace(tt, &opentype.FaceOptions{Size: float64(size), DPI: 72})
	if err != nil {
		return nil
	}
	return f
}

func ch(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// ghostDelta returns a channel offset in +/-[ghostMinDelta, ghostMaxDelta].
func ghostDelta(rng *mrand.Rand) int {
	d := ghostMinDelta + rng.Intn(ghostMaxDelta-ghostMinDelta+1)
	if rng.Intn(2) == 0 {
		d = -d
	}
	return d
}

func ghostColor(rng *mrand.Rand) color.NRGBA {
	return color.NRGBA{
		R: ch(bgR + ghostDelta(rng)),
		G: ch(bgG + ghostDelta(rng)),
		B: ch(bgB + ghostDelta(rng)),
		A: 255,
	}
}

func lowDelta(rng *mrand.Rand, band int) color.NRGBA {
	d := rng.Intn(2*band+1) - band
	return color.NRGBA{R: ch(bgR + d), G: ch(bgG + d), B: ch(bgB + d), A: 255}
}

// Render deterministically produces the captcha PNG for (seed, word, decoys):
// per-pixel noise underlay, N human-invisible ghost words, one bold true word
// with overlapping, vertically-jittered glyphs, then light streaks and dots
// on top. Same seed => byte-identical PNG (served with no-store).
func Render(seed uint64, word string, decoys int) []byte {
	facesOnce.Do(loadFonts)
	rng := mrand.New(mrand.NewSource(int64(seed)))
	img := image.NewNRGBA(image.Rect(0, 0, imgW, imgH))

	for y := 0; y < imgH; y++ {
		for x := 0; x < imgW; x++ {
			d := rng.Intn(7) - 3
			img.SetNRGBA(x, y, color.NRGBA{R: ch(bgR + d), G: ch(bgG + d), B: ch(bgB + d), A: 255})
		}
	}

	for i := 0; i < decoys; i++ {
		gw := pickWordRng(rng)
		if gw == word {
			continue
		}
		c := ghostColor(rng)
		size := 36 + rng.Intn(22)
		f := faceFor(sansReg, size)
		if f == nil {
			continue
		}
		wdt := measure(f, gw)
		if wdt > imgW-16 {
			continue
		}
		x := 8 + rng.Intn(imgW-wdt-16+1)
		y := 24 + rng.Intn(imgH-38)
		drawWord(img, f, gw, c, x, y)
	}

	drawTrueWord(img, rng, word)

	for i := 0; i < 14; i++ {
		streak(img, rng)
	}
	for i := 0; i < 240; i++ {
		img.SetNRGBA(rng.Intn(imgW), rng.Intn(imgH), lowDelta(rng, 12))
	}

	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func drawWord(img *image.NRGBA, f font.Face, s string, c color.Color, x, y int) {
	d := &font.Drawer{Dst: img, Src: image.NewUniform(c), Face: f, Dot: fixed.P(x, y)}
	d.DrawString(s)
}

func measure(f font.Face, s string) int {
	d := font.Drawer{Face: f}
	return int(d.MeasureString(s) >> 6)
}

func advance(f font.Face, ru rune) int {
	a, _ := f.GlyphAdvance(ru)
	return int(a>>6) * 72 / 100
}

// drawTrueWord: bold, dark, glyph-by-glyph with ~28% horizontal overlap and
// baseline jitter - overlap kills character-segmentation solvers outright.
func drawTrueWord(img *image.NRGBA, rng *mrand.Rand, word string) {
	size := 58
	var f font.Face
	var total int
	for {
		f = faceFor(sansBold, size)
		if f == nil {
			return
		}
		total = 0
		for _, ru := range word {
			total += advance(f, ru)
		}
		if total <= imgW-24 || size <= 34 {
			break
		}
		size -= 2
	}
	x := (imgW - total) / 2
	x += rng.Intn(16) - 8
	if x < 8 {
		x = 8
	}
	if x > imgW-total-8 {
		x = imgW - total - 8
	}
	y := imgH * 3 / 4
	col := color.NRGBA{R: 30, G: 28, B: 24, A: 255}
	for _, ru := range word {
		drawWord(img, f, string(ru), col, x, y)
		x += advance(f, ru) + rng.Intn(7) - 3
		y += rng.Intn(19) - 9
		if y < size+6 {
			y = size + 6
		}
		if y > imgH-10 {
			y = imgH - 10
		}
	}
}

// streak draws a low-contrast line (Bresenham) over everything.
func streak(img *image.NRGBA, rng *mrand.Rand) {
	x0, y0 := rng.Intn(imgW), rng.Intn(imgH)
	x1, y1 := rng.Intn(imgW), rng.Intn(imgH)
	c := lowDelta(rng, 10)
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := 1, 1
	if x0 >= x1 {
		sx = -1
	}
	if y0 >= y1 {
		sy = -1
	}
	err := dx + dy
	for {
		if x0 >= 0 && x0 < imgW && y0 >= 0 && y0 < imgH {
			img.SetNRGBA(x0, y0, c)
		}
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

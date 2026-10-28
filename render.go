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
	imgW, imgH    = 480, 150
	bgR, bgG, bgB = 247, 246, 242

	// ghost channel delta band: imperceptible on the noise floor (base noise
	// is +/-3), yet decoded as full-strength text by OCR/VLM encoders.
	// Luminance-only (same delta on R,G,B) so ghosts read as neutral paper
	// texture to humans instead of colored text.
	ghostMinDelta, ghostMaxDelta = 3, 6

	// glyph advance as a percentage of the full advance width. 78% overlaps
	// neighbouring glyphs enough to break naive character segmentation while
	// staying comfortably readable for humans.
	advancePct = 78
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

// ghostDelta returns a luminance offset in +/-[ghostMinDelta, ghostMaxDelta].
func ghostDelta(rng *mrand.Rand) int {
	d := ghostMinDelta + rng.Intn(ghostMaxDelta-ghostMinDelta+1)
	if rng.Intn(2) == 0 {
		d = -d
	}
	return d
}

// ghostColor is luminance-only: identical delta on all channels.
func ghostColor(rng *mrand.Rand) color.NRGBA {
	d := ghostDelta(rng)
	return color.NRGBA{R: ch(bgR + d), G: ch(bgG + d), B: ch(bgB + d), A: 255}
}

func lowDelta(rng *mrand.Rand, band int) color.NRGBA {
	d := rng.Intn(2*band+1) - band
	return color.NRGBA{R: ch(bgR + d), G: ch(bgG + d), B: ch(bgB + d), A: 255}
}

// Render deterministically produces the captcha PNG for (seed, word, decoys):
// per-pixel noise underlay, N human-invisible ghost words drawn with the SAME
// bold face, size range and overlapping-glyph treatment as the true word (so
// stroke weight / letter shape carry no "this one is real" signal), one dark
// true word, cut-out specks through it, then light streaks and dots on top.
// Same seed => byte-identical PNG (served with no-store).
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
		size := 40 + rng.Intn(24)
		f := faceFor(sansBold, size)
		if f == nil {
			continue
		}
		total := measureOverlapped(f, gw)
		if total > imgW-16 {
			continue
		}
		x := 8 + rng.Intn(imgW-total-16+1)
		y := size + 6 + rng.Intn(imgH-size-18)
		drawOverlapped(img, rng, gw, f, c, x, y, 5)
	}

	drawTrueWord(img, rng, word)

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

func drawGlyph(img *image.NRGBA, f font.Face, s string, c color.Color, x, y int) {
	d := &font.Drawer{Dst: img, Src: image.NewUniform(c), Face: f, Dot: fixed.P(x, y)}
	d.DrawString(s)
}

func measure(f font.Face, s string) int {
	d := font.Drawer{Face: f}
	return int(d.MeasureString(s) >> 6)
}

func advance(f font.Face, ru rune) int {
	a, _ := f.GlyphAdvance(ru)
	return int(a>>6) * advancePct / 100
}

func measureOverlapped(f font.Face, s string) int {
	total := 0
	for _, ru := range s {
		total += advance(f, ru)
	}
	return total
}

// drawOverlapped draws s glyph-by-glyph with horizontal overlap and baseline
// jitter (jitter = max +/- y offset in px). Used for both the true word and
// the ghost decoys - identical treatment, identical structure.
func drawOverlapped(img *image.NRGBA, rng *mrand.Rand, s string, f font.Face, c color.Color, x, y, jitter int) {
	for _, ru := range s {
		drawGlyph(img, f, string(ru), c, x, y)
		x += advance(f, ru) + rng.Intn(5) - 2
		y += rng.Intn(2*jitter+1) - jitter
		if y < 20 {
			y = 20
		}
		if y > imgH-10 {
			y = imgH - 10
		}
	}
}

// drawTrueWord: 64pt bold, dark, centred-ish, with cut-out specks through the
// glyphs (background-coloured nicks that break stroke continuity for OCR).
func drawTrueWord(img *image.NRGBA, rng *mrand.Rand, word string) {
	size := 64
	var f font.Face
	var total int
	for {
		f = faceFor(sansBold, size)
		if f == nil {
			return
		}
		total = measureOverlapped(f, word)
		if total <= imgW-24 || size <= 34 {
			break
		}
		size -= 2
	}
	x := (imgW - total) / 2
	x += rng.Intn(12) - 6
	if x < 8 {
		x = 8
	}
	if x > imgW-total-8 {
		x = imgW - total - 8
	}
	y := imgH * 3 / 4
	col := color.NRGBA{R: 30, G: 28, B: 24, A: 255}
	drawOverlapped(img, rng, word, f, col, x, y, 6)

	// cut-out specks: background-coloured nicks inside the word box
	top := y - size
	if top < 4 {
		top = 4
	}
	bottom := y + size/5
	if bottom > imgH-4 {
		bottom = imgH - 4
	}
	for i := 0; i < 55; i++ {
		sx := x + rng.Intn(total+8)
		sy := top + rng.Intn(bottom-top+1)
		w := 1 + rng.Intn(3)
		for dx := 0; dx < w; dx++ {
			for dy := 0; dy < w; dy++ {
				img.SetNRGBA(sx+dx, sy+dy, lowDelta(rng, 3))
			}
		}
	}
}

// streak draws a low-contrast line (Bresenham) over everything.
func streak(img *image.NRGBA, rng *mrand.Rand) {
	x0, y0 := rng.Intn(imgW), rng.Intn(imgH)
	x1, y1 := rng.Intn(imgW), rng.Intn(imgH)
	c := lowDelta(rng, 6)
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

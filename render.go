package tsnp

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	mrand "math/rand"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	imgW, imgH    = 480, 150
	bgR, bgG, bgB = 247, 246, 242

	// ghost channel delta band floor: imperceptible on the noise floor (base
	// noise is +/-3), yet decoded as full-strength text by OCR/VLM encoders.
	// Luminance-only (same delta on R,G,B) so ghosts read as neutral paper
	// texture to humans instead of colored text.
	ghostMinDelta, ghostMaxDelta = 3, 8
)

// profile is the per-challenge render style. Every challenge picks its own
// combination (font, glyph overlap, baseline wave, texture) so a solver
// trained or tuned on one render style does not transfer to the next; the
// true word and the decoys always share the same style, so style never leaks
// which word is real.
type profile struct {
	font     *opentype.Font
	overlap  int // glyph advance, percent of full width (68..88)
	jitter   int // baseline jitter, px (+/-)
	waveAmp  int // baseline wave amplitude, px (0..9)
	waveLen  int // baseline wave wavelength, px (30..70)
	decoys   int
	ghostMax int // decoy luminance delta cap (5..8)
	texture  int // 0: pixel noise, 1: + horizontal streaks, 2: + diagonal
}

var (
	facesOnce sync.Once
	fonts     []*opentype.Font
	fontsErr  error
)

func loadFonts() {
	ttfs := [][]byte{goregular.TTF, gobold.TTF, gomedium.TTF, gomono.TTF}
	for _, raw := range ttfs {
		f, err := opentype.Parse(raw)
		if err != nil {
			fontsErr = err
			continue
		}
		fonts = append(fonts, f)
	}
}

func fontAt(i int) *opentype.Font {
	if len(fonts) == 0 {
		return nil
	}
	return fonts[i%len(fonts)]
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

func profileFor(rng *mrand.Rand, decoys int) profile {
	p := profile{
		font:     fontAt(rng.Intn(4)),
		overlap:  68 + rng.Intn(21),
		jitter:   3 + rng.Intn(6),
		waveAmp:  rng.Intn(10),
		waveLen:  30 + rng.Intn(41),
		decoys:   decoys + rng.Intn(3) - 1,
		ghostMax: 5 + rng.Intn(4),
		texture:  rng.Intn(3),
	}
	if p.decoys < 1 {
		p.decoys = 1
	}
	if p.waveAmp == 0 {
		p.waveAmp = 1 // keep a hint of curvature for template confusion
	}
	return p
}

// ghostDelta returns a luminance offset in +/-[ghostMinDelta, p.ghostMax].
func ghostDelta(rng *mrand.Rand, p profile) int {
	d := ghostMinDelta + rng.Intn(p.ghostMax-ghostMinDelta+1)
	if rng.Intn(2) == 0 {
		d = -d
	}
	return d
}

// ghostColor is luminance-only: identical delta on all channels.
func ghostColor(rng *mrand.Rand, p profile) color.NRGBA {
	d := ghostDelta(rng, p)
	return color.NRGBA{R: ch(bgR + d), G: ch(bgG + d), B: ch(bgB + d), A: 255}
}

func lowDelta(rng *mrand.Rand, band int) color.NRGBA {
	d := rng.Intn(2*band+1) - band
	return color.NRGBA{R: ch(bgR + d), G: ch(bgG + d), B: ch(bgB + d), A: 255}
}

// Render deterministically produces the captcha PNG for (seed, word, decoys):
// noise/texture underlay, N human-invisible ghost words, one dark true word
// with overlapping, jittered, wave-distorted glyphs, then cut-out specks and
// light streaks on top. Same seed => byte-identical PNG (served no-store).
func Render(seed uint64, word string, decoys int) []byte {
	facesOnce.Do(loadFonts)
	rng := mrand.New(mrand.NewSource(int64(seed)))
	p := profileFor(rng, decoys)

	img := image.NewNRGBA(image.Rect(0, 0, imgW, imgH))
	drawBackground(img, rng, p)

	for i := 0; i < p.decoys; i++ {
		gw := pickWordRng(rng)
		if gw == word {
			continue
		}
		c := ghostColor(rng, p)
		size := 40 + rng.Intn(24)
		f := faceFor(p.font, size)
		if f == nil {
			continue
		}
		total := measureOverlapped(f, gw, p.overlap)
		if total > imgW-16 {
			continue
		}
		x := 8 + rng.Intn(imgW-total-16+1)
		y := size + 6 + rng.Intn(imgH-size-18)
		drawOverlapped(img, rng, gw, f, c, x, y, p, size)
	}

	drawTrueWord(img, rng, p, word)

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

func drawBackground(img *image.NRGBA, rng *mrand.Rand, p profile) {
	for y := 0; y < imgH; y++ {
		for x := 0; x < imgW; x++ {
			d := rng.Intn(7) - 3
			img.SetNRGBA(x, y, color.NRGBA{R: ch(bgR + d), G: ch(bgG + d), B: ch(bgB + d), A: 255})
		}
	}
	switch p.texture {
	case 1:
		for i := 0; i < 26; i++ {
			y := rng.Intn(imgH)
			c := lowDelta(rng, 5)
			for x := rng.Intn(imgW / 3); x < imgW; x++ {
				img.SetNRGBA(x, y, c)
			}
		}
	case 2:
		for i := 0; i < 18; i++ {
			drawLine(img, rng.Intn(120), rng.Intn(imgH), imgW-40+rng.Intn(40), rng.Intn(imgH), lowDelta(rng, 5))
		}
	}
}

func drawGlyph(img *image.NRGBA, f font.Face, s string, c color.Color, x, y int) {
	d := &font.Drawer{Dst: img, Src: image.NewUniform(c), Face: f, Dot: fixed.P(x, y)}
	d.DrawString(s)
}

func measure(f font.Face, s string) int {
	d := font.Drawer{Face: f}
	return int(d.MeasureString(s) >> 6)
}

func advance(f font.Face, ru rune, overlap int) int {
	a, _ := f.GlyphAdvance(ru)
	return int(a>>6) * overlap / 100
}

func measureOverlapped(f font.Face, s string, overlap int) int {
	total := 0
	for _, ru := range s {
		total += advance(f, ru, overlap)
	}
	return total
}

// wave returns the baseline offset for a glyph starting at x.
func wave(p profile, x int) int {
	if p.waveAmp == 0 || p.waveLen == 0 {
		return 0
	}
	return int(float64(p.waveAmp) * math.Sin(2*math.Pi*float64(x)/float64(p.waveLen)))
}

// drawOverlapped draws s glyph-by-glyph with horizontal overlap, per-glyph
// baseline jitter and a slow baseline wave. Used for the true word and every
// decoy alike.
func drawOverlapped(img *image.NRGBA, rng *mrand.Rand, s string, f font.Face, c color.Color, x, y int, p profile, size int) {
	startX := x
	for _, ru := range s {
		drawGlyph(img, f, string(ru), c, x, y+wave(p, x-startX))
		x += advance(f, ru, p.overlap) + rng.Intn(5) - 2
		y += rng.Intn(2*p.jitter+1) - p.jitter
		if y < size+4 {
			y = size + 4
		}
		if y > imgH-8 {
			y = imgH - 8
		}
	}
}

// drawTrueWord: 64pt dark word, wave + overlap + jitter, then cut-out specks
// (background-coloured nicks that break stroke continuity for OCR).
func drawTrueWord(img *image.NRGBA, rng *mrand.Rand, p profile, word string) {
	size := 64
	var f font.Face
	var total int
	for {
		f = faceFor(p.font, size)
		if f == nil {
			return
		}
		total = measureOverlapped(f, word, p.overlap)
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
	drawOverlapped(img, rng, word, f, col, x, y, p, size)

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

func drawLine(img *image.NRGBA, x0, y0, x1, y1 int, c color.Color) {
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
			img.Set(x0, y0, c)
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

// streak draws a low-contrast random line over everything.
func streak(img *image.NRGBA, rng *mrand.Rand) {
	drawLine(img,
		rng.Intn(imgW), rng.Intn(imgH),
		rng.Intn(imgW), rng.Intn(imgH),
		lowDelta(rng, 6))
}

// RenderIllusion draws a Muller-Lyer challenge: two exactly equal-length
// vertical lines, one with outward fins (the side humans perceive as longer),
// the other with inward fins. A pixel-measuring model sees equality; the
// answer side is sealed in the token (l/r).
func RenderIllusion(seed uint64) []byte {
	facesOnce.Do(loadFonts)
	rng := mrand.New(mrand.NewSource(int64(seed)))
	img := image.NewNRGBA(image.Rect(0, 0, imgW, imgH))
	drawBackground(img, rng, profile{texture: 0})

	cx := imgW / 2
	top, bot := 36+rng.Intn(8), imgH-36-rng.Intn(8)
	sep := 66 + rng.Intn(14)
	lx, rx := cx-sep, cx+sep
	ink := color.NRGBA{R: 30, G: 28, B: 24, A: 255}

	answerLeft := IllusionAnswer(seed) == "l"
	drawMuller(img, lx, top, bot, answerLeft, ink, rng)
	drawMuller(img, rx, top, bot, !answerLeft, ink, rng)

	for i := 0; i < 8; i++ {
		streak(img, rng)
	}
	for i := 0; i < 160; i++ {
		img.SetNRGBA(rng.Intn(imgW), rng.Intn(imgH), lowDelta(rng, 10))
	}

	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// drawMuller: vertical line with fins. outward=true flares the fins away from
// the shaft at both ends (Muller-Lyer: perceived longer); outward=false turns
// them inwards (perceived shorter). Both shafts are exactly bot-top long.
func drawMuller(img *image.NRGBA, x, top, bot int, outward bool, c color.Color, rng *mrand.Rand) {
	fx := 14 + rng.Intn(6)
	fy := 25 + rng.Intn(6)
	drawLine(img, x, top, x, bot, c)
	drawLine(img, x+1, top, x+1, bot, c)
	dirTop, dirBot := -1, 1
	if !outward {
		dirTop, dirBot = 1, -1
	}
	drawLine(img, x, top, x-fx, top+dirTop*fy, c)
	drawLine(img, x, top, x+fx, top+dirTop*fy, c)
	drawLine(img, x, bot, x-fx, bot+dirBot*fy, c)
	drawLine(img, x, bot, x+fx, bot+dirBot*fy, c)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

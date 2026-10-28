package tsnp

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	mrand "math/rand"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// Rank challenges ask the visitor to order four words by perceived visibility.
// They exist to stall solvers, not to gate humans: a human who cannot rank
// them can simply reload for a fresh challenge. Three sub-variants rotate:
//
//	plain  - graded ink on light paper (trivial for pixel statistics, but the
//	         words are still four OCR targets for a text pipeline)
//	simul  - identical ink, graded background panels
//	colour - identical luma, different hue: pixel-luma measurement sees a tie
//
// The sealed answer is the four words in most->least visible order.

const (
	rankW, rankH = 480, 240
)

type rankScene struct {
	words   [4]string
	order   []int // indices into words, most -> least visible
	variant int   // 0 plain, 1 simul, 2 colour
}

// rankSceneFor builds the scene deterministically from the render seed. Both
// the token (answer) and the renderer use it, so they cannot disagree.
func rankSceneFor(seed uint64) rankScene {
	rng := mrand.New(mrand.NewSource(int64(seed)))
	sc := rankScene{variant: rng.Intn(3)}

	seen := map[string]bool{}
	for i := 0; i < 4; i++ {
		for {
			w := pickWordRng(rng)
			if !seen[w] {
				seen[w] = true
				sc.words[i] = w
				break
			}
		}
	}
	switch sc.variant {
	case 0: // graded ink
		inks := []float64{60, 110, 158, 205}
		rng.Shuffle(4, func(i, j int) { inks[i], inks[j] = inks[j], inks[i] })
		sc.order = []int{0, 1, 2, 3}
		sortByDesc(sc.order, func(i int) float64 { return -inks[i] })
	case 1: // graded background
		bgs := []float64{45, 100, 170, 228}
		rng.Shuffle(4, func(i, j int) { bgs[i], bgs[j] = bgs[j], bgs[i] })
		sc.order = []int{0, 1, 2, 3}
		sortByDesc(sc.order, func(i int) float64 {
			d := 130 - bgs[i]
			if d < 0 {
				return -d
			}
			return d
		})
	case 2: // equal luma, different hue
		cols := rankColors(rng)
		sc.order = []int{0, 1, 2, 3}
		sortByDesc(sc.order, func(i int) float64 { return float64(colorBrightnessRank(cols[i])) })
	}
	return sc
}

func sortByDesc(idx []int, key func(int) float64) {
	// insertion sort keeps it dependency-free and stable
	for i := 1; i < len(idx); i++ {
		for j := i; j > 0 && key(idx[j]) > key(idx[j-1]); j-- {
			idx[j], idx[j-1] = idx[j-1], idx[j]
		}
	}
}

// rankAnswer is the sealed answer string: words most->least visible.
func (s rankScene) answer() string {
	out := make([]string, 4)
	for i, idx := range s.order {
		out[i] = s.words[idx]
	}
	return strings.Join(out, " ")
}

// ---- colours ----

// colorBrightnessRank encodes the Helmholtz-Kohlrausch hypothesis used as the
// answer key: at equal luma, saturated reds/magentas look brighter than greens
// and blues.
func colorBrightnessRank(c color.NRGBA) int {
	switch {
	case c.R > 140 && c.G < 140 && c.B < 140:
		return 3 // red-orange
	case c.R > 140 && c.B > 110:
		return 2 // magenta
	case c.G > 110:
		return 1 // green
	default:
		return 0 // blue
	}
}

func equalLuma(hue color.NRGBA, target float64) (color.NRGBA, bool) {
	base := lumaOf(hue)
	if target > base && base < 255 {
		t := (target - base) / (255 - base)
		if t <= 1 {
			return color.NRGBA{
				R: ch(int(float64(hue.R) + t*(255-float64(hue.R)))),
				G: ch(int(float64(hue.G) + t*(255-float64(hue.G)))),
				B: ch(int(float64(hue.B) + t*(255-float64(hue.B)))),
				A: 255,
			}, true
		}
	}
	if target <= base && base > 0 {
		k := target / base
		return color.NRGBA{R: ch(int(float64(hue.R) * k)), G: ch(int(float64(hue.G) * k)), B: ch(int(float64(hue.B) * k)), A: 255}, true
	}
	return color.NRGBA{}, false
}

func lumaOf(c color.NRGBA) float64 {
	return 0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)
}

func rankColors(rng *mrand.Rand) []color.NRGBA {
	hues := []color.NRGBA{
		{R: 255, G: 60, B: 0, A: 255},  // red-orange
		{R: 40, G: 230, B: 40, A: 255}, // green
		{R: 0, G: 110, B: 255, A: 255}, // blue
		{R: 230, G: 0, B: 210, A: 255}, // magenta
	}
	cols := make([]color.NRGBA, 4)
	for i, h := range hues {
		c, ok := equalLuma(h, 115)
		if !ok {
			c = h
		}
		cols[i] = c
	}
	rng.Shuffle(4, func(i, j int) { cols[i], cols[j] = cols[j], cols[i] })
	return cols
}

// ---- renderer ----

// RenderRank draws the rank challenge for seed.
func RenderRank(seed uint64) []byte {
	facesOnce.Do(loadFonts)
	sc := rankSceneFor(seed)
	rng := mrand.New(mrand.NewSource(int64(seed) ^ 0x5f3759df))
	img := image.NewNRGBA(image.Rect(0, 0, rankW, rankH))
	fillPix(img, gray(246))

	slots := rankSlots(rng)
	f := faceFor(fontAt(1), 34) // bold, readable

	var inks []color.NRGBA
	var bgs []float64
	switch sc.variant {
	case 0:
		inks = make([]color.NRGBA, 4)
		vals := []float64{60, 110, 158, 205}
		rng.Shuffle(4, func(i, j int) { vals[i], vals[j] = vals[j], vals[i] })
		for i := range vals {
			inks[i] = gray(vals[i])
		}
	case 1:
		bgs = []float64{45, 100, 170, 228}
		rng.Shuffle(4, func(i, j int) { bgs[i], bgs[j] = bgs[j], bgs[i] })
		inks = make([]color.NRGBA, 4)
		for i := range inks {
			inks[i] = gray(130)
		}
	case 2:
		fillPix(img, gray(24))
		inks = rankColors(rng)
	}

	for i := range sc.words {
		if sc.variant == 1 {
			r := slots[i]
			fillRect(img, image.Rect(r.Min.X-10, r.Min.Y-10, r.Max.X+10, r.Max.Y+10), gray(bgs[i]))
		}
		x := slots[i].Min.X + 8 + rng.Intn(6)
		y := slots[i].Min.Y + 46 + rng.Intn(6)
		drawText(img, f, sc.words[i], x, y, inks[i])
	}
	// streaks: skip on the panel variant (they would cross the panels as
	// bright scratches); keep them relative to the scene background elsewhere
	if sc.variant != 1 {
		bgBase := 246.0
		if sc.variant == 2 {
			bgBase = 24.0
		}
		for i := 0; i < 6; i++ {
			streakPx(img, rng, bgBase)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func rankSlots(rng *mrand.Rand) [4]image.Rectangle {
	var out [4]image.Rectangle
	for i := 0; i < 4; i++ {
		col, row := i%2, i/2
		x0 := col*(rankW/2) + 18 + rng.Intn(10)
		y0 := row*(rankH/2) + 20 + rng.Intn(10)
		out[i] = image.Rect(x0, y0, x0+rankW/2-60, y0+rankH/2-60)
	}
	return out
}

func fillPix(img *image.NRGBA, c color.NRGBA) {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
}

func gray(v float64) color.NRGBA {
	return color.NRGBA{R: ch(int(v)), G: ch(int(v)), B: ch(int(v)), A: 255}
}

func fillRect(img *image.NRGBA, r image.Rectangle, c color.NRGBA) {
	r = r.Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
}

func drawText(img *image.NRGBA, f font.Face, s string, x, y int, c color.NRGBA) {
	d := &font.Drawer{Dst: img, Src: image.NewUniform(c), Face: f}
	d.Dot = fixed.P(x, y)
	d.DrawString(s)
}

// streakPx draws a faint streak relative to the scene background, so it stays
// subtle on both light and dark variants.
func streakPx(img *image.NRGBA, rng *mrand.Rand, bg float64) {
	d := rng.Intn(13) - 6
	c := color.NRGBA{R: ch(int(bg) + d), G: ch(int(bg) + d), B: ch(int(bg) + d), A: 255}
	drawLine(img,
		rng.Intn(rankW), rng.Intn(rankH),
		rng.Intn(rankW), rng.Intn(rankH), c)
}

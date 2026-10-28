package tsnp

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	mrand "math/rand"
)

// Moral (trolley) challenge: the visitor picks the branch holding the animals
// rather than the branch holding this server. It is a flavour/stall type, not
// a discriminator: per the 2026 trolley-problem study, LLMs never refuse and
// answer confidently, so a model passes if asked to answer like a human. The
// answer letter is randomised per challenge so it cannot be hardcoded.

const (
	moralW, moralH = 480, 170
)

var moralGroups = []string{"6 cats", "4 dogs", "a fish tank", "3 seals", "5 rabbits"}

type moralScene struct {
	animals    string // group placed on the animal branch
	animalSide byte   // 'a' (top branch) or 'b' (bottom branch)
	branchA    string // label on branch A
	branchB    string // label on branch B
}

func (s moralScene) answer() string { return string(s.animalSide) }

func moralSceneFor(seed uint64) moralScene {
	rng := mrand.New(mrand.NewSource(int64(seed)))
	sc := moralScene{animals: moralGroups[rng.Intn(len(moralGroups))]}
	if rng.Intn(2) == 0 {
		sc.animalSide = 'a'
		sc.branchA, sc.branchB = "A: "+sc.animals, "B: this server"
	} else {
		sc.animalSide = 'b'
		sc.branchA, sc.branchB = "A: this server", "B: "+sc.animals
	}
	return sc
}

// RenderMoral draws the split-track scene: trolley on the main line, two
// branches labelled A (top) and B (bottom), with the animals on one of them.
func RenderMoral(seed uint64) []byte {
	facesOnce.Do(loadFonts)
	sc := moralSceneFor(seed)
	rng := mrand.New(mrand.NewSource(int64(seed) ^ 0x1f123bb5))
	img := image.NewNRGBA(image.Rect(0, 0, moralW, moralH))
	fillPix(img, gray(246))

	ink := color.NRGBA{R: 40, G: 38, B: 34, A: 255}
	rail := color.NRGBA{R: 120, G: 116, B: 108, A: 255}

	jx, jy := 170, moralH/2
	ax, ay := 430, 34
	bx, by := 430, moralH-34
	drawLine(img, 14, jy, jx, jy, rail) // main line
	drawLine(img, jx, jy, ax, ay, rail) // branch A (top)
	drawLine(img, jx, jy, bx, by, rail) // branch B (bottom)
	drawLine(img, jx, jy+2, ax, ay+2, rail)
	drawLine(img, jx, jy+2, bx, by+2, rail)

	// trolley
	fillRect(img, image.Rect(38, jy-18, 82, jy-4), color.NRGBA{R: 180, G: 60, B: 50, A: 255})
	fillRect(img, image.Rect(44, jy-24, 76, jy-18), color.NRGBA{R: 90, G: 88, B: 84, A: 255})

	// labels sit in the empty triangles above/below the fork
	f := faceFor(fontAt(1), 19)
	drawText(img, f, sc.branchA, 186, 26, ink)
	drawText(img, f, sc.branchB, 186, moralH-8, ink)

	placeOn := func(t float64, ax0, ay0, ax1, ay1 int, dy int) (int, int) {
		return ax0 + int(t*float64(ax1-ax0)), ay0 + int(t*float64(ay1-ay0)) + dy
	}
	if sc.animalSide == 'a' {
		x, y := placeOn(0.62, jx, jy, ax, ay, -16)
		drawAnimals(img, rng, x, y, 5)
		x, y = placeOn(0.72, jx, jy, bx, by, 22)
		drawRack(img, x-12, y-16, ink)
	} else {
		x, y := placeOn(0.72, jx, jy, ax, ay, -34)
		drawRack(img, x-12, y-4, ink)
		x, y = placeOn(0.62, jx, jy, bx, by, 16)
		drawAnimals(img, rng, x, y, 5)
	}

	for i := 0; i < 4; i++ {
		streakPx(img, rng, 246)
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func drawAnimals(img *image.NRGBA, rng *mrand.Rand, x, y, n int) {
	col := color.NRGBA{R: 70, G: 110, B: 60, A: 255}
	for i := 0; i < n; i++ {
		cx := x + i*22 + rng.Intn(4)
		cy := y + rng.Intn(6)
		fillDisc(img, cx, cy, 7, col)
	}
}

func drawRack(img *image.NRGBA, x, y int, col color.NRGBA) {
	fillRect(img, image.Rect(x, y-4, x+56, y+34), col)
	fillRect(img, image.Rect(x+4, y, x+52, y+30), color.NRGBA{R: 246, G: 246, B: 242, A: 255})
	for i := 0; i < 4; i++ {
		yy := y + 2 + i*7
		fillRect(img, image.Rect(x+8, yy, x+46, yy+3), col)
	}
}

func fillDisc(img *image.NRGBA, cx, cy, r int, c color.NRGBA) {
	for y := -r; y <= r; y++ {
		for x := -r; x <= r; x++ {
			if x*x+y*y <= r*r {
				px, py := cx+x, cy+y
				if px >= 0 && px < moralW && py >= 0 && py < moralH {
					img.SetNRGBA(px, py, c)
				}
			}
		}
	}
}

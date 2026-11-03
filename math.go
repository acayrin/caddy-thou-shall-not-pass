package tsnp

import (
	"fmt"
	"strconv"
)

// mathFor derives the arithmetic expression and its answer from the render
// seed. Stateless like the other kinds: /c.png redraws exactly what the token
// sealed, and the answer never has to be stored.
//
// Two-digit operands, results never negative, never longer than three digits,
// division only when it comes out even. ASCII 'x' and '/' only - the Go fonts
// have no guaranteed U+00D7/U+00F7 glyphs.
func mathFor(seed uint64) (expr, answer string) {
	rng := newSeedRand(seed)
	switch rng.Intn(4) {
	case 0: // addition
		a, b := 2+rng.Intn(98), 2+rng.Intn(98)
		return fmt.Sprintf("%d + %d =", a, b), strconv.Itoa(a + b)
	case 1: // subtraction, always positive
		a := 11 + rng.Intn(89)
		b := 2 + rng.Intn(a-2)
		return fmt.Sprintf("%d - %d =", a, b), strconv.Itoa(a - b)
	case 2: // multiplication, 2-digit by 1-digit at most
		a, b := 2+rng.Intn(98), 2+rng.Intn(8)
		return fmt.Sprintf("%d x %d =", a, b), strconv.Itoa(a * b)
	default: // division, always exact
		b := 2 + rng.Intn(8)
		q := 2 + rng.Intn(98)
		return fmt.Sprintf("%d / %d =", b*q, b), strconv.Itoa(q)
	}
}

// RenderMath draws the expression with the word-challenge pipeline, minus the
// ghost decoys (they would just be noise for a sum).
func RenderMath(seed uint64) []byte {
	expr, _ := mathFor(seed)
	return RenderText(seed, expr, -1)
}

func digitsOnly(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			b = append(b, c)
		} else if c == '-' && len(b) == 0 {
			b = append(b, c)
		}
	}
	return string(b)
}

package tsnp

import (
	_ "embed"
	mrand "math/rand"
	"strings"
)

//go:embed words.txt
var wordsRaw string

// words is the answer/decoy vocabulary: 4-8 lowercase letters, unique
// (validated in tests). Includes coined entries on purpose - non-dictionary
// strings defeat language-model priors in solvers while a human still just
// types what they see.
var words = strings.Fields(wordsRaw)

func pickWordRng(rng *mrand.Rand) string {
	return words[rng.Intn(len(words))]
}

// pickWord uses the seeded global math/rand (auto-seeded per process).
func pickWord() string {
	return words[mrand.Intn(len(words))]
}

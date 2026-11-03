package tsnp

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// evalExpr checks an expression the way a human would: parse the two operands
// and the operator, compute, compare with the sealed answer.
func evalExpr(t *testing.T, expr, answer string) int {
	t.Helper()
	f := strings.Fields(expr)
	if len(f) != 4 || f[3] != "=" {
		t.Fatalf("bad expression %q", expr)
	}
	a, err1 := strconv.Atoi(f[0])
	b, err2 := strconv.Atoi(f[2])
	if err1 != nil || err2 != nil {
		t.Fatalf("bad operands in %q", expr)
	}
	var want int
	switch f[1] {
	case "+":
		want = a + b
	case "-":
		want = a - b
	case "x":
		want = a * b
	case "/":
		if b == 0 || a%b != 0 {
			t.Fatalf("inexact division %q", expr)
		}
		want = a / b
	default:
		t.Fatalf("bad operator in %q", expr)
	}
	got, err := strconv.Atoi(answer)
	if err != nil {
		t.Fatalf("non-numeric answer %q for %q", answer, expr)
	}
	if got != want {
		t.Fatalf("%q answered %d, want %d", expr, got, want)
	}
	return got
}

func TestMathAnswers(t *testing.T) {
	for s := uint64(0); s < 5000; s++ {
		expr, answer := mathFor(s)
		got := evalExpr(t, expr, answer)
		if got < 0 || got > 999 {
			t.Fatalf("%q = %d out of range", expr, got)
		}
	}
}

func TestMathDeterministic(t *testing.T) {
	for s := uint64(1); s < 200; s++ {
		e1, a1 := mathFor(s)
		e2, a2 := mathFor(s)
		if e1 != e2 || a1 != a2 {
			t.Fatalf("seed %d not deterministic: %q/%q vs %q/%q", s, e1, a1, e2, a2)
		}
	}
}

func TestMathTokenMatchesRender(t *testing.T) {
	a := testApp(t)
	checked := 0
	for i := 0; i < 2000 && checked < 25; i++ {
		kind, ans, tok, err := a.NewChallenge()
		if err != nil {
			t.Fatal(err)
		}
		if kind != KindMath {
			continue
		}
		k, ans2, seed, ok := a.OpenChallenge(tok)
		if !ok || k != KindMath || ans2 != ans {
			t.Fatalf("token mismatch: ok=%v kind=%c ans=%q sealed=%q", ok, k, ans2, ans)
		}
		if _, want := mathFor(seed); want != ans {
			t.Fatalf("render answer %q != sealed %q", want, ans)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no math challenges generated")
	}
}

func TestMathRenderSize(t *testing.T) {
	checkSize(t, RenderMath(42))
}

func TestDigitsOnly(t *testing.T) {
	cases := map[string]string{"= 19": "19", "19": "19", " 1 9 ": "19", "-5": "-5", "abc": ""}
	for in, want := range cases {
		if got := digitsOnly(in); got != want {
			t.Errorf("digitsOnly(%q) = %q, want %q", in, got, want)
		}
	}
	if fmt.Sprint(digitsOnly("3 5")) != "35" {
		t.Errorf("digitsOnly did not collapse spaces")
	}
}

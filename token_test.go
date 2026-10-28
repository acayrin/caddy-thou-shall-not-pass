package tsnp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
)

func testApp(t *testing.T) *App {
	t.Helper()
	a := &App{Secret: "unit-test-secret"}
	if err := a.provision(); err != nil {
		t.Fatalf("provision: %v", err)
	}
	return a
}

func TestChallengeRoundTrip(t *testing.T) {
	a := testApp(t)
	word, token, err := a.NewChallenge()
	if err != nil {
		t.Fatal(err)
	}
	word2, seed, ok := a.OpenChallenge(token)
	if !ok || word2 != word {
		t.Fatalf("open failed: ok=%v word=%q want %q", ok, word2, word)
	}
	_, seed2, _ := a.OpenChallenge(token)
	if seed != seed2 {
		t.Fatal("seed not deterministic for same token")
	}
	// tampered ciphertext
	bad := token[:len(token)-4] + strings.Repeat("A", 4)
	if _, _, ok := a.OpenChallenge(bad); ok {
		t.Fatal("tampered token accepted")
	}
	// wrong app secret
	b := &App{Secret: "other-secret"}
	if err := b.provision(); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := b.OpenChallenge(token); ok {
		t.Fatal("token from other key accepted")
	}
	// expired
	c := &App{Secret: "unit-test-secret", ChallengeTTL: caddy.Duration(-time.Minute)}
	if err := c.provision(); err != nil {
		t.Fatal(err)
	}
	_, tokExp, err := c.NewChallenge()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := c.OpenChallenge(tokExp); ok {
		t.Fatal("expired token accepted")
	}
	// malformed inputs
	for _, s := range []string{"", "!!!", "AAAA"} {
		if _, _, ok := a.OpenChallenge(s); ok {
			t.Fatalf("garbage %q accepted", s)
		}
	}
}

func passReq(t *testing.T, cookie, ua string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Cookie", cookie)
	r.Header.Set("User-Agent", ua)
	return r
}

func TestPassRoundTrip(t *testing.T) {
	a := testApp(t)
	p := passParts{id: "abcd", used: 1, exp: time.Now().Add(time.Hour).Unix(), ua: "deadbeefcafe"}
	cookie := a.CookieName + "=" + a.mkPass(p)
	r := passReq(t, cookie, "Mozilla")
	// ua binding on by default; token ua was "deadbeefcafe" which won't match
	if _, ok := a.parsePass(r); ok {
		t.Fatal("ua mismatch accepted")
	}
	p.ua = uaHash(r)
	cookie = a.CookieName + "=" + a.mkPass(p)
	r = passReq(t, cookie, "Mozilla")
	got, ok := a.parsePass(r)
	if !ok || got.used != 1 || got.id != "abcd" {
		t.Fatalf("valid pass rejected: %v %+v", ok, got)
	}
	// budget exhausted
	p.used = a.Accesses
	r = passReq(t, a.CookieName+"="+a.mkPass(p), "Mozilla")
	if _, ok := a.parsePass(r); ok {
		t.Fatal("exhausted budget accepted")
	}
	// expired
	p.used = 1
	p.exp = time.Now().Add(-time.Minute).Unix()
	r = passReq(t, a.CookieName+"="+a.mkPass(p), "Mozilla")
	if _, ok := a.parsePass(r); ok {
		t.Fatal("expired pass accepted")
	}
	// flipped mac
	p.exp = time.Now().Add(time.Hour).Unix()
	v := a.mkPass(p)
	v = v[:len(v)-2] + "00"
	r = passReq(t, a.CookieName+"="+v, "Mozilla")
	if _, ok := a.parsePass(r); ok {
		t.Fatal("bad mac accepted")
	}
	// ua binding off
	off := false
	a2 := testApp(t)
	a2.UABinding = &off
	p.ua = "anything"
	r = passReq(t, a2.CookieName+"="+a2.mkPass(p), "Whatever")
	if _, ok := a2.parsePass(r); !ok {
		t.Fatal("pass rejected with ua_binding off")
	}
}

func TestCleanRedirect(t *testing.T) {
	for in, want := range map[string]string{
		"/some/path": "/some/path",
		"/?x=1":      "/?x=1",
		"//evil.com": "/",
		"http://x":   "/",
		"":           "/",
		"relative":   "/",
	} {
		if got := cleanRedirect(in); got != want {
			t.Errorf("cleanRedirect(%q)=%q want %q", in, got, want)
		}
	}
}

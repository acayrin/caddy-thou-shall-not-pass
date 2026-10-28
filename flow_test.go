package tsnp

import (
	"bytes"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type dummyNext struct{ called bool }

func (d *dummyNext) ServeHTTP(w http.ResponseWriter, r *http.Request) error {
	d.called = true
	return nil
}

func newFormPost(path string, v url.Values) *http.Request {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.RemoteAddr = "198.51.100.5:1234"
	return r
}

func TestFullFlow(t *testing.T) {
	a := testApp(t)
	e := &Endpoint{app: a}

	// 1. challenge page
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/?r=/some/page", nil)
	r.RemoteAddr = "198.51.100.5:1234"
	if err := e.ServeHTTP(rec, r, nil); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `action="solve"`) {
		t.Fatalf("bad challenge page: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `value="/some/page"`) {
		t.Fatal("redirect target not embedded in form")
	}

	// 2. PNG renders and decodes for a fresh token
	_, _, tok, err := a.NewChallenge()
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, "/c.png?d="+url.QueryEscape(tok), nil)
	if err := e.ServeHTTP(rec, r, nil); err != nil {
		t.Fatal(err)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content-type %q", ct)
	}
	if _, err := png.Decode(bytes.NewReader(rec.Body.Bytes())); err != nil {
		t.Fatalf("png undecodable: %v", err)
	}

	// 3. wrong answer -> fresh challenge page, strike counted
	rec = httptest.NewRecorder()
	r = newFormPost("/solve", url.Values{"d": {tok}, "a": {"totally-wrong"}, "r": {"/some/page"}})
	if err := e.ServeHTTP(rec, r, nil); err != nil {
		t.Fatal(err)
	}
	// wrong answer now uses post/redirect/get: 303 back to the challenge page
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "bad=1") {
		t.Fatalf("wrong-answer flow: code %d loc %q", rec.Code, rec.Header().Get("Location"))
	}

	// 4. correct answer (case-insensitive) -> 303 + pass cookie
	kind, word, tok, err := a.NewChallenge()
	_ = kind
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	r = newFormPost("/solve", url.Values{"d": {tok}, "a": {strings.ToUpper(word)}, "r": {"/some/page"}})
	r.Header.Set("User-Agent", "Mozilla/5.0 test")
	if err := e.ServeHTTP(rec, r, nil); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/some/page" {
		t.Fatalf("solve flow: code %d loc %q", rec.Code, rec.Header().Get("Location"))
	}
	passCookie := ""
	for _, c := range rec.Result().Cookies() {
		if c.Name == a.CookieName {
			passCookie = c.Value
		}
	}
	if passCookie == "" {
		t.Fatal("no pass cookie issued")
	}

	// 5. gate lets the pass through and refreshes budget
	g := &Gate{app: a, BaseURL: "/.tsnp"}
	next := &dummyNext{}
	rec = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, "/some/page", nil)
	r.Header.Set("Cookie", a.CookieName+"="+passCookie)
	r.Header.Set("User-Agent", "Mozilla/5.0 test")
	r.RemoteAddr = "198.51.100.5:1234"
	if err := g.ServeHTTP(rec, r, next); err != nil {
		t.Fatal(err)
	}
	if !next.called {
		t.Fatal("gate blocked a valid pass")
	}

	// 6. no cookie -> redirect to base_url
	next = &dummyNext{}
	rec = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, "/whatever?x=1", nil)
	r.RemoteAddr = "198.51.100.9:9"
	if err := g.ServeHTTP(rec, r, next); err != nil {
		t.Fatal(err)
	}
	if next.called || rec.Code != http.StatusFound {
		t.Fatalf("gate let a cookieless request through: %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/.tsnp/?r="+url.QueryEscape("/whatever?x=1")) {
		t.Fatalf("bad redirect %q", loc)
	}

	// 7. blocked subnet -> 403 even with valid pass
	for i := 0; i < a.MaxFails; i++ {
		a.store.Add(NetKey("198.51.100.5"))
	}
	rec = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, "/some/page", nil)
	r.Header.Set("Cookie", a.CookieName+"="+passCookie)
	r.Header.Set("User-Agent", "Mozilla/5.0 test")
	r.RemoteAddr = "198.51.100.5:1234"
	_ = g.ServeHTTP(rec, r, &dummyNext{})
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "Thou shalt not pass") {
		t.Fatalf("blocked flow: code %d", rec.Code)
	}
}

func TestStaleGetOnSolveRedirects(t *testing.T) {
	app := testApp(t)
	e := &Endpoint{app: app}
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/solve?r=/some/page", nil)
	if err := e.ServeHTTP(rec, r, nil); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("GET /solve should redirect, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "./?r=") {
		t.Fatalf("bad redirect %q", loc)
	}
}

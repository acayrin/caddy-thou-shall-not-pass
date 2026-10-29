package tsnp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func browserReq() *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "acay.cc"
	r.RemoteAddr = "198.51.100.7:443"
	r.Proto = "HTTP/2.0"
	r.Header.Set("Accept", "text/html")
	r.Header.Set("Accept-Language", "en-GB")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("Referer", "https://acay.cc/")
	r.AddCookie(&http.Cookie{Name: "tsnp_pass", Value: "x"})
	return r
}

func curlReq() *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "acay.cc"
	r.RemoteAddr = "198.51.100.7:443"
	r.Proto = "HTTP/1.1"
	return r
}

func TestPassiveRecordsAreNeutral(t *testing.T) {
	p := &Passive{cad: newCadenceStore(100)}
	for _, r := range []*http.Request{browserReq(), curlReq()} {
		rec := p.record(r, 200, 5*time.Millisecond)
		if rec.Score != neutralScore {
			t.Fatalf("passive score must be neutral, got %.2f", rec.Score)
		}
		// raw fields are what matter now
		if rec.Key == "" || rec.Site == "" || rec.Proto == "" {
			t.Fatalf("missing raw fields: %+v", rec)
		}
	}
}

func TestPassiveSkip(t *testing.T) {
	p := &Passive{cad: newCadenceStore(10)}
	for _, path := range []string{"/.tsnp/c.png", "/.well-known/acme-challenge/x"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		if !p.skip(r) {
			t.Fatalf("%s should be skipped", path)
		}
	}
	if p.skip(httptest.NewRequest(http.MethodGet, "/", nil)) {
		t.Fatal("document request must not be skipped")
	}
	// assets are sampled, but the ratio must stay measurable: with many paths
	// some must survive
	kept := 0
	for i := 0; i < 200; i++ {
		r := httptest.NewRequest(http.MethodGet, "/a/"+string(rune('a'+i%26))+".js", nil)
		r.RemoteAddr = "198.51.100." + string(rune('0'+i%10)) + ":1"
		r.Host = "acay.cc"
		if !p.skip(r) {
			kept++
		}
	}
	if kept == 0 || kept == 200 {
		t.Fatalf("asset sampling is degenerate: kept %d/200", kept)
	}
}

func TestPassiveWritesRecords(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.jsonl")
	app := testApp(t)
	app.scorer = newScoreSink(path)
	p := &Passive{app: app, cad: newCadenceStore(100)}

	rec := httptest.NewRecorder()
	if err := p.ServeHTTP(rec, browserReq(), &dummyNext{}); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", rec.Code)
	}
	// writer goroutine is async
	for i := 0; i < 50; i++ {
		if b, _ := os.ReadFile(path); len(b) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no record written")
}

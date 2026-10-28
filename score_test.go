package tsnp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func f64(v float64) *float64 { return &v }

func humanFeats() feats {
	return feats{
		T0FirstMove: f64(420), NMoves: 24, PathEff: 0.86, VPeak: 900, VP95: 700,
		AccelSigns: 5, CoalescedMax: 3, PressureSeen: true,
		TFirstKey: f64(1200), DwellMed: 95, FlightStd: 60, NEdits: 1,
		ClickMs: 110, VisChanges: 1, DwellPage: 6000, Plugins: 5,
	}
}

func botFeats() feats {
	return feats{
		NMoves: 8, PathEff: 1.0, VPeak: 4000, VP95: 4000,
		CoalescedMax: 0, Webdriver: true, ClickMs: 1, DwellPage: 300,
	}
}

func TestScoreSeparates(t *testing.T) {
	h, b := score(humanFeats()), score(botFeats())
	if h < 0.8 {
		t.Fatalf("human-like vector scored too low: %.3f", h)
	}
	if b > 0.3 {
		t.Fatalf("bot-like vector scored too high: %.3f", b)
	}
}

func TestPlausibility(t *testing.T) {
	bad := []feats{
		{NMoves: 0, VPeak: 10},             // kinematic claim without moves
		{NMoves: 5, PathEff: 1.2},          // straight > travelled
		{NMoves: 5, VPeak: 100, VP95: 200}, // percentile inverted
		{NMoves: 5, VPeak: 50000},          // teleporting
		{NMoves: 5, DwellMed: -1},          // impossible timing
		{NMoves: 5, ClickMs: 20000},
	}
	for i, f := range bad {
		if plausible(f) {
			t.Fatalf("case %d should be implausible", i)
		}
	}
	if !plausible(humanFeats()) || !plausible(botFeats()) {
		t.Fatal("real-shaped vectors rejected")
	}
}

func TestDecodeFeats(t *testing.T) {
	if _, ok := decodeFeats(""); ok {
		t.Fatal("empty payload accepted")
	}
	if _, ok := decodeFeats("not json"); ok {
		t.Fatal("garbage accepted")
	}
	p := featPayload{Token: "abc", Time: 1, F: humanFeats()}
	raw, _ := json.Marshal(p)
	got, ok := decodeFeats(string(raw))
	if !ok || got.Token != "abc" {
		t.Fatalf("valid payload rejected: %v", ok)
	}
	if len(tokenTag("abc")) != 8 {
		t.Fatal("tokenTag should be 8 hex chars")
	}
}

func TestAppJSAndPageWiring(t *testing.T) {
	app := testApp(t)
	e := &Endpoint{app: app}

	// asset is served with a JS content type
	rec := httptest.NewRecorder()
	if err := e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app.js", nil), nil); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("app.js not served: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if len(rec.Body.Bytes()) < 1000 {
		t.Fatalf("app.js looks empty (%d bytes)", rec.Body.Len())
	}

	// challenge page carries the collector; the block page must not
	rec = httptest.NewRecorder()
	_ = e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil), nil)
	page := rec.Body.String()
	if !strings.Contains(page, `src="app.js"`) || !strings.Contains(page, `name="f"`) {
		tail := page
		if len(tail) > 300 {
			tail = tail[len(tail)-300:]
		}
		t.Fatalf("challenge page missing collector wiring (status %d, %d bytes, tail: %q)", rec.Code, rec.Body.Len(), tail)
	}
	if strings.Contains(blockHTML, "app.js") {
		t.Fatal("block page must not load the collector")
	}
}

func TestScoreSink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shadow.jsonl")
	s := newScoreSink(path)
	s.write(scoreRecord{TS: 1, Token: "abc12345", Score: 0.87, Kind: "payload", Payload: true})
	s.write(scoreRecord{TS: 2, Token: "def67890", Score: 0.5, Kind: "none"})

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 records, got %d", len(lines))
	}
	if !strings.Contains(lines[0], `"token":"abc12345"`) || !strings.Contains(lines[1], `"kind":"none"`) {
		t.Fatalf("unexpected records: %q", lines)
	}
}

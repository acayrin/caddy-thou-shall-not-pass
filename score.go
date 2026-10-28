package tsnp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"sync"
)

// Behavioural features collected on the challenge page (see web/app.js).
// Phase 1 runs in shadow mode: the score is logged and never changes the
// outcome. No identifiers, no persistence - the vector is per request.
type feats struct {
	T0FirstMove  *float64 `json:"t0_first_move"`
	NMoves       int      `json:"n_moves"`
	PathEff      float64  `json:"path_eff"`
	VPeak        float64  `json:"v_peak"`
	VP95         float64  `json:"v_p95"`
	AccelSigns   int      `json:"accel_signs"`
	CoalescedMax int      `json:"coalesced_max"`
	PressureSeen bool     `json:"pressure_seen"`
	TFirstKey    *float64 `json:"t_first_key"`
	DwellMed     float64  `json:"dwell_med"`
	FlightStd    float64  `json:"flight_std"`
	NEdits       int      `json:"n_edits"`
	Pasted       bool     `json:"pasted"`
	ClickMs      float64  `json:"click_ms"`
	ClicksOff    int      `json:"clicks_off"`
	VisChanges   int      `json:"vis_changes"`
	DwellPage    float64  `json:"dwell_page"`
	Webdriver    bool     `json:"webdriver"`
	Plugins      int      `json:"plugins"`
	Fonts        int      `json:"fonts"`
}

type featPayload struct {
	Token string `json:"d"`
	Time  int64  `json:"t"`
	F     feats  `json:"e"`
}

// neutralScore is used when the payload is absent or unusable (JS disabled,
// old cached page, deliberately omitted). It never counts as evidence either
// way.
const neutralScore = 0.5

// decodeFeats parses the `f` form field. ok=false means "no usable payload".
func decodeFeats(raw string) (featPayload, bool) {
	if raw == "" || len(raw) > 8192 {
		return featPayload{}, false
	}
	var p featPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return featPayload{}, false
	}
	if !plausible(p.F) {
		return featPayload{}, false
	}
	return p, true
}

// plausible rejects vectors that cannot come from a real browser session.
// This is what makes fabricating a vector cost work, rather than one line.
func plausible(f feats) bool {
	if f.NMoves == 0 {
		// no interaction: kinematic claims must be empty
		return f.VPeak == 0 && f.VP95 == 0 && f.PathEff == 0 && f.CoalescedMax == 0
	}
	if f.PathEff < 0 || f.PathEff > 1.001 {
		return false // straight-line distance can never exceed travelled path
	}
	if f.VPeak < f.VP95 || f.VP95 < 0 {
		return false
	}
	if f.VPeak > 30000 { // px/s, far beyond human hand movement
		return false
	}
	if f.AccelSigns < 0 || f.AccelSigns > f.NMoves*4+8 {
		return false
	}
	if f.DwellMed < 0 || f.DwellMed > 3000 {
		return false
	}
	if f.FlightStd < 0 || f.FlightStd > 10000 {
		return false
	}
	if f.ClickMs < 0 || f.ClickMs > 10000 {
		return false
	}
	if f.DwellPage < 0 || f.DwellPage > 3600_000 {
		return false
	}
	if f.T0FirstMove != nil && (*f.T0FirstMove < 0 || *f.T0FirstMove > 600_000) {
		return false
	}
	if f.TFirstKey != nil && (*f.TFirstKey < 0 || *f.TFirstKey > 600_000) {
		return false
	}
	return true
}

// score maps a feature vector to [0,1]. Hand-tuned for shadow calibration only;
// Phase 2 replaces it with a wasm model once real data exists.
func score(f feats) float64 {
	var s float64

	if f.Webdriver {
		s -= 2.0
	}
	switch {
	case f.NMoves == 0:
		s -= 1.0
	case f.NMoves < 3:
		s -= 0.5
	}
	switch {
	case f.PathEff >= 0.985:
		s -= 0.8 // perfect straight lines / linear interpolation
	case f.PathEff >= 0.95:
		s -= 0.3
	case f.PathEff > 0 && f.PathEff < 0.9:
		s += 0.3
	}
	if f.CoalescedMax > 1 {
		s += 0.5
	}
	if f.PressureSeen {
		s += 0.3
	}
	if f.AccelSigns >= 2 {
		s += 0.4
	}
	switch {
	case f.VPeak == 0:
	case f.VPeak > 2500:
		s -= 0.4 // teleporting cursor
	case f.VPeak >= 300:
		s += 0.3
	}
	if f.T0FirstMove != nil {
		if *f.T0FirstMove >= 80 && *f.T0FirstMove <= 5000 {
			s += 0.3
		}
	}
	if f.DwellMed >= 40 && f.DwellMed <= 220 {
		s += 0.3
	}
	if f.FlightStd > 25 {
		s += 0.3
	}
	if f.Pasted {
		s -= 0.5
	}
	if f.ClickMs >= 40 && f.ClickMs <= 300 {
		s += 0.2
	}
	if f.VisChanges >= 1 {
		s += 0.2
	}
	if f.DwellPage >= 1500 && f.DwellPage <= 180_000 {
		s += 0.2
	}
	if f.Plugins > 0 {
		s += 0.1
	}
	if f.ClicksOff > 0 {
		s += 0.1
	}

	return 1 / (1 + math.Exp(-s))
}

// tokenTag is the loggable form of a challenge token: irreversible, 8 hex
// chars, enough to correlate one session's events without storing the token.
func tokenTag(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:4])
}

// scoreSink appends shadow records to a JSONL file. The frontyard caddy only
// emits error-level runtime logs, so the journal is not a usable sink there.
type scoreSink struct {
	mu   sync.Mutex
	path string
}

const scoreLogMax = 64 << 20 // stop growing past 64 MB; calibration data is plenty

func newScoreSink(path string) *scoreSink { return &scoreSink{path: path} }

type scoreRecord struct {
	TS      int64   `json:"ts"`
	Token   string  `json:"token"`
	Score   float64 `json:"score"`
	Kind    string  `json:"kind"`
	Moves   int     `json:"moves"`
	PathEff float64 `json:"path_eff"`
	VPeak   float64 `json:"v_peak"`
	WD      bool    `json:"webdriver"`
	Page    float64 `json:"dwell_ms"`
	Payload bool    `json:"payload"`
}

func (s *scoreSink) write(rec scoreRecord) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if fi, err := os.Stat(s.path); err == nil && fi.Size() > scoreLogMax {
		return
	}
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	b, err := json.Marshal(rec)
	if err != nil {
		return
	}
	b = append(b, '\n')
	_, _ = f.Write(b)
}

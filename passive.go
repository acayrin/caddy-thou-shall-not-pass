package tsnp

import (
	"crypto/sha256"
	"encoding/json"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

// Passive is the global, no-JS recorder: it scores every request from proxy-
// visible signals and appends a shadow record. It is meant to be added to
// every vhost so behavioural data accumulates without page instrumentation.
//
// Shadow only: it never changes a response.
type Passive struct {
	app *App
	cad *cadenceStore
}

func (Passive) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{ID: "http.handlers.tsnp_passive", New: func() caddy.Module { return new(Passive) }}
}

func (p *Passive) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	d.Next()
	return nil
}

func (p *Passive) Provision(ctx caddy.Context) error {
	a, err := lookupApp(ctx)
	if err != nil {
		return err
	}
	p.app = a
	p.cad = newCadenceStore(20000)
	return nil
}

func (p *Passive) Validate() error { return nil }

func (p *Passive) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	if p.skip(r) {
		return next.ServeHTTP(w, r)
	}
	start := time.Now()
	sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
	err := next.ServeHTTP(sw, r)

	rec := p.record(r, sw.status, time.Since(start))
	if p.app != nil && p.app.scorer != nil {
		if b, e := json.Marshal(rec); e == nil {
			p.app.scorer.enqueue(b)
		}
	}
	return err
}

// skip keeps the sink to meaningful traffic: the captcha endpoint has its own
// scorer, well-known probes are machine traffic, and asset requests are
// heavily sampled (they only matter as a ratio against document requests).
func (p *Passive) skip(r *http.Request) bool {
	path := r.URL.Path
	switch {
	case strings.HasPrefix(path, "/.tsnp/"):
		return true
	case strings.HasPrefix(path, "/.well-known/"):
		return true
	case strings.HasPrefix(path, "/.cap/"):
		return true
	}
	if isAsset(path) && assetSample(r) != 0 {
		return true
	}
	return false
}

var assetExts = map[string]bool{
	".js": true, ".mjs": true, ".css": true, ".map": true, ".png": true,
	".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".svg": true,
	".ico": true, ".woff": true, ".woff2": true, ".ttf": true, ".mp4": true,
	".webm": true, ".mp3": true, ".ogg": true, ".json": true, ".xml": true,
}

func isAsset(path string) bool {
	i := strings.LastIndexByte(path, '.')
	if i < 0 || i < len(path)-8 {
		return false
	}
	return assetExts[strings.ToLower(path[i:])]
}

// assetSample deterministically keeps 1 in 8 asset requests (by IP /24, path
// and minute) so the asset:document ratio stays measurable without flooding.
func assetSample(r *http.Request) int {
	sum := sha256.Sum256([]byte(hostOnly(r.RemoteAddr) + "|" + r.URL.Path + "|" + r.Host + "|" + time.Now().Format("2006-01-02T15:04")))
	return int(sum[0] % 8)
}

func hostOnly(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}

type passiveRecord struct {
	TS    int64   `json:"ts"`
	Src   string  `json:"src"`
	Site  string  `json:"site"`
	Key   string  `json:"key"`
	M     string  `json:"m"`
	Path  string  `json:"p"`
	State int     `json:"s"`
	Ms    int64   `json:"ms"`
	DT    int64   `json:"dt_ms"`
	N10   int     `json:"n_10s"`
	Asset int     `json:"asset"`
	Doc   int     `json:"doc"`
	Proto string  `json:"proto"`
	TLSv  string  `json:"tls"`
	ALPN  string  `json:"alpn"`
	SecF  bool    `json:"secf"`
	Acc   bool    `json:"acc"`
	Lang  bool    `json:"lang"`
	Ref   bool    `json:"ref"`
	Ck    bool    `json:"cookie"`
	Score float64 `json:"score"`
}

func (p *Passive) record(r *http.Request, status int, took time.Duration) passiveRecord {
	ip := clientIP(r)
	key := NetKey(ip)
	dt, n10, assets, docs := p.cad.touch(key, isAsset(r.URL.Path))

	u := r.TLS
	rec := passiveRecord{
		TS:    time.Now().Unix(),
		Src:   "passive",
		Site:  r.Host,
		Key:   key,
		M:     r.Method,
		Path:  truncatePath(r.URL.Path),
		State: status,
		Ms:    took.Milliseconds(),
		DT:    dt.Milliseconds(),
		N10:   n10,
		Asset: assets,
		Doc:   docs,
		Proto: r.Proto,
		SecF:  r.Header.Get("Sec-Fetch-Site") != "" || r.Header.Get("Sec-Fetch-Mode") != "",
		Acc:   r.Header.Get("Accept") != "",
		Lang:  r.Header.Get("Accept-Language") != "",
		Ref:   r.Header.Get("Referer") != "",
		Ck:    len(r.Cookies()) > 0,
	}
	if u != nil {
		rec.TLSv = tlsName(u.Version)
		rec.ALPN = u.NegotiatedProtocol
	}
	rec.Score = passiveScore(rec)
	return rec
}

func tlsName(v uint16) string {
	switch v {
	case 0x0304:
		return "1.3"
	case 0x0303:
		return "1.2"
	case 0x0302:
		return "1.1"
	}
	return "?"
}

func truncatePath(p string) string {
	if len(p) > 96 {
		return p[:96]
	}
	return p
}

// passiveScore is deliberately conservative: browser-shaped header sets and a
// healthy doc/asset mix push it up, machine-gun cadence and header-less
// clients push it down. Shadow only.
func passiveScore(r passiveRecord) float64 {
	// neutral baseline below 0: a request with no browser-ish headers at all
	// must land clearly on the machine side
	s := -1.2
	if r.SecF {
		s += 0.8
	}
	if r.Acc {
		s += 0.3
	}
	if r.Lang {
		s += 0.4
	}
	if r.Ref {
		s += 0.2
	}
	if r.Ck {
		s += 0.3
	}
	if r.ALPN == "h2" || r.ALPN == "http/1.1" {
		s += 0.2
	}
	if r.TLSv == "1.3" {
		s += 0.2
	}
	if r.Doc > 0 && r.Asset > 0 {
		s += 0.5 // loads sub-resources like a browser
	}
	switch {
	case r.N10 > 60:
		s -= 1.5 // machine-gun rate from one /24
	case r.N10 > 25:
		s -= 0.6
	}
	if r.DT > 0 && r.DT < 20 {
		s -= 0.4 // sub-20 ms gap between requests
	}
	if r.M == http.MethodHead {
		s -= 0.2
	}
	return 1 / (1 + math.Exp(-s))
}

// statusWriter captures the status code without disturbing the body.
type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusWriter) WriteHeader(code int) {
	if !s.wrote {
		s.status = code
		s.wrote = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if !s.wrote {
		s.status = http.StatusOK
		s.wrote = true
	}
	return s.ResponseWriter.Write(b)
}

func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// ---- cadence ----

type cadenceStore struct {
	mu  sync.Mutex
	max int
	m   map[string]*cadence
}

type cadence struct {
	last        time.Time
	windowStart time.Time
	n           int
	assets      int
	docs        int
}

func newCadenceStore(max int) *cadenceStore {
	return &cadenceStore{max: max, m: map[string]*cadence{}}
}

// touch returns (time since the previous request, requests in the current 10s
// window, asset count, document count) for this key, then updates state.
func (c *cadenceStore) touch(key string, asset bool) (time.Duration, int, int, int) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	e := c.m[key]
	if e == nil {
		if len(c.m) >= c.max {
			for k := range c.m { // cheap eviction: drop an arbitrary entry
				delete(c.m, k)
				break
			}
		}
		e = &cadence{windowStart: now}
		c.m[key] = e
	}
	dt := time.Duration(0)
	if !e.last.IsZero() {
		dt = now.Sub(e.last)
	}
	e.last = now
	if now.Sub(e.windowStart) > 10*time.Second {
		e.windowStart, e.n, e.assets, e.docs = now, 0, 0, 0
	}
	e.n++
	if asset {
		e.assets++
	} else {
		e.docs++
	}
	return dt, e.n, e.assets, e.docs
}

var _ caddyhttp.MiddlewareHandler = (*Passive)(nil)

func parseCaddyFilePassive(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var p Passive
	if err := p.UnmarshalCaddyfile(h.Dispenser); err != nil {
		return nil, err
	}
	return &p, nil
}

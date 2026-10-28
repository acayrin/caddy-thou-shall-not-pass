package tsnp

import (
	"crypto/rand"
	"encoding/hex"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

// Endpoint is the `tsnp_endpoint` handler mounted under the base URL; it
// serves the challenge page, the PNG, and the (plain HTML form, no-JS)
// solve POST. Paths are relative because handle_path strips the base.
type Endpoint struct {
	app *App
}

func (Endpoint) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{ID: "http.handlers.tsnp_endpoint", New: func() caddy.Module { return new(Endpoint) }}
}

func (e *Endpoint) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	d.Next() // consume directive name
	if d.NextArg() {
		return d.ArgErr()
	}
	return nil
}

func (e *Endpoint) Provision(ctx caddy.Context) error {
	a, err := lookupApp(ctx)
	if err != nil {
		return err
	}
	e.app = a
	return nil
}

func (e *Endpoint) Validate() error { return nil }

func (e *Endpoint) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	a := e.app
	key := NetKey(clientIP(r))

	switch r.URL.Path {
	case "/", "":
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return nil
		}
		serveChallenge(w, a, r.URL.Query().Get("r"), "", false)
		return nil

	case "/c.png":
		word, seed, ok := a.OpenChallenge(r.URL.Query().Get("d"))
		if !ok {
			http.Error(w, "invalid or expired challenge", http.StatusBadRequest)
			return nil
		}
		h := w.Header()
		h.Set("Content-Type", "image/png")
		// no-store + no re-encode anywhere in the path: recompression would
		// destroy the ghost-pixel layer.
		h.Set("Cache-Control", "no-store, max-age=0")
		h.Set("X-Content-Type-Options", "nosniff")
		_, err := w.Write(Render(seed, word, a.Decoys))
		_ = err
		return nil

	case "/solve":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return nil
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return nil
		}
		d := r.FormValue("d")
		answer := strings.ToLower(strings.TrimSpace(r.FormValue("a")))
		word, _, valid := a.OpenChallenge(d)
		route := cleanRedirect(r.FormValue("r"))
		switch {
		case !valid:
			// expired/tampered token: fresh challenge, no strike against the
			// client (a slow human and a bot look identical here).
			serveChallenge(w, a, route, "That challenge expired - here's a new one.", false)
		case answer != word:
			blocked := a.store.Add(key)
			if blocked {
				serveBlockPage(w, a)
				return nil
			}
			serveChallenge(w, a, route, "Nope - that's not the bold word.", true)
		default:
			a.store.Clear(key)
			id := make([]byte, 4)
			_, _ = rand.Read(id)
			p := passParts{
				id:   hex.EncodeToString(id),
				used: 1,
				exp:  time.Now().Add(time.Duration(a.PassTTL)).Unix(),
				ua:   uaHash(r),
			}
			a.setPassCookie(w, a.mkPass(p))
			http.Redirect(w, r, route, http.StatusSeeOther)
		}
		return nil

	default:
		http.NotFound(w, r)
		return nil
	}
}

var _ caddyhttp.MiddlewareHandler = (*Endpoint)(nil)

func parseCaddyFileEndpoint(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var e Endpoint
	if err := e.UnmarshalCaddyfile(h.Dispenser); err != nil {
		return nil, err
	}
	return &e, nil
}

// ---- pages ----

const styleCSS = `
body{font:16px/1.6 system-ui,sans-serif;background:#faf9f6;color:#221e1a;
display:flex;min-height:100vh;align-items:center;justify-content:center;margin:0}
main{max-width:520px;padding:2rem;text-align:center}
.cat{width:160px;height:auto}
.challenge{width:min(420px,92vw);height:auto;border:1px solid #ddd7c9;border-radius:10px;background:#fff}
input[type=text]{font:22px ui-monospace,monospace;padding:.5rem .8rem;margin:.8rem auto;
display:block;border:1px solid #c9c2ae;border-radius:8px;text-align:center;width:min(300px,80vw)}
button{font:600 18px/1 system-ui,sans-serif;padding:.7rem 1.6rem;margin-top:.4rem;border:0;
border-radius:8px;background:#e8a33d;color:#3a2a10;cursor:pointer}
button:hover{background:#f0b558}
.bad{color:#b3261e;font-weight:600}
.note{color:#7a7466;font-size:13px;margin-top:1.4rem}
h1{font-size:1.5rem;margin:1rem 0 .3rem}
`

type pageData struct {
	Title   string
	Note    string
	Token   string
	R       string
	Mail    string
	Fail    bool
	Blocked bool
}

var challengeTpl = template.Must(template.New("challenge").Parse(challengeHTML))

var blockTpl = template.Must(template.New("block").Parse(blockHTML))

const challengeHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="noindex">
<title>{{.Title}}</title><style>` + styleCSS + `</style></head>
<body><main>
<img class="cat" src="https://static.acay.cc/imgs/zenacat_question.png" alt="Zena the gatekeeper cat" width="160">
<h1>{{.Title}}</h1>
<p>Type the <strong>bold word</strong> below to open the gate.
The faint shapes are for the machines; your eyes are supposed to ignore them.</p>
{{if .Note}}<p class="bad">{{.Note}}</p>{{end}}
<form method="post" action="solve">
<input type="hidden" name="d" value="{{.Token}}">
<input type="hidden" name="r" value="{{.R}}">
<img class="challenge" src="c.png?d={{.Token}}" width="420" height="132" alt="captcha challenge image">
<input type="text" name="a" autocomplete="off" autocapitalize="none" autocorrect="off"
 spellcheck="false" placeholder="the bold word" required autofocus>
<button type="submit">Pass</button>
</form>
{{if .Mail}}<p class="note">Locked out of something you own? Write to {{.Mail}}.</p>{{end}}
</main></body></html>`

const blockHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="noindex">
<title>Blocked</title><style>` + styleCSS + `</style></head>
<body><main>
<img class="cat" src="https://static.acay.cc/imgs/zenacat_beam.png" alt="Zena the gatekeeper cat, unimpressed" width="160">
<h1>Thou shalt not pass</h1>
<p class="bad">Too many wrong answers from your corner of the internet.
The gate is shut for a while. Come back later - or from somewhere else.</p>
{{if .Mail}}<p class="note">If this is your house, write to {{.Mail}}.</p>{{end}}
</main></body></html>`

func serveChallenge(w http.ResponseWriter, a *App, route, note string, fail bool) {
	_, token, err := a.NewChallenge()
	if err != nil {
		http.Error(w, "challenge generation failed", http.StatusInternalServerError)
		return
	}
	d := pageData{
		Title: a.Title,
		Note:  note,
		Token: token,
		R:     route,
		Mail:  a.Mail,
		Fail:  fail,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = challengeTpl.Execute(w, d)
}

func serveBlockPage(w http.ResponseWriter, a *App) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Retry-After", strconv.Itoa(int(time.Duration(a.BlockTTL).Seconds())))
	w.WriteHeader(http.StatusForbidden)
	_ = blockTpl.Execute(w, pageData{Title: "Blocked", Mail: a.Mail, Blocked: true})
}

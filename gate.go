package tsnp

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

// Gate is the `tsnp` route middleware: checks the pass cookie, refreshes its
// budget, or kicks the client to the challenge endpoint.
type Gate struct {
	BaseURL string `json:"base_url,omitempty"` // default "/.tsnp"

	app *App
}

func (Gate) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{ID: "http.handlers.tsnp", New: func() caddy.Module { return new(Gate) }}
}

func (g *Gate) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	d.Next() // consume directive name
	for nesting := d.Nesting(); d.NextBlock(nesting); {
		opt := d.Val()
		switch opt {
		case "base_url":
			if !d.NextArg() {
				return d.ArgErr()
			}
			g.BaseURL = d.Val()
		default:
			return d.Errf("unknown tsnp subdirective '%s'", opt)
		}
	}
	return nil
}

func (g *Gate) Provision(ctx caddy.Context) error {
	a, err := lookupApp(ctx)
	if err != nil {
		return err
	}
	g.app = a
	if g.BaseURL == "" {
		g.BaseURL = "/.tsnp"
	}
	return nil
}

func (g *Gate) Validate() error { return nil }

func (g *Gate) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	a := g.app
	if a.store.Blocked(NetKey(clientIP(r))) {
		serveBlockPage(w, a)
		return nil
	}
	if p, ok := a.parsePass(r); ok {
		p.used++
		a.setPassCookie(w, a.mkPass(p))
		return next.ServeHTTP(w, r)
	}
	q := url.Values{"r": []string{cleanRedirect(r.URL.RequestURI())}}
	http.Redirect(w, r, g.BaseURL+"/?"+q.Encode(), http.StatusFound)
	return nil
}

// cleanRedirect accepts only same-site absolute paths (no scheme, no
// protocol-relative //).
func cleanRedirect(u string) string {
	if !strings.HasPrefix(u, "/") || strings.HasPrefix(u, "//") {
		return "/"
	}
	return u
}

// clientIP resolves the peer address. PROXY protocol (listener wrapper) and
// direct/LAN connections both land in RemoteAddr as the true client IP.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func lookupApp(ctx caddy.Context) (*App, error) {
	v, err := ctx.App(appName)
	if err != nil {
		return nil, fmt.Errorf("tsnp: add a global `tsnp { }` options block (with secret/secret_env): %w", err)
	}
	a, ok := v.(*App)
	if !ok {
		return nil, fmt.Errorf("tsnp: app %q is not a *tsnp.App", appName)
	}
	return a, nil
}

func parseCaddyFileGate(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var g Gate
	if err := g.UnmarshalCaddyfile(h.Dispenser); err != nil {
		return nil, err
	}
	return &g, nil
}

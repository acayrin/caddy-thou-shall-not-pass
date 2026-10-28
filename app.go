// Package tsnp implements "thou-shall-not-pass": a self-hosted, no-JS
// adversarial word-CAPTCHA gate for Caddy, replacing compute-based proof-of-
// work challenges (which headless browsers solve for free) with an
// attention-cost challenge: one bold true word plus N human-invisible ghost
// decoy words that AI readers latch onto, so solver accuracy collapses to
// 1/(N+1) guessing. Pass tokens are stateless HMAC cookies with a bounded
// request budget; repeated wrong answers block the client's subnet for a TTL
// (same semantics as Cerberus's max_pending, but counting failures).
package tsnp

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
)

const appName = "tsnp"

// App is the global tsnp configuration (mirrors the `tsnp { }` global block,
// like cerberus does). Exactly one exists per Caddy runtime; the gate and
// endpoint handlers look it up via ctx.App("tsnp").
type App struct {
	// signing key material: prefer secret_env (EnvironmentFile), inline
	// secret only for tests.
	Secret    string `json:"secret,omitempty"`
	SecretEnv string `json:"secret_env,omitempty"`

	Decoys       int            `json:"decoys,omitempty"`            // ghost words per image (default 5)
	Accesses     int            `json:"accesses_per_pass,omitempty"` // requests per solved challenge (default 50)
	PassTTL      caddy.Duration `json:"pass_ttl,omitempty"`          // default 30m
	ChallengeTTL caddy.Duration `json:"challenge_ttl,omitempty"`     // default 60s
	MaxFails     int            `json:"max_fails,omitempty"`         // default 25
	FailWindow   caddy.Duration `json:"fail_window,omitempty"`       // default 1h
	BlockTTL     caddy.Duration `json:"block_ttl,omitempty"`         // default 1h
	UABinding    *bool          `json:"ua_binding,omitempty"`        // bind pass to UA+Accept-Language (default true)
	Secure       *bool          `json:"secure_cookies,omitempty"`
	CookieName   string         `json:"cookie_name,omitempty"` // default "tsnp_pass"
	Title        string         `json:"title,omitempty"`
	Mail         string         `json:"mail,omitempty"`

	aesKey, hmacKey []byte
	store           *failStore
}

func (App) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{ID: appName, New: func() caddy.Module { return new(App) }}
}

func (a *App) provision() error {
	secret := a.Secret
	if secret == "" && a.SecretEnv != "" {
		secret = os.Getenv(a.SecretEnv)
	}
	if secret == "" {
		return fmt.Errorf("tsnp: no secret configured (set secret or secret_env)")
	}
	a.aesKey = keyDomain(secret, "aes-256-gcm")
	a.hmacKey = keyDomain(secret, "hmac-sha256")

	if a.Decoys == 0 {
		a.Decoys = 5
	}
	if a.Decoys > 12 {
		a.Decoys = 12
	}
	if a.Accesses == 0 {
		a.Accesses = 50
	}
	if a.PassTTL == 0 {
		a.PassTTL = caddy.Duration(30 * time.Minute)
	}
	if a.ChallengeTTL == 0 {
		a.ChallengeTTL = caddy.Duration(60 * time.Second)
	}
	if a.MaxFails == 0 {
		a.MaxFails = 25
	}
	if a.FailWindow == 0 {
		a.FailWindow = caddy.Duration(time.Hour)
	}
	if a.BlockTTL == 0 {
		a.BlockTTL = caddy.Duration(time.Hour)
	}
	if a.CookieName == "" {
		a.CookieName = "tsnp_pass"
	}
	if a.UABinding == nil {
		t := true
		a.UABinding = &t
	}
	if a.Secure == nil {
		t := true
		a.Secure = &t
	}
	if a.Title == "" {
		a.Title = "Verifying that you're human"
	}
	a.store = newFailStore(a.MaxFails, time.Duration(a.FailWindow), time.Duration(a.BlockTTL))
	return nil
}

func keyDomain(secret, domain string) []byte {
	h := sha256.Sum256([]byte(domain + "|" + secret))
	return h[:]
}

func (a *App) Provision(caddy.Context) error { return a.provision() }
func (a *App) Validate() error               { return a.provision() }
func (a *App) Start() error                  { return nil }
func (a *App) Stop() error                   { return nil }

func (a *App) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	d.Next() // consume directive name
	for nesting := d.Nesting(); d.NextBlock(nesting); {
		opt := d.Val()
		switch opt {
		case "secret", "secret_env", "cookie_name", "title", "mail":
			if !d.NextArg() {
				return d.ArgErr()
			}
			switch opt {
			case "secret":
				a.Secret = d.Val()
			case "secret_env":
				a.SecretEnv = d.Val()
			case "cookie_name":
				a.CookieName = d.Val()
			case "title":
				a.Title = d.Val()
			case "mail":
				a.Mail = d.Val()
			}
		case "decoys", "accesses_per_pass", "max_fails":
			if !d.NextArg() {
				return d.ArgErr()
			}
			n, err := strconv.Atoi(d.Val())
			if err != nil {
				return d.Errf("%s must be an integer: %v", opt, err)
			}
			switch opt {
			case "decoys":
				a.Decoys = n
			case "accesses_per_pass":
				a.Accesses = n
			case "max_fails":
				a.MaxFails = n
			}
		case "pass_ttl", "challenge_ttl", "fail_window", "block_ttl":
			if !d.NextArg() {
				return d.ArgErr()
			}
			dur, err := time.ParseDuration(d.Val())
			if err != nil {
				return d.Errf("%s must be a valid duration: %v", opt, err)
			}
			switch opt {
			case "pass_ttl":
				a.PassTTL = caddy.Duration(dur)
			case "challenge_ttl":
				a.ChallengeTTL = caddy.Duration(dur)
			case "fail_window":
				a.FailWindow = caddy.Duration(dur)
			case "block_ttl":
				a.BlockTTL = caddy.Duration(dur)
			}
		case "ua_binding", "secure_cookies":
			v := true
			if d.NextArg() {
				b, err := strconv.ParseBool(d.Val())
				if err != nil {
					return d.Errf("%s must be true or false", opt)
				}
				v = b
			}
			a.setBool(opt, &v)
		default:
			return d.Errf("unknown tsnp subdirective '%s'", opt)
		}
	}
	return nil
}

func (a *App) setBool(opt string, v *bool) {
	switch opt {
	case "ua_binding":
		a.UABinding = v
	case "secure_cookies":
		a.Secure = v
	}
}

// ParseCaddyFileApp backs the `tsnp { ... }` global options block.
func ParseCaddyFileApp(d *caddyfile.Dispenser, _ any) (any, error) {
	var a App
	err := a.UnmarshalCaddyfile(d)
	return httpcaddyfile.App{
		Name:  appName,
		Value: caddyconfig.JSON(a, nil),
	}, err
}

func init() {
	caddy.RegisterModule(App{})
	caddy.RegisterModule(Gate{})
	caddy.RegisterModule(Endpoint{})
	httpcaddyfile.RegisterHandlerDirective("tsnp", parseCaddyFileGate)
	httpcaddyfile.RegisterHandlerDirective("tsnp_endpoint", parseCaddyFileEndpoint)
	httpcaddyfile.RegisterGlobalOption(appName, ParseCaddyFileApp)
	// gate runs before any serve directive (respond/file_server/reverse_proxy);
	// endpoint is only ever used inside its own handle_path/route.
	httpcaddyfile.RegisterDirectiveOrder("tsnp", "after", "header")
	httpcaddyfile.RegisterDirectiveOrder("tsnp_endpoint", "after", "header")
}

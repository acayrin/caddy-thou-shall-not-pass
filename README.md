# caddy-thou-shall-not-pass (`tsnp`)

A self-hosted, no-JavaScript, adversarial **word-CAPTCHA gate** for Caddy.
It replaces compute-based proof-of-work challenges (Anubis/Cerberus), which
headless browsers solve for free, with an *attention* challenge that makes
sustained scraping economically expensive.

## How it works

- Gated requests without a valid pass cookie get a 302 to a challenge page:
  one PNG plus a plain HTML form (no JS needed to solve or submit).
- The PNG shows **one bold true word** over a noise underlay; hidden in the
  same image are **N ghost words** rendered within a human-imperceptible
  channel-delta band (±3..8) of the background. Human eyes see nothing;
  OCR/VLM encoders read them as full-strength text - so an automated solver
  has no way to tell which word is the answer and accuracy collapses to
  roughly `1/(N+1)` guessing.
- Glyphs of the true word overlap (~28%) and jitter vertically, defeating
  character-segmentation pipelines.
- The challenge answer travels inside an AES-256-GCM sealed token (stateless,
  short TTL); the render seed is derived from the token so `c.png` is
  byte-reproducible without server storage. Serve it with `no-store` and
  never let anything recompress it - re-encoding destroys the ghost layer.
- Correct solve ⇒ a stateless HMAC pass cookie with a bounded request budget
  (default 50 accesses / 30 min), optionally bound to UA + Accept-Language
  (not IP, to survive rotating proxy exits). The gate re-issues the cookie
  with `used+1` on every request; when the budget runs out the client is
  re-challenged. This rotation is what makes the captcha *scale* attacker
  cost: solve price (ML APIs or human farms) × requests ÷ budget.
- Repeated wrong answers ⇒ the client's subnet (IPv4 /24, IPv6 /64) gets
  blocked for `block_ttl` (403), mirroring Cerberus's `max_pending` but
  counting explicit failures.

Honest caveat: this is a cost tax, not an impenetrable wall. Contrast-
stretching preprocessors and frontier VLMs solve some fraction; the goal is
that a botnet pays $0.50-3.00 per 1k solves *per 50 requests*, not per
session.

## Caddyfile

Global options (exactly one per runtime; also the source of defaults):

```caddy
{
    tsnp {
        secret_env TSNP_SECRET   # or `secret` (env recommended)
        decoys 5                 # ghost words per image
        accesses_per_pass 50     # requests allowed per solved challenge
        pass_ttl 30m
        challenge_ttl 60s
        max_fails 25             # wrong answers per subnet before block
        fail_window 1h
        block_ttl 1h
        cookie_name "tsnp_pass"
        title "Verifying that you're human"
        mail "webmaster@example.org"
        # ua_binding true|false      (default true)
        # secure_cookies true|false  (default true)
    }
}

(tsnp) {
    handle_path /.tsnp/* {
        tsnp_endpoint
    }
    @tsnp {
        not path /.tsnp/*
    }
    tsnp @tsnp {
        base_url "/.tsnp"
    }
}

secure.example {
    import tsnp
    reverse_proxy backend:8080
}
```

Non-browser clients (git smart-HTTP, webdav, APIs, ACME, feed readers)
cannot type into a picture; exempt them with extra `not path ...` matchers
in your `@tsnp` block.

## Endpoints

```
GET  {base}/       challenge page (?r= return path)
GET  {base}/c.png  challenge PNG  (?d= sealed token)
POST {base}/solve  d, a (answer), r (return path)
```

## Development

```
go test ./...          # pure Go, no network needed
go vet ./...
```

MIT licensed. Mascot URLs in the bundled pages are placeholders from my own
site (static.acay.cc); override by templating or host your own images.

# caddy-thou-shall-not-pass (`tsnp`)

A self-hosted, no-JavaScript, adversarial **word-CAPTCHA gate** for Caddy.
It replaces compute-based proof-of-work challenges (Anubis/Cerberus), which
headless browsers solve for free, with an *attention* challenge that makes
sustained scraping economically expensive.

## How it works

Three challenge types rotate randomly (55% word, 20% illusion, 25% trolley):

1. **Word**: one dark word rendered with overlapping, baseline-jittered,
   wave-distorted glyphs and cut-out specks, plus N faint decoy words in a
   luminance +/-3..8 band. Every challenge draws its own render profile
   (font family regular/bold/medium/mono, glyph overlap 68-88%, wave amplitude
   and wavelength, background texture) and the decoys share that exact
   profile, so style never identifies the real word and a solver tuned on one
   render style does not transfer. OCR pipelines die on the overlap; VLMs read
   the decoys too but must still pick the dark one.
2. **Müller-Lyer illusion**: two exactly equal vertical lines, one with
   outward fins (perceived longer by humans) and one with inward fins. The
   answer is the perceived-longer side (l/r) - a pixel-measuring model sees
   equality, and a "read the text" OCR stack sees no text at all.

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
  (default 1000 accesses / 30 min), optionally bound to UA + Accept-Language
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
        accesses_per_pass 1000   # requests allowed per solved challenge
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

package tsnp

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	mrand "math/rand"
)

// ---- challenge tokens (stateless) ----
//
// d = base64url( nonce[12] || AES-256-GCM(key, nonce, exp[8]||word) )
// GCM authentication covers expiry and word; no server-side storage.
// The render seed is derived from the token blob so c.png can regenerate the
// exact same image (true word + ghost decoys + noise) from the token alone.

func (a *App) aead() (cipher.AEAD, error) {
	block, err := aes.NewCipher(a.aesKey)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Challenge kinds sealed in the token.
const (
	KindWord     = 'w' // answer: the bold word
	KindIllusion = 'i' // answer: "l" or "r" (Muller-Lyer perceived-longer side)
)

// NewChallenge returns (kind, answer, token). Kind rotates randomly per
// challenge so generic OCR/"read the text" solver stacks fail outright on
// the illusion half.
func (a *App) NewChallenge() (kind byte, answer, token string, err error) {
	nonce := make([]byte, 12)
	if _, err = rand.Read(nonce); err != nil {
		return 0, "", "", err
	}
	// the render seed comes from the nonce, so the illusion side (and with it
	// the sealed answer) must be derived from the same seed the renderer uses
	if mrand.Intn(2) == 0 {
		kind, answer = KindWord, pickWord()
	} else {
		kind = KindIllusion
		answer = IllusionAnswer(nonceSeed(nonce))
	}
	exp := uint64(time.Now().Add(time.Duration(a.ChallengeTTL)).Unix())
	pt := make([]byte, 9, 9+len(answer))
	binary.BigEndian.PutUint64(pt, exp)
	pt[8] = kind
	pt = append(pt, answer...)
	gcm, err := a.aead()
	if err != nil {
		return 0, "", "", err
	}
	blob := gcm.Seal(nonce, nonce, pt, nil)
	return kind, answer, base64.RawURLEncoding.EncodeToString(blob), nil
}

// OpenChallenge decrypts and checks expiry; returns kind, answer and the
// render seed. Tampered/expired tokens yield ok=false.
func (a *App) OpenChallenge(token string) (kind byte, answer string, seed uint64, ok bool) {
	blob, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(blob) < 12+12+10 {
		return 0, "", 0, false
	}
	gcm, err := a.aead()
	if err != nil {
		return 0, "", 0, false
	}
	pt, err := gcm.Open(nil, blob[:12], blob[12:], nil)
	if err != nil || len(pt) < 10 {
		return 0, "", 0, false
	}
	exp := binary.BigEndian.Uint64(pt[:8])
	if time.Now().Unix() > int64(exp) {
		return 0, "", 0, false
	}
	kind = pt[8]
	if kind != KindWord && kind != KindIllusion {
		return 0, "", 0, false
	}
	return kind, string(pt[9:]), nonceSeed(blob[:12]), true
}

// nonceSeed derives the deterministic render seed.
func nonceSeed(nonce []byte) uint64 {
	sum := sha256.Sum256(nonce)
	return binary.BigEndian.Uint64(sum[:8])
}

// IllusionAnswer is the side humans perceive as longer for a given seed:
// "l" when the left line carries the outward fins.
func IllusionAnswer(seed uint64) string {
	if seed%2 == 0 {
		return "l"
	}
	return "r"
}

// ---- pass cookies (stateless, bounded budget) ----
//
// tsnp_pass = v1.<id>.<used>.<expUnix>.<uaHash>.<mac>
// mac = HMAC-SHA256(hmacKey, "pass|"+all above)[:12] hex.
// Each validated request re-issues the cookie with used+1; when used reaches
// the configured budget (or exp passes) the client gets re-challenged.
// Bound (optionally) to UA+Accept-Language, never to IP (proxy exit churn).

type passParts struct {
	id   string
	used int
	exp  int64
	ua   string
}

func (a *App) macPass(body string) string {
	m := hmac.New(sha256.New, a.hmacKey)
	m.Write([]byte("pass|" + body))
	return hex.EncodeToString(m.Sum(nil)[:12])
}

func (a *App) mkPass(p passParts) string {
	body := "v1." + p.id + "." + strconv.Itoa(p.used) + "." + strconv.FormatInt(p.exp, 10) + "." + p.ua
	return body + "." + a.macPass(body)
}

func (a *App) parsePass(r *http.Request) (passParts, bool) {
	c, err := r.Cookie(a.CookieName)
	if err != nil || c.Value == "" {
		return passParts{}, false
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 6 || parts[0] != "v1" {
		return passParts{}, false
	}
	body := strings.Join(parts[:5], ".")
	macB, err := hex.DecodeString(parts[5])
	if err != nil || !hmac.Equal(macB, mustHex(a.macPass(body))) {
		return passParts{}, false
	}
	p := passParts{id: parts[1], ua: parts[4]}
	if p.used, err = strconv.Atoi(parts[2]); err != nil {
		return passParts{}, false
	}
	if p.exp, err = strconv.ParseInt(parts[3], 10, 64); err != nil {
		return passParts{}, false
	}
	if time.Now().Unix() > p.exp || p.used >= a.Accesses {
		return passParts{}, false
	}
	if *a.UABinding && p.ua != uaHash(r) {
		return passParts{}, false
	}
	return p, true
}

func mustHex(s string) []byte {
	b, _ := hex.DecodeString(s)
	return b
}

func uaHash(r *http.Request) string {
	h := sha256.Sum256([]byte(r.UserAgent() + "\x00" + r.Header.Get("Accept-Language")))
	return hex.EncodeToString(h[:6])
}

func (a *App) setPassCookie(w http.ResponseWriter, val string) {
	http.SetCookie(w, &http.Cookie{
		Name:     a.CookieName,
		Value:    val,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   *a.Secure,
		MaxAge:   int(time.Duration(a.PassTTL).Seconds()),
	})
}

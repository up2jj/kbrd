package web

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"sync"
	"time"
)

const sessionCookie = "kbrd_session"

// auth implements the shared-token login: POST /login compares against the
// token in constant time and sets an HMAC cookie derived from a boot-random
// key, so a forged cookie requires the key and restarting invalidates all
// sessions.
type auth struct {
	token   string
	key     []byte // boot-random HMAC key
	secure  bool   // Secure cookie attribute (on when TLS is active)
	limiter loginLimiter
}

func newAuth(token string, secure bool) *auth {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic(err) // crypto/rand failure is unrecoverable
	}
	return &auth{token: token, key: key, secure: secure}
}

// sessionValue is the expected cookie value.
func (a *auth) sessionValue() string {
	mac := hmac.New(sha256.New, a.key)
	mac.Write([]byte(a.token))
	return hex.EncodeToString(mac.Sum(nil))
}

// validCookie reports whether the request carries a valid session.
func (a *auth) validCookie(r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(a.sessionValue())) == 1
}

// login checks the submitted token (rate-limited per IP) and, on success,
// sets the session cookie. Returns whether the login succeeded.
func (a *auth) login(w http.ResponseWriter, r *http.Request, submitted string) bool {
	ip := clientIP(r)
	if !a.limiter.allow(ip) {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(submitted), []byte(a.token)) != 1 {
		a.limiter.fail(ip)
		return false
	}
	a.limiter.reset(ip)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    a.sessionValue(),
		Path:     "/",
		MaxAge:   30 * 24 * 60 * 60, // advisory; real revocation = restart/rotation
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   a.secure,
	})
	return true
}

// logout clears the session cookie.
func (a *auth) logout(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   a.secure,
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// loginLimiter applies exponential backoff per client IP: after each failed
// attempt the IP must wait 2^(n-1) seconds (capped at 60) before the next try
// counts. State is in-memory; a restart forgives everyone.
type loginLimiter struct {
	mu sync.Mutex
	m  map[string]*loginState
}

type loginState struct {
	fails int
	until time.Time
}

func (l *loginLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	st := l.m[ip]
	return st == nil || time.Now().After(st.until)
}

func (l *loginLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.m == nil {
		l.m = make(map[string]*loginState)
	}
	st := l.m[ip]
	if st == nil {
		st = &loginState{}
		l.m[ip] = st
	}
	st.fails++
	backoff := min(time.Second<<min(st.fails-1, 6), time.Minute) // 1s,2s,4s,… capped at 60s
	st.until = time.Now().Add(backoff)
}

func (l *loginLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, ip)
}

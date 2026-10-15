package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"time"
)

const (
	// SessionCookieName adalah nama cookie session di browser.
	SessionCookieName = "session"
	// SessionTTL adalah umur session; cookie dan baris DB kedaluwarsa bersama.
	SessionTTL = 24 * time.Hour
)

// NewSessionToken membuat token acak untuk cookie (diberikan ke browser)
// beserta hash SHA-256-nya (yang disimpan di database). Token mentah tidak
// pernah masuk DB, jadi kebocoran database tidak membocorkan session.
func NewSessionToken() (token, tokenHash string, err error) {
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, HashToken(token), nil
}

// HashToken mengubah token cookie menjadi hash untuk pencarian di database.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// SetSessionCookie memasang cookie session yang aman: HttpOnly (JavaScript
// tidak bisa membaca), SameSite=Lax (tahan CSRF untuk form biasa), dan
// Secure hanya saat di belakang HTTPS (Tailscale serve / TLS).
func SetSessionCookie(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(SessionTTL),
		MaxAge:   int(SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie menghapus cookie session di browser. Atribut
// HttpOnly, SameSite=Lax, dan Secure (kondisional sesuai config) dipasang
// sama seperti SetSessionCookie supaya browser mengenali cookie yang dihapus.
func ClearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

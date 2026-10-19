package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"net"
	"net/http"
	"sync"
	"time"

	"accounting/internal/auth"
	"accounting/internal/database/db"
	"accounting/templates"
)

// loginFailures membatasi percobaan login gagal per IP: ditolak bila sudah
// ada 5 percobaan gagal dalam satu menit terakhir (in-memory, tanpa
// dependensi baru; hilang saat server restart). Known limitation yang
// diterima untuk deployment single instance: setelah restart penghitung
// kembali nol, jadi penyerang bisa memperoleh 5 percobaan baru per restart.
// Persist ke DB sengaja tidak dilakukan (over-engineering untuk skala ini).
var loginFailures = newFailLimiter(5, time.Minute)

// dummyBcryptHash adalah hash bcrypt valid dari password acak, dibuat sekali
// saat proses start. Dipakai saat username tidak ditemukan supaya waktu
// respons login tidak membocorkan keberadaan username (anti timing
// enumeration).
var dummyBcryptHash = mustDummyBcryptHash()

func mustDummyBcryptHash() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	hash, err := auth.HashPassword(base64.RawURLEncoding.EncodeToString(buf))
	if err != nil {
		panic(err)
	}
	return hash
}

// failLimiter mencatat waktu setiap percobaan gagal per IP. Catatan yang
// lebih lama dari window dibuang setiap kali diperiksa.
type failLimiter struct {
	mu     sync.Mutex
	fails  map[string][]time.Time
	limit  int
	window time.Duration
}

func newFailLimiter(limit int, window time.Duration) *failLimiter {
	return &failLimiter{fails: make(map[string][]time.Time), limit: limit, window: window}
}

// blocked melaporkan apakah IP sudah mencapai batas percobaan gagal.
func (l *failLimiter) blocked(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	kept := l.prune(ip)
	if len(kept) == 0 {
		delete(l.fails, ip)
		return false
	}
	l.fails[ip] = kept
	return len(kept) >= l.limit
}

// fail mencatat satu percobaan gagal untuk IP tersebut.
func (l *failLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fails[ip] = append(l.prune(ip), time.Now())
}

// reset menghapus catatan IP; dipanggil setelah login berhasil.
func (l *failLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, ip)
}

// prune menyaring catatan yang sudah lebih tua dari window.
func (l *failLimiter) prune(ip string) []time.Time {
	cut := time.Now().Add(-l.window)
	old := l.fails[ip]
	kept := make([]time.Time, 0, len(old))
	for _, t := range old {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	return kept
}

// clientIP mengambil alamat IP client dari r.RemoteAddr (alamat koneksi
// nyata). Header X-Forwarded-For / X-Real-IP sengaja TIDAK dipercaya: klien
// bisa mengisinya sendiri untuk memalsukan IP dan melewati rate limit.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// LoginGet menampilkan halaman login.
func (h *Handlers) LoginGet(w http.ResponseWriter, r *http.Request) {
	// Sudah login? Langsung ke beranda.
	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil && cookie.Value != "" {
		if _, err := h.DB.GetSessionWithUser(r.Context(), auth.HashToken(cookie.Value)); err == nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
	}
	// First-run: database belum punya satu pengguna pun, arahkan ke halaman
	// setup supaya akun pertama dibuat sebelum ada yang bisa masuk.
	exists, err := h.hasAnyUser(r)
	if err == nil && !exists {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	h.renderLogin(w, r, "")
}

// LoginPost memvalidasi kredensial lalu membuat session baru.
// Daftar perusahaan hanya ditampilkan SETELAH login, sesuai kebutuhan:
// halaman pilih perusahaan hanya memuat perusahaan milik user tersebut.
func (h *Handlers) LoginPost(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if loginFailures.blocked(ip) {
		h.renderLoginStatus(w, r, http.StatusTooManyRequests,
			"Terlalu banyak percobaan login. Tunggu satu menit lalu coba lagi.")
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "400 form tidak valid", http.StatusBadRequest)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")

	user, err := h.DB.GetUserByUsername(r.Context(), username)
	if err != nil {
		// Username tidak ditemukan: tetap jalankan bcrypt terhadap hash
		// dummy (bcrypt.CompareHashAndPassword lewat auth.VerifyPassword)
		// supaya waktu respons sama dan keberadaan username tidak bocor.
		auth.VerifyPassword(dummyBcryptHash, password)
		loginFailures.fail(ip)
		// Pesan error sengaja umum: tidak membocorkan apakah username terdaftar.
		h.renderLogin(w, r, "Username atau password salah")
		return
	}
	if !auth.VerifyPassword(user.PasswordHash, password) {
		loginFailures.fail(ip)
		h.renderLogin(w, r, "Username atau password salah")
		return
	}
	loginFailures.reset(ip)

	token, tokenHash, err := auth.NewSessionToken()
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}

	_, err = h.DB.CreateSession(r.Context(), db.CreateSessionParams{
		UserID:    user.ID,
		TokenHash: tokenHash,
		ExpiresAt: timestamptz(time.Now().Add(auth.SessionTTL)),
	})
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}

	auth.SetSessionCookie(w, token, h.CookieSecure)
	// Mendarat di halaman pilih perusahaan milik user.
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// LogoutPost mengakhiri session di database dan menghapus cookie.
func (h *Handlers) LogoutPost(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil && cookie.Value != "" {
		if row, err := h.DB.GetSessionWithUser(r.Context(), auth.HashToken(cookie.Value)); err == nil {
			_ = h.DB.DeleteSession(r.Context(), row.SessionID)
		}
	}
	auth.ClearSessionCookie(w, h.CookieSecure)

	// HTMX: minta browser pindah halaman penuh.
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/login")
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *Handlers) renderLogin(w http.ResponseWriter, r *http.Request, errMsg string) {
	status := http.StatusOK
	if errMsg != "" {
		status = http.StatusUnauthorized
	}
	h.renderLoginStatus(w, r, status, errMsg)
}

// renderLoginStatus merender halaman login dengan status HTTP tertentu
// (mis. 429 saat rate limit menolak percobaan login).
func (h *Handlers) renderLoginStatus(w http.ResponseWriter, r *http.Request, status int, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	_ = templates.LoginPage(errMsg).Render(r.Context(), w)
}

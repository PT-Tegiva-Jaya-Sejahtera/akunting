// Package middleware memuat middleware chi untuk autentikasi, scoping
// perusahaan (multi-tenant), dan RBAC per perusahaan.
package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"accounting/internal/auth"
	"accounting/internal/database/db"
	"accounting/internal/models"
)

// Role yang dikenali sistem, dari yang paling rendah:
// staff (input jurnal), admin (kelola pembukuan), owner (kelola user & peran).
const (
	RoleStaff  = "staff"
	RoleAdmin  = "admin"
	RoleOwner  = "owner"
)

type contextKey int

const (
	ctxUserKey contextKey = iota
	ctxRoleKey
)

// SessionUser adalah data user yang sudah terautentikasi, tersimpan di context.
type SessionUser struct {
	UserID   int64
	Username string
	FullName string
}

// ctxCompanyKey adalah kunci context untuk perusahaan aktif hasil verifikasi
// RequireCompany.
type ctxKeyCompany struct{}

var ctxCompanyKey = ctxKeyCompany{}

// CompanyFromContext mengambil perusahaan aktif yang sudah diverifikasi.
func CompanyFromContext(ctx context.Context) (db.Company, bool) {
	c, ok := ctx.Value(ctxCompanyKey).(db.Company)
	return c, ok
}

// UserFromContext mengambil data user hasil autentikasi.
func UserFromContext(ctx context.Context) (SessionUser, bool) {
	u, ok := ctx.Value(ctxUserKey).(SessionUser)
	return u, ok
}

// RoleFromContext mengambil role user di perusahaan aktif.
func RoleFromContext(ctx context.Context) (string, bool) {
	r, ok := ctx.Value(ctxRoleKey).(string)
	return r, ok
}

func roleRank(role string) int {
	switch role {
	case RoleOwner:
		return 3
	case RoleAdmin:
		return 2
	case RoleStaff:
		return 1
	default:
		return 0
	}
}

// CSRFProtect menolak request yang mengubah state (POST/PUT/PATCH/DELETE)
// bila asal request (header Origin, fallback Referer) bukan host yang menerima
// request. Header yang tidak ada atau tidak cocok ditolak 403, sehingga form
// di situs lain tidak bisa dipakai untuk menjalankan aksi atas nama user yang
// sudah login. Request GET/HEAD tidak disentuh.
func CSRFProtect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			next.ServeHTTP(w, r)
			return
		}
		if !sameOrigin(r) {
			http.Error(w, "403 permintaan ditolak (origin tidak cocok)", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sameOrigin membandingkan host pada Origin (fallback Referer) dengan r.Host.
func sameOrigin(r *http.Request) bool {
	src := r.Header.Get("Origin")
	if src == "" {
		src = r.Header.Get("Referer")
	}
	if src == "" {
		return false
	}
	u, err := url.Parse(src)
	if err != nil {
		return false
	}
	return u.Host != "" && strings.EqualFold(u.Host, r.Host)
}

// RequireAuth memastikan request membawa session cookie yang sah.
// Data user disimpan ke context untuk handler berikutnya. Parameter
// cookieSecure diteruskan ke ClearSessionCookie supaya atribut cookie saat
// penghapusan sama dengan saat pemasangan.
func RequireAuth(q *db.Queries, cookieSecure bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(auth.SessionCookieName)
			if err != nil || cookie.Value == "" {
				denyUnauthenticated(w, r)
				return
			}

			row, err := q.GetSessionWithUser(r.Context(), auth.HashToken(cookie.Value))
			if err != nil {
				// Token tidak dikenal atau session sudah kedaluwarsa.
				auth.ClearSessionCookie(w, cookieSecure)
				denyUnauthenticated(w, r)
				return
			}

			user := SessionUser{UserID: row.UserID, Username: row.Username, FullName: row.FullName}
			// Catat aktivitas terakhir user (paling sering sekali per menit).
			_, _ = q.TouchUserActivity(r.Context(), row.UserID)
			ctx := context.WithValue(r.Context(), ctxUserKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireCompany memverifikasi bahwa user aktif memiliki akses ke perusahaan
// pada URL (/companies/{companyKey}/...) lalu menyimpan perusahaan dan role-nya
// ke context. Ini titik kunci keamanan multi-tenant: sebelum query apa pun
// dijalankan, hak akses perusahaan sudah dipastikan. Nomor id perusahaan tidak
// pernah muncul di URL supaya urutan pembukuan tidak bisa ditebak.
func RequireCompany(q *db.Queries) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := UserFromContext(r.Context())
			if !ok {
				denyUnauthenticated(w, r)
				return
			}

			key := chi.URLParam(r, "companyKey")
			if !models.CompanyKeyValid(key) {
				http.Error(w, "404 perusahaan tidak ditemukan", http.StatusNotFound)
				return
			}
			company, err := q.GetCompanyByKey(r.Context(), key)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					http.Error(w, "404 perusahaan tidak ditemukan", http.StatusNotFound)
					return
				}
				http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
				return
			}

			role, err := q.GetUserRoleInCompany(r.Context(), db.GetUserRoleInCompanyParams{
				UserID:    user.UserID,
				CompanyID: company.ID,
			})
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					// User tidak punya hubungan dengan perusahaan ini.
					http.Error(w, "403 akses ditolak", http.StatusForbidden)
					return
				}
				http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
				return
			}

			ctx := context.WithValue(r.Context(), ctxRoleKey, role)
			ctx = context.WithValue(ctx, ctxCompanyKey, company)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole membatasi route pada role minimal tertentu.
// Hanya dipasang setelah RequireCompany sehingga role sudah tersedia.
func RequireRole(minRole string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, ok := RoleFromContext(r.Context())
			if !ok {
				http.Error(w, "403 akses ditolak", http.StatusForbidden)
				return
			}
			if roleRank(role) < roleRank(minRole) {
				http.Error(w, "403 role tidak mencukupi", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r.WithContext(r.Context()))
		})
	}
}

// denyUnauthenticated mengarahkan ke halaman login. Untuk request HTMX
// dipakai header HX-Redirect supaya browser pindah halaman penuh.
func denyUnauthenticated(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/login")
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

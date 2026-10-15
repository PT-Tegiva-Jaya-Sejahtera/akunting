// Package handlers memuat HTTP handler untuk autentikasi dan halaman SSR.
package handlers

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"accounting/internal/database/db"
)

// timestamptz membungkus time.Time ke tipe pgtype yang dipakai sqlc.
func timestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true, InfinityModifier: pgtype.Finite}
}

// Handlers membawa dependensi bersama untuk semua handler.
type Handlers struct {
	DB *db.Queries
	// CookieSecure = true saat aplikasi disajikan lewat HTTPS (Tailscale serve).
	CookieSecure bool
	// PublicBaseURL = alamat publik aplikasi (env PUBLIC_BASE_URL), dipakai
	// untuk menyusun link undangan. Bila kosong, fallback https:// + r.Host.
	PublicBaseURL string
}

// New membuat Handlers dengan dependensi yang diberikan.
func New(q *db.Queries, cookieSecure bool, publicBaseURL string) *Handlers {
	return &Handlers{DB: q, CookieSecure: cookieSecure, PublicBaseURL: publicBaseURL}
}

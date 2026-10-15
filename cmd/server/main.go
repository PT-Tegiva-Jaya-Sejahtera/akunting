// Command server adalah entrypoint aplikasi akuntansi multi-perusahaan.
// Monolitik: satu binary, semua aset statis ter-embed via go:embed,
// SSR dengan templ + HTMX.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"accounting/internal/database/db"
	"accounting/internal/handlers"
	appmw "accounting/internal/middleware"
	"accounting/static"
)

// Config dibaca dari environment; nilai default aman untuk pengembangan.
type Config struct {
	ListenAddr   string        // alamat bind, mis. "100.x.x.x:8080" (Tailscale)
	DatabaseURL  string        // postgres://user@host:5432/db
	CookieSecure bool          // true saat di belakang HTTPS (Tailscale serve)
	ShutdownTTL  time.Duration // grace period saat SIGINT/SIGTERM
	// PublicBaseURL = alamat publik aplikasi untuk menyusun link undangan.
	// Dibaca sekali saat start dari env PUBLIC_BASE_URL; bila kosong, handler
	// fallback ke https:// + r.Host tanpa mempercayai header X-Forwarded-*.
	PublicBaseURL string
}

func loadConfig() Config {
	cfg := Config{
		ListenAddr:    envOr("LISTEN_ADDR", "127.0.0.1:8080"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		CookieSecure:  truthy(os.Getenv("COOKIE_SECURE")),
		ShutdownTTL:   10 * time.Second,
		PublicBaseURL: strings.TrimRight(envOr("PUBLIC_BASE_URL", ""), "/"),
	}
	if cfg.DatabaseURL == "" {
		slog.Error("DATABASE_URL wajib diisi")
		os.Exit(1)
	}
	return cfg
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func truthy(v string) bool {
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg := loadConfig()
	if !cfg.CookieSecure {
		slog.Warn("COOKIE_SECURE=false: cookie sesi tanpa flag Secure; setel COOKIE_SECURE=1 untuk deployment HTTPS")
	}

	// Koneksi database: satu pool pgx untuk seluruh request.
	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		slog.Error("gagal membuat pgxpool", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.Ping(ctx); err != nil {
		slog.Error("gagal koneksi PostgreSQL", "error", err)
		os.Exit(1)
	}
	slog.Info("PostgreSQL terhubung")

	queries := db.New(pool)
	handler := handlers.New(queries, cfg.CookieSecure, cfg.PublicBaseURL)

	// Bersihkan berkas unggahan import yang sudah kedaluwarsa saat mulai.
	handlers.PruneUploads()

	srv := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      newRouter(pool, queries, handler),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Shutdown anggun: hentikan listener, tunggu request in-flight selesai.
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		slog.Info("sinyal diterima, mematikan server", "signal", sig.String())

		shutCtx, shutCancel := context.WithTimeout(context.Background(), cfg.ShutdownTTL)
		defer shutCancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			slog.Error("shutdown tidak bersih", "error", err)
		}
	}()

	slog.Info("server berjalan", "addr", cfg.ListenAddr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server berhenti", "error", err)
		os.Exit(1)
	}
	slog.Info("server berhenti dengan baik")
}

// newRouter menyusun route chi. Middleware wajib: request ID, logging,
// recovery panic, dan timeout per request. middleware.RealIP TIDAK dipakai:
// dia mempercayai X-Forwarded-For mentah sehingga rate limit login bisa
// dilewati dengan header palsu. IP untuk logging dan rate limit diambil dari
// r.RemoteAddr (alamat koneksi nyata, tidak bisa di-spoof klien).
// Area perusahaan diproteksi berlapis: RequireAuth -> RequireCompany -> RequireRole.
func newRouter(pool *pgxpool.Pool, queries *db.Queries, h *handlers.Handlers) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	// Anti-CSRF: tolak request lintas origin (Origin/Referer vs Host).
	r.Use(appmw.CSRFProtect)

	// Aset statis (Tailwind hasil compile + HTMX) dari dalam binary.
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServerFS(static.Files)))

	// Liveness + kesehatan koneksi DB.
	r.Get("/healthz", func(w http.ResponseWriter, req *http.Request) {
		ctx, cancel := context.WithTimeout(req.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			http.Error(w, "db unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Autentikasi.
	r.Get("/login", h.LoginGet)
	r.Post("/login", h.LoginPost)
	r.Post("/logout", h.LogoutPost)

	// Penerimaan undangan: publik, dijaga token di URL.
	r.Get("/invitations/{token}", h.InvitationAcceptGet)
	r.Post("/invitations/{token}", h.InvitationAcceptPost)
	// Pemeriksa ketersediaan username, hanya hidup di dalam tautan undangan.
	r.Get("/invitations/{token}/cek-username", h.UsernameCheckGet)

	// Area terautentikasi.
	r.Group(func(r chi.Router) {
		r.Use(appmw.RequireAuth(queries, h.CookieSecure))

		r.Get("/", h.HomeGet)

		// Profil pengguna: berlaku lintas perusahaan, tidak perlu cakupan
		// perusahaan karena hanya menyangkut akun sendiri.
		r.Get("/profile", h.ProfileGet)
		r.Post("/profile", h.ProfilePost)

		r.Route("/companies/{companyKey}", func(r chi.Router) {
			r.Use(appmw.RequireCompany(queries))

			r.Get("/dashboard", h.DashboardGet)
			r.Get("/ledger", h.LedgerGet)
			r.Get("/receivables", h.ReceivablesGet)
			r.Get("/payables", h.PayablesGet)
			r.Get("/adjustments", h.AdjustmentsGet)
			r.Get("/worksheet", h.WorksheetGet)
			r.Get("/exchange", h.ExchangeGet)
			r.Get("/exchange/export", h.ExchangeExport)
			r.Get("/exchange/template", h.ExchangeTemplate)
			r.Get("/reports", h.ReportsGet)
			r.Get("/reports/pl", h.ReportsPLGet)
			r.Get("/reports/bs", h.ReportsBSGet)
			r.Get("/reports/equity", h.ReportsEquityGet)
			r.Get("/reports/cashflow", h.ReportsCashFlowGet)
			r.Get("/reports/notes", h.ReportsNotesGet)
			r.Get("/reports/pl/pdf", h.ReportsPLPdfGet)
			r.Get("/reports/bs/pdf", h.ReportsBSPdfGet)

			// Jurnal: semua role boleh melihat daftar dan detail.
			r.Get("/journals", h.JournalsListGet)
			r.Get("/journals/{journalID}", h.JournalDetailGet)

			// Mengisi jurnal untuk staff ke atas.
			r.Group(func(r chi.Router) {
				r.Use(appmw.RequireRole(appmw.RoleStaff))
				r.Get("/journals/new", h.JournalNewGet)
				r.Get("/journals/line", h.JournalLineGet)
				r.Post("/journals", h.JournalCreatePost)
				r.Post("/journals/preview", h.JournalPreviewPost)
			})

			// Kelola akun (Chart of Accounts): admin ke atas.
			r.Group(func(r chi.Router) {
				r.Use(appmw.RequireRole(appmw.RoleAdmin))
				r.Get("/accounts", h.AccountsGet)
				r.Post("/accounts", h.AccountCreatePost)
				r.Post("/accounts/{accountID}", h.AccountUpdatePost)

				// Import akun (exchange) = operasi admin, sama seperti CRUD akun.
				r.Post("/exchange/import", h.ExchangeImportPost)
				r.Post("/exchange/import/confirm", h.ExchangeImportConfirm)

				// PSAK 201: periode akuntansi, saldo awal, dan tutup buku.
				r.Get("/periods", h.PeriodsGet)
				r.Post("/periods", h.PeriodCreatePost)
				r.Get("/periods/opening", h.OpeningGet)
				r.Post("/periods/opening", h.OpeningPost)
				r.Post("/periods/{periodID}/close", h.PeriodClosePost)
				r.Post("/periods/{periodID}/reopen", h.PeriodReopenPost)
			})

			// Pengaturan user & peran: khusus owner.
			r.Group(func(r chi.Router) {
				r.Use(appmw.RequireRole(appmw.RoleOwner))
				r.Get("/settings", h.SettingsGet)
				r.Post("/settings/profile", h.CompanyProfilePost)
				r.Post("/settings/users/role", h.UserRolePost)
				r.Post("/settings/users/remove", h.UserRemovePost)
				r.Get("/invitations", h.InvitationsGet)
				r.Post("/invitations", h.InvitationCreatePost)
				r.Post("/invitations/{inviteID}/cancel", h.InvitationCancelPost)
			})
		})
	})

	return r
}

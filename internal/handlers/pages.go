package handlers

import (
	"net/http"

	"accounting/internal/database/db"
	appmw "accounting/internal/middleware"
	"accounting/templates"
)

// HomeGet menampilkan daftar perusahaan yang boleh diakses user (RBAC).
func (h *Handlers) HomeGet(w http.ResponseWriter, r *http.Request) {
	user, ok := appmw.UserFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	companies, err := h.DB.ListCompaniesForUserWithStats(r.Context(), user.UserID)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}

	// Halaman pemilihan pembukuan dirender tanpa sidebar dan tanpa menu
	// perusahaan: satu-satunya pilihan di sini adalah pembukuan mana yang dibuka.
	shell := h.newShell(r, user, db.Company{}, "", "home", "Pilih Pembukuan")
	shell.Plain = true

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.HomePage(shell, companies).Render(r.Context(), w)
}

// companyContext merapikan data yang dibutuhkan halaman perusahaan:
// user login, role di perusahaan ini, dan data perusahaan.
func (h *Handlers) companyContext(w http.ResponseWriter, r *http.Request) (appmw.SessionUser, string, db.Company, bool) {
	user, ok := appmw.UserFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return appmw.SessionUser{}, "", db.Company{}, false
	}

	role, ok := appmw.RoleFromContext(r.Context())
	if !ok {
		http.Error(w, "403 akses ditolak", http.StatusForbidden)
		return appmw.SessionUser{}, "", db.Company{}, false
	}

	// Perusahaan sudah diverifikasi middleware lewat kunci URL, jadi halaman
	// tidak perlu membaca ulang dari database.
	company, ok := appmw.CompanyFromContext(r.Context())
	if !ok {
		http.Error(w, "404 perusahaan tidak ditemukan", http.StatusNotFound)
		return appmw.SessionUser{}, "", db.Company{}, false
	}

	return user, role, company, true
}

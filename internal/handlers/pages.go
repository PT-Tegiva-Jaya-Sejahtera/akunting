package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"accounting/internal/database/db"
	appmw "accounting/internal/middleware"
	"accounting/internal/models"
	"accounting/templates"
	"github.com/jackc/pgx/v5"
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
	_ = templates.HomePage(shell, companies, "").Render(r.Context(), w)
}

// CompanyCreatePost membuat pembukuan baru dari halaman pilih pembukuan.
// Pembuatnya otomatis menjadi owner perusahaan itu, jadi akun pertama hasil
// setup bisa langsung mengundang pengguna lain dan mengimpor data.
func (h *Handlers) CompanyCreatePost(w http.ResponseWriter, r *http.Request) {
	user, ok := appmw.UserFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "400 form tidak valid", http.StatusBadRequest)
		return
	}

	name := sanitizeText(r.FormValue("name"), 120)
	if name == "" {
		h.renderHomeErr(w, r, "Nama perusahaan wajib diisi.")
		return
	}

	// Slug dari nama, dibuat unik dengan angka di belakang bila bentrok.
	base := models.SlugifyName(name)
	slug := base
	for i := 2; ; i++ {
		_, err := h.DB.GetCompanyBySlug(r.Context(), slug)
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {
			http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
			return
		}
		if i > 100 {
			h.renderHomeErr(w, r, "Gagal membuat slug unik untuk perusahaan ini. Coba nama lain.")
			return
		}
		slug = base + "-" + strconv.Itoa(i)
	}

	key, err := models.NewCompanyKey()
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	company, err := h.DB.CreateCompany(r.Context(), db.CreateCompanyParams{
		UrlKey:       key,
		Name:         name,
		Slug:         slug,
		BaseCurrency: "IDR",
	})
	if err != nil {
		h.renderHomeErr(w, r, "Gagal membuat pembukuan (nama mungkin sudah dipakai).")
		return
	}
	if err := h.DB.GrantCompanyAccess(r.Context(), db.GrantCompanyAccessParams{
		CompanyID: company.ID,
		UserID:    user.UserID,
		Role:      db.CompanyRoleOwner,
	}); err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/companies/"+company.UrlKey+"/dashboard", http.StatusSeeOther)
}

// renderHomeErr menampilkan ulang halaman pilih pembukuan dengan pesan galat.
func (h *Handlers) renderHomeErr(w http.ResponseWriter, r *http.Request, errMsg string) {
	user, _ := appmw.UserFromContext(r.Context())
	companies, err := h.DB.ListCompaniesForUserWithStats(r.Context(), user.UserID)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	shell := h.newShell(r, user, db.Company{}, "", "home", "Pilih Pembukuan")
	shell.Plain = true
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	_ = templates.HomePage(shell, companies, errMsg).Render(r.Context(), w)
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

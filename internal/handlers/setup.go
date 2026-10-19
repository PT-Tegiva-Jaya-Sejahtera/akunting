// First-run setup: saat database belum punya satu pengguna pun, aplikasi
// mengarahkan setiap kunjungan /login ke /setup supaya akun pertama dibuat
// lewat halaman ini. Akun pertama nantinya membuat pembukuan, mengundang
// pengguna lain (halaman Pengaturan perusahaan), dan mengimpor data
// (Pertukaran Data). Halaman ini mati otomatis setelah ada satu pengguna.
package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"accounting/internal/auth"
	"accounting/internal/database/db"
	"accounting/templates"
)

// hasAnyUser melaporkan apakah database sudah punya pengguna aktif.
func (h *Handlers) hasAnyUser(r *http.Request) (bool, error) {
	total, err := h.DB.CountActiveUsers(r.Context())
	if err != nil {
		return false, err
	}
	return total > 0, nil
}

// SetupGet menampilkan form pembuatan akun pertama. Bila sudah ada
// pengguna, halaman ini tidak lagi berlaku dan pengunjung dialihkan ke login.
func (h *Handlers) SetupGet(w http.ResponseWriter, r *http.Request) {
	exists, err := h.hasAnyUser(r)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	if exists {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	h.renderSetup(w, r, http.StatusOK, "")
}

// SetupPost membuat akun pertama. Penjaga atomic di query (CreateFirstUser)
// menolak bila akun pertama sudah dibuat oleh request lain, jadi halaman ini
// tidak bisa dipakai menambah pengguna lewat jendela waktu.
func (h *Handlers) SetupPost(w http.ResponseWriter, r *http.Request) {
	exists, err := h.hasAnyUser(r)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	if exists {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "400 form tidak valid", http.StatusBadRequest)
		return
	}

	fullName := sanitizeText(r.FormValue("full_name"), 100)
	username := sanitizeUsername(r.FormValue("username"))
	password := r.FormValue("password")
	konfirmasi := r.FormValue("password_confirm")

	errMsg := ""
	switch {
	case fullName == "":
		errMsg = "Nama lengkap wajib diisi."
	case username == "":
		errMsg = "Username wajib diisi."
	}
	if errMsg != "" {
		h.renderSetup(w, r, http.StatusBadRequest, errMsg)
		return
	}
	aturan := auth.PeriksaPassword(password, konfirmasi)
	if len(aturan.Masalah) > 0 {
		h.renderSetup(w, r, http.StatusBadRequest, strings.Join(aturan.Masalah, " "))
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}

	user, err := h.DB.CreateFirstUser(r.Context(), db.CreateFirstUserParams{
		Username:     username,
		PasswordHash: hash,
		FullName:     fullName,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Kalah race: akun pertama sudah dibuat, cukup login.
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		h.renderSetup(w, r, http.StatusBadRequest,
			"Username sudah dipakai atau akun gagal dibuat. Coba username lain.")
		return
	}
	_ = user
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// renderSetup merender halaman setup dengan status dan pesan tertentu.
func (h *Handlers) renderSetup(w http.ResponseWriter, r *http.Request, status int, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	_ = templates.SetupPage(errMsg).Render(r.Context(), w)
}

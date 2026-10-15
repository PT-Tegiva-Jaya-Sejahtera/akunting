package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"accounting/internal/database/db"
	appmw "accounting/internal/middleware"
	"accounting/internal/models"
	"accounting/templates"
)

// SettingsGet menampilkan manajemen user & peran per perusahaan (khusus owner).
func (h *Handlers) SettingsGet(w http.ResponseWriter, r *http.Request) {
	h.renderSettings(w, r, "")
}

// CompanyProfilePost menyimpan identitas entitas untuk kepala laporan
// keuangan: nama, alamat, NPWP, catatan jenis laporan, dan satuan pembulatan
// penyajian angka (PSAK 201 par. 49-53).
func (h *Handlers) CompanyProfilePost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "400 form tidak valid", http.StatusBadRequest)
		return
	}
	_, _, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		h.renderSettings(w, r, "Nama entitas wajib diisi.")
		return
	}
	rounding := strings.TrimSpace(r.FormValue("rounding_unit"))
	switch rounding {
	case models.RoundingFull, models.RoundingThousand, models.RoundingMillion:
	default:
		h.renderSettings(w, r, "Satuan pembulatan tidak dikenal.")
		return
	}

	if err := h.DB.UpdateCompanyProfile(r.Context(), db.UpdateCompanyProfileParams{
		ID:           company.ID,
		Name:         name,
		Address:      strings.TrimSpace(r.FormValue("address")),
		Npwp:         strings.TrimSpace(r.FormValue("npwp")),
		RoundingUnit: rounding,
		EntityNote:   strings.TrimSpace(r.FormValue("entity_note")),
	}); err != nil {
		h.renderSettings(w, r, "Gagal menyimpan identitas laporan keuangan.")
		return
	}
	http.Redirect(w, r, settingsPath(company.UrlKey), http.StatusSeeOther)
}

// UserRolePost mengubah peran anggota perusahaan.
func (h *Handlers) UserRolePost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "400 form tidak valid", http.StatusBadRequest)
		return
	}
	_, _, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}

	targetID, err := strconv.ParseInt(r.FormValue("user_id"), 10, 64)
	if err != nil {
		http.Error(w, "400 user tidak valid", http.StatusBadRequest)
		return
	}
	role, err := parseRoleForm(r.FormValue("role"))
	if err != nil {
		h.renderSettings(w, r, "Peran tidak dikenal.")
		return
	}

	// Penjaga: perusahaan harus selalu punya minimal satu owner.
	if err := h.guardLastOwner(r, company.ID, targetID); err != nil {
		h.renderSettings(w, r, err.Error())
		return
	}

	if err := h.DB.UpdateCompanyUserRole(r.Context(), db.UpdateCompanyUserRoleParams{
		CompanyID: company.ID,
		UserID:    targetID,
		Role:      role,
	}); err != nil {
		h.renderSettings(w, r, "Gagal mengubah peran.")
		return
	}
	http.Redirect(w, r, settingsPath(company.UrlKey), http.StatusSeeOther)
}

// UserRemovePost mencabut akses user dari perusahaan.
func (h *Handlers) UserRemovePost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "400 form tidak valid", http.StatusBadRequest)
		return
	}
	_, _, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}

	targetID, err := strconv.ParseInt(r.FormValue("user_id"), 10, 64)
	if err != nil {
		http.Error(w, "400 user tidak valid", http.StatusBadRequest)
		return
	}
	if err := h.guardLastOwner(r, company.ID, targetID); err != nil {
		h.renderSettings(w, r, err.Error())
		return
	}

	if err := h.DB.DeleteCompanyUser(r.Context(), db.DeleteCompanyUserParams{
		CompanyID: company.ID,
		UserID:    targetID,
	}); err != nil {
		h.renderSettings(w, r, "Gagal mencabut akses.")
		return
	}
	http.Redirect(w, r, settingsPath(company.UrlKey), http.StatusSeeOther)
}

// guardLastOwner menolak perubahan yang meninggalkan perusahaan tanpa owner.
func (h *Handlers) guardLastOwner(r *http.Request, companyID, targetID int64) error {
	target, err := h.DB.GetUserRoleInCompany(r.Context(), db.GetUserRoleInCompanyParams{
		UserID:    targetID,
		CompanyID: companyID,
	})
	if err != nil {
		return errInvalidTarget
	}
	if target != appmw.RoleOwner {
		return nil
	}
	count, err := h.DB.CountCompanyOwners(r.Context(), companyID)
	if err != nil {
		return errInvalidTarget
	}
	if count <= 1 {
		return errLastOwner
	}
	return nil
}

var (
	errLastOwner     = errString("Perusahaan harus memiliki minimal satu owner, perubahan dibatalkan.")
	errInvalidTarget = errString("Pengguna tidak ditemukan di perusahaan ini.")
)

type errString string

func (e errString) Error() string { return string(e) }

func parseRoleForm(s string) (db.CompanyRole, error) {
	switch s {
	case appmw.RoleOwner:
		return db.CompanyRoleOwner, nil
	case appmw.RoleAdmin:
		return db.CompanyRoleAdmin, nil
	case appmw.RoleStaff:
		return db.CompanyRoleStaff, nil
	}
	return "", errString("Peran tidak dikenal.")
}

// sanitizeUsername menyisakan huruf, angka, titik, garis bawah, dan tanda
// hubung saja, huruf kecil semua, maksimal 40 karakter.
func sanitizeUsername(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		}
	}
	if len(b.String()) > 40 {
		return b.String()[:40]
	}
	return b.String()
}

func (h *Handlers) renderSettings(w http.ResponseWriter, r *http.Request, errMsg string) {
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	members, err := h.DB.ListCompanyUsers(r.Context(), company.ID)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if errMsg != "" {
		w.WriteHeader(http.StatusBadRequest)
	}
	_ = templates.SettingsPage(h.newShell(r, user, company, role, "settings", "Pengguna & Peran"), company, members, errMsg).Render(r.Context(), w)
}

func settingsPath(companyKey string) string {
	return "/companies/" + companyKey + "/settings"
}

package handlers

import (
	"net/http"

	"accounting/internal/database/db"
	appmw "accounting/internal/middleware"
	"accounting/templates"
)

// newShell menyusun kerangka halaman: judul, pengguna aktif, perusahaan aktif,
// dan daftar perusahaan untuk picker di sidebar. Dipakai semua handler supaya
// menambah isi sidebar tidak menyentuh setiap halaman.
func (h *Handlers) newShell(r *http.Request, user appmw.SessionUser, company db.Company, role, active, title string) templates.Shell {
	shell := templates.Shell{
		Title:       title,
		UserName:    user.FullName,
		CompanyID:   company.ID,
		CompanyKey:  company.UrlKey,
		CompanyName: company.Name,
		Role:        role,
		Active:      active,
	}
	rows, err := h.DB.ListCompaniesForUser(r.Context(), user.UserID)
	if err != nil {
		return shell
	}
	for _, c := range rows {
		shell.Companies = append(shell.Companies, templates.CompanyOption{
			ID:      c.ID,
			Key:     c.UrlKey,
			Name:    c.Name,
			Role:    c.Role,
			Current: c.ID == company.ID,
		})
	}
	return shell
}

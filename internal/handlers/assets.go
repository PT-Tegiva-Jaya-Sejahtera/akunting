// Register aset tetap (PSAK 216, Stage 4a): daftar aset, kalkulasi depresiasi
// akumulasi, dan nilai buku per tanggal laporan. Jurnal depresiasi dibuat
// manual oleh user (penyesuaian); halaman ini juga menampilkan pembanding
// saldo akun aset tetap + akumulasi penyusutan di buku besar sebagai
// kalkulasi rekomendasi untuk Neraca.
package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"accounting/internal/database/db"
	appmw "accounting/internal/middleware"
	"accounting/internal/models"
	"accounting/templates"
)

// AssetsGet menampilkan register aset tetap. Parameter "to" (dd/mm/yyyy)
// memilih tanggal laporan; bila kosong dipakai hari ini.
func (h *Handlers) AssetsGet(w http.ResponseWriter, r *http.Request) {
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}

	now := time.Now()
	asOf := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if s := r.URL.Query().Get("to"); s != "" {
		t, err := models.ParseDateID(s)
		if err != nil {
			http.Error(w, "400 tanggal laporan harus berformat dd/mm/yyyy", http.StatusBadRequest)
			return
		}
		asOf = t
	}

	h.renderAssets(w, r, user, role, company, asOf, "")
}

// renderAssets menyusun halaman register aset: baris per aset, baris jumlah,
// dan pembanding saldo akun COA (buku besar) dengan rekomendasi modul.
func (h *Handlers) renderAssets(w http.ResponseWriter, r *http.Request,
	user appmw.SessionUser, role string, company db.Company,
	asOf time.Time, errMsg string) {

	dbAssets, err := h.DB.ListFixedAssets(r.Context(), company.ID)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	fixed := make([]models.FixedAsset, 0, len(dbAssets))
	for _, a := range dbAssets {
		fixed = append(fixed, models.FixedAsset{
			ID:              a.ID,
			CompanyID:       a.CompanyID,
			Name:            a.Name,
			Code:            a.Code,
			CostCents:       models.NumericToCents(a.Cost),
			AcquisitionDate: a.AcquisitionDate.Time,
			UsefulLifeYears: int(a.UsefulLifeYears),
			ResidualCents:   models.NumericToCents(a.ResidualValue),
			Method:          string(a.Method),
			IsActive:        a.IsActive,
		})
	}
	rows, total := models.BuildAssetRows(fixed, asOf)

	// Pembanding menurut buku besar: saldo akun psak_line aset_tetap dan
	// aset_tetap_akumulasi sampai tanggal laporan. Jurnal pembelian dan
	// depresiasi dibuat manual oleh user; angka di sebelahnya adalah
	// rekomendasi modul.
	totals, err := h.DB.ListAccountTotalsByPeriod(r.Context(), db.ListAccountTotalsByPeriodParams{
		CompanyID: company.ID,
		DateFrom:  pgtype.Date{Time: time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true, InfinityModifier: pgtype.Finite},
		DateTo:    pgtype.Date{Time: asOf, Valid: true, InfinityModifier: pgtype.Finite},
	})
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	var buku models.AssetCOABuku
	for _, t := range totals {
		switch t.PsakLine {
		case models.PsakAsetTetap:
			buku.CostCents += models.NumericToCents(t.TotalDebit) - models.NumericToCents(t.TotalCredit)
		case models.PsakAsetTetapAkum:
			buku.AccumCents += models.NumericToCents(t.TotalCredit) - models.NumericToCents(t.TotalDebit)
		}
	}
	buku.BookCents = buku.CostCents - buku.AccumCents

	view := models.AssetsView{
		AsOfISO:       asOf.Format("2006-01-02"),
		AsOfLabel:     models.FormatDateID(asOf),
		Rows:          rows,
		Total:         total,
		Buku:          buku,
		RoundingUnit:  company.RoundingUnit,
		RoundingLabel: models.RoundingLabel(company.RoundingUnit),
		CanManage:     role == appmw.RoleAdmin || role == appmw.RoleOwner,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.AssetsPage(h.newShell(r, user, company, role, "assets",
		"Aset Tetap "+company.Name), company, view, errMsg).Render(r.Context(), w)
}

// AssetsCreatePost mendaftarkan aset tetap baru ke perusahaan.
func (h *Handlers) AssetsCreatePost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "400 form tidak valid", http.StatusBadRequest)
		return
	}
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	code := strings.TrimSpace(r.FormValue("code"))
	method := r.FormValue("method")
	errMsg := ""

	switch {
	case name == "":
		errMsg = "Nama aset wajib diisi."
	case method != models.MethodStraightLine && method != models.MethodDeclining:
		errMsg = "Metode depresiasi tidak dikenal."
	}

	cost, errC := models.ParseMoney(r.FormValue("cost"))
	if errMsg == "" && (errC != nil || cost <= 0) {
		errMsg = "Harga perolehan harus lebih besar dari nol."
	}
	residual, errR := models.ParseMoney(r.FormValue("residual_value"))
	if errMsg == "" && (errR != nil || residual < 0 || residual > cost) {
		errMsg = "Nilai residu tidak boleh negatif atau melebihi harga perolehan."
	}
	life, errL := strconv.Atoi(strings.TrimSpace(r.FormValue("useful_life_years")))
	if errMsg == "" && (errL != nil || life <= 0) {
		errMsg = "Masa manfaat harus lebih besar dari nol (tahun)."
	}
	acq, errD := models.ParseDateID(r.FormValue("acquisition_date"))
	if errMsg == "" && errD != nil {
		errMsg = "Tanggal perolehan harus berformat dd/mm/yyyy."
	}

	if errMsg != "" {
		now := time.Now()
		h.renderAssets(w, r, user, role, company,
			time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC), errMsg)
		return
	}

	_, err := h.DB.CreateFixedAsset(r.Context(), db.CreateFixedAssetParams{
		CompanyID:       company.ID,
		Name:            name,
		Code:            code,
		Cost:            mustNumeric(cost),
		AcquisitionDate: pgtype.Date{Time: acq, Valid: true, InfinityModifier: pgtype.Finite},
		UsefulLifeYears: int32(life),
		ResidualValue:   mustNumeric(residual),
		Method:          db.FixedAssetMethod(method),
		IsActive:        true,
	})
	if err != nil {
		now := time.Now()
		h.renderAssets(w, r, user, role, company,
			time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC),
			"Gagal menyimpan aset (kode mungkin sudah dipakai).")
		return
	}
	http.Redirect(w, r, assetsPath(company.UrlKey), http.StatusSeeOther)
}

// AssetsTogglePost mengaktifkan atau menonaktifkan satu aset. Aset nonaktif
// tetap tampil di register sebagai arsip; kalkulasi depresiasinya tetap
// dihitung sampai tanggal laporan (penghentian penyusutan karena pelepasan
// aset dicatat manual oleh user, di luar scope MVP).
func (h *Handlers) AssetsTogglePost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "400 form tidak valid", http.StatusBadRequest)
		return
	}
	_, _, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	assetID, err := strconv.ParseInt(chi.URLParam(r, "assetID"), 10, 64)
	if err != nil {
		http.Error(w, "400 aset tidak valid", http.StatusBadRequest)
		return
	}
	isActive := r.FormValue("is_active") == "1"
	if _, err := h.DB.SetFixedAssetActive(r.Context(), db.SetFixedAssetActiveParams{
		ID: assetID, CompanyID: company.ID, IsActive: isActive,
	}); err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, assetsPath(company.UrlKey), http.StatusSeeOther)
}

// mustNumeric membungkus nilai sen tanpa error (bentuk uang sudah valid
// melewati ParseMoney).
func mustNumeric(cents int64) pgtype.Numeric {
	n, err := models.CentsToNumeric(cents)
	if err != nil {
		return pgtype.Numeric{}
	}
	return n
}

func assetsPath(companyKey string) string {
	return "/companies/" + companyKey + "/assets"
}

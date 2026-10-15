// Tiga laporan baru yang diwajibkan PSAK 201: Laporan Perubahan Ekuitas (par.
// 106-110), Laporan Arus Kas (PSAK 207), dan Catatan atas Laporan Keuangan
// (par. 112-138). Angka disusun dari mutasi ledger lewat model.AccountTotal
// supaya bisa diuji tanpa HTTP.
package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"accounting/internal/database/db"
	"accounting/internal/models"
	"accounting/templates"
)

// ReportsEquityGet menampilkan Laporan Perubahan Ekuitas: rekonsiliasi saldo
// awal ke saldo akhir untuk setiap komponen ekuitas (PSAK 201 par. 106-110).
func (h *Handlers) ReportsEquityGet(w http.ResponseWriter, r *http.Request) {
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	win, ok := h.reportWindow(w, r, company, "equity")
	if !ok {
		return
	}
	view := models.EquityView{ReportView: win.view}
	cur, ok := h.equityStatement(r.Context(), company.ID, win.from, win.to)
	if !ok {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	view.Cur = cur
	if win.cmp.ok {
		pri, ok := h.equityStatement(r.Context(), company.ID, win.cmp.from, win.cmp.to)
		if !ok {
			http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
			return
		}
		view.Pri = &pri
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.EquityPage(h.newShell(r, user, company, role, "equity",
		"Laporan Perubahan Ekuitas "+company.Name), company, view).Render(r.Context(), w)
}

// equityStatement menyusun satu matriks perubahan ekuitas untuk rentang waktu.
func (h *Handlers) equityStatement(ctx context.Context, companyID int64, from, to time.Time) (models.EquityStatement, bool) {
	opening, err := h.accountTotals(ctx, companyID, totalsOpening, from, to)
	if err != nil {
		return models.EquityStatement{}, false
	}
	movements, err := h.accountTotals(ctx, companyID, totalsMovements, from, to)
	if err != nil {
		return models.EquityStatement{}, false
	}
	closing, err := h.accountTotals(ctx, companyID, totalsClosing, from, to)
	if err != nil {
		return models.EquityStatement{}, false
	}
	return models.BuildEquity(opening, movements, closing), true
}

// ReportsCashFlowGet menampilkan Laporan Arus Kas metode tidak langsung
// (PSAK 207 par. 18 huruf b) dengan kolom pembanding.
func (h *Handlers) ReportsCashFlowGet(w http.ResponseWriter, r *http.Request) {
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	win, ok := h.reportWindow(w, r, company, "cashflow")
	if !ok {
		return
	}
	cur, ok := h.cashFlowStatement(r.Context(), company.ID, win.from, win.to)
	if !ok {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	view := models.CashFlowView{ReportView: win.view, Cur: cur}
	if win.cmp.ok {
		pri, ok := h.cashFlowStatement(r.Context(), company.ID, win.cmp.from, win.cmp.to)
		if !ok {
			http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
			return
		}
		view.Pri = &pri
	}
	view.MergeLines()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.CashFlowPage(h.newShell(r, user, company, role, "cashflow",
		"Laporan Arus Kas "+company.Name), company, view).Render(r.Context(), w)
}

// cashFlowStatement menyusun laporan arus kas satu rentang waktu.
func (h *Handlers) cashFlowStatement(ctx context.Context, companyID int64, from, to time.Time) (models.CashFlow, bool) {
	opening, err := h.accountTotals(ctx, companyID, totalsOpening, from, to)
	if err != nil {
		return models.CashFlow{}, false
	}
	movements, err := h.accountTotals(ctx, companyID, totalsMovements, from, to)
	if err != nil {
		return models.CashFlow{}, false
	}
	return models.BuildCashFlow(opening, movements), true
}

// ReportsNotesGet menampilkan Catatan atas Laporan Keuangan (PSAK 201 par.
// 112-138): pernyataan kepatuhan, dasar penyusunan, kebijakan akuntansi
// signifikan, sumber ketidakpastian estimasi, dan rincian per akun material
// dengan referensi silang ke kebijakan (par. 113).
func (h *Handlers) ReportsNotesGet(w http.ResponseWriter, r *http.Request) {
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	win, ok := h.reportWindow(w, r, company, "notes")
	if !ok {
		return
	}
	ctx := r.Context()
	inception := time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)

	balance, err := h.accountTotalsRange(ctx, company.ID, inception, win.to)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	movements, err := h.accountTotals(ctx, company.ID, totalsMovements, win.from, win.to)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	var priorBalance, priorMovements []models.AccountTotal
	if win.cmp.ok {
		priorBalance, err = h.accountTotalsRange(ctx, company.ID, inception, win.cmp.to)
		if err != nil {
			http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
			return
		}
		priorMovements, err = h.accountTotals(ctx, company.ID, totalsMovements, win.cmp.from, win.cmp.to)
		if err != nil {
			http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
			return
		}
	}
	equity, ok := h.equityStatement(ctx, company.ID, win.from, win.to)
	if !ok {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}

	view := models.NotesView{ReportView: win.view}
	view.Build(balance, priorBalance, movements, priorMovements, equity)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.NotesPage(h.newShell(r, user, company, role, "notes",
		"Catatan atas Laporan Keuangan "+company.Name), company, view).Render(r.Context(), w)
}

// ---------------------------------------------------------------------------
// Akses data: ringkasan saldo/mutasi akun per jenis sumber
// ---------------------------------------------------------------------------

// Jenis ringkasan akun yang diambil dari database.
const (
	totalsMovements = "movements" // mutasi tanpa jurnal saldo awal/penutup
	totalsClosing   = "closing"   // mutasi jurnal penutup saja
	totalsOpening   = "opening"   // saldo awal sebelum rentang
)

// accountTotals mengambil ringkasan akun satu rentang tanggal menurut jenisnya.
func (h *Handlers) accountTotals(ctx context.Context, companyID int64, kind string, from, to time.Time) ([]models.AccountTotal, error) {
	params := db.ListAccountTotalsMovementsParams{
		CompanyID: companyID,
		DateFrom:  dateOnly(from),
		DateTo:    dateOnly(to),
	}
	switch kind {
	case totalsMovements:
		rows, err := h.DB.ListAccountTotalsMovements(ctx, params)
		if err != nil {
			return nil, err
		}
		out := make([]models.AccountTotal, 0, len(rows))
		for _, r := range rows {
			out = append(out, accountTotal(r.ID, r.Code, r.Name, string(r.AccountType),
				r.SubType, r.PsakLine, r.TotalDebit, r.TotalCredit))
		}
		return out, nil
	case totalsClosing:
		rows, err := h.DB.ListAccountTotalsClosing(ctx, db.ListAccountTotalsClosingParams(params))
		if err != nil {
			return nil, err
		}
		out := make([]models.AccountTotal, 0, len(rows))
		for _, r := range rows {
			out = append(out, accountTotal(r.ID, r.Code, r.Name, string(r.AccountType),
				r.SubType, r.PsakLine, r.TotalDebit, r.TotalCredit))
		}
		return out, nil
	default: // totalsOpening
		rows, err := h.DB.ListAccountTotalsOpening(ctx, db.ListAccountTotalsOpeningParams(params))
		if err != nil {
			return nil, err
		}
		out := make([]models.AccountTotal, 0, len(rows))
		for _, r := range rows {
			out = append(out, accountTotal(r.ID, r.Code, r.Name, string(r.AccountType),
				r.SubType, r.PsakLine, r.TotalDebit, r.TotalCredit))
		}
		return out, nil
	}
}

// accountTotalsRange mengambil saldo akun (semua jenis jurnal) satu rentang.
func (h *Handlers) accountTotalsRange(ctx context.Context, companyID int64, from, to time.Time) ([]models.AccountTotal, error) {
	rows, err := h.periodRows(ctx, companyID, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]models.AccountTotal, 0, len(rows))
	for _, r := range rows {
		out = append(out, accountTotal(r.ID, r.Code, r.Name, string(r.AccountType),
			r.SubType, r.PsakLine, r.TotalDebit, r.TotalCredit))
	}
	return out, nil
}

// accountTotal mengubah satu baris ringkasan akun menjadi models.AccountTotal.
func accountTotal(id int64, code, name, accountType, subType, psakLine string, debit, credit pgtype.Numeric) models.AccountTotal {
	return models.AccountTotal{
		ID:          id,
		Code:        code,
		Name:        name,
		AccountType: accountType,
		SubType:     subType,
		PsakLine:    psakLine,
		DebitCents:  models.NumericToCents(debit),
		CreditCents: models.NumericToCents(credit),
	}
}

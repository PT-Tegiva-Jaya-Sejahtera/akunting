package handlers

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"accounting/internal/database/db"
	"accounting/internal/models"
	"accounting/templates"
)

// ReportsGet menampilkan laporan keuangan lengkap (laba rugi dan posisi
// keuangan) menurut PSAK 201.
func (h *Handlers) ReportsGet(w http.ResponseWriter, r *http.Request) {
	h.renderReport(w, r, "full", "statements", "Laporan Keuangan")
}

// ReportsPLGet menampilkan hanya Laporan Laba Rugi.
func (h *Handlers) ReportsPLGet(w http.ResponseWriter, r *http.Request) {
	h.renderReport(w, r, "pl", "pl", "Laporan Laba Rugi")
}

// ReportsBSGet menampilkan hanya Neraca (laporan posisi keuangan).
func (h *Handlers) ReportsBSGet(w http.ResponseWriter, r *http.Request) {
	h.renderReport(w, r, "bs", "bs", "Neraca")
}

func (h *Handlers) renderReport(w http.ResponseWriter, r *http.Request, mode, active, title string) {
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	view, ok := h.reportView(w, r, company, mode)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.ReportsPage(h.newShell(r, user, company, role, active, title+" "+company.Name),
		company, view).Render(r.Context(), w)
}

// reportView menyusun angka laporan untuk mode tertentu: "pl" hanya laba rugi,
// "bs" hanya posisi keuangan, mode lain keduanya.
func (h *Handlers) reportView(w http.ResponseWriter, r *http.Request, company db.Company, mode string) (models.ReportView, bool) {
	periods, err := h.DB.ListFiscalPeriods(r.Context(), company.ID)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return models.ReportView{}, false
	}

	selected := selectPeriod(periods, r.URL.Query().Get("period"))
	today := time.Now()

	dateFrom := r.URL.Query().Get("from")
	dateTo := r.URL.Query().Get("to")
	custom := dateFrom != "" && dateTo != ""

	view := models.ReportView{
		CompanyName:   company.Name,
		CompanyAddr:   company.Address,
		CompanyNpwp:   company.Npwp,
		EntityNote:    company.EntityNote,
		RoundingUnit:  company.RoundingUnit,
		RoundingLabel: models.RoundingLabel(company.RoundingUnit),
		BaseCurrency:  company.BaseCurrency,
		Mode:          mode,
	}

	var fromT, toT time.Time
	switch {
	case custom:
		var err1, err2 error
		fromT, err1 = models.ParseDateID(dateFrom)
		toT, err2 = models.ParseDateID(dateTo)
		if err1 != nil || err2 != nil || fromT.After(toT) {
			http.Error(w, "400 rentang tanggal harus berformat dd/mm/yyyy dan urut", http.StatusBadRequest)
			return models.ReportView{}, false
		}
		view.PeriodName = "Rentang " + dateFrom + " s.d. " + dateTo
	case selected != nil:
		fromT = selected.StartDate.Time
		toT = selected.EndDate.Time
		if selected.Status == db.PeriodStatusOpen && today.Before(toT) {
			// Periode belum selesai: laporan disajikan sampai hari ini.
			toT = today
		}
		view.PeriodName = selected.Name
		view.PeriodStatus = string(selected.Status)
		if selected.Status == db.PeriodStatusClosed {
			view.PeriodStatusLabel = "Ditutup"
		} else {
			view.PeriodStatusLabel = "Terbuka"
		}
	default:
		// Belum ada periode: perilaku lama, bulan berjalan sampai hari ini.
		toT = today
		fromT = time.Date(toT.Year(), toT.Month(), 1, 0, 0, 0, 0, time.UTC)
		view.PeriodName = models.FormatDateID(fromT) + " s.d. " + models.FormatDateID(toT)
	}

	view.Periods = periodViews(periods)
	if selected != nil {
		view.SelectedPeriodID = selected.ID
	}
	view.DateFrom = fromT.Format("2006-01-02")
	view.DateTo = toT.Format("2006-01-02")
	view.DateFromLabel = models.FormatDateID(fromT)
	view.DateToLabel = models.FormatDateID(toT)
	view.CustomRange = custom

	if mode != "bs" {
		periodRows, err := h.periodRows(r.Context(), company.ID, fromT, toT)
		if err != nil {
			http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
			return models.ReportView{}, false
		}
		h.fillProfitLoss(&view, periodRows)
	}
	if mode != "pl" {
		// Neraca selalu disusun sejak awal catatan agar ekuitas dan aset seimbang.
		inception := time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)
		allRows, err := h.periodRows(r.Context(), company.ID, inception, toT)
		if err != nil {
			http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
			return models.ReportView{}, false
		}
		h.fillBalanceSheet(&view, allRows)
	}
	return view, true
}

// selectPeriod memilih periode: dari parameter, lalu periode yang memuat hari
// ini, lalu periode terbaru. Daftar periode terurut dari yang paling baru.
func selectPeriod(periods []db.FiscalPeriod, param string) *db.FiscalPeriod {
	if param != "" {
		if id, err := strconv.ParseInt(param, 10, 64); err == nil {
			for i := range periods {
				if periods[i].ID == id {
					return &periods[i]
				}
			}
		}
	}
	today := time.Now()
	for i := range periods {
		p := periods[i]
		if !today.Before(p.StartDate.Time) && !today.After(p.EndDate.Time) {
			return &periods[i]
		}
	}
	if len(periods) > 0 {
		return &periods[0]
	}
	return nil
}

func periodViews(periods []db.FiscalPeriod) []models.PeriodView {
	views := make([]models.PeriodView, 0, len(periods))
	for _, p := range periods {
		views = append(views, periodView(p))
	}
	return views
}

func (h *Handlers) periodRows(ctx context.Context, companyID int64, from, to time.Time) ([]db.ListAccountTotalsByPeriodRow, error) {
	return h.DB.ListAccountTotalsByPeriod(ctx, db.ListAccountTotalsByPeriodParams{
		CompanyID: companyID,
		DateFrom:  pgtype.Date{Time: from, Valid: true, InfinityModifier: pgtype.Finite},
		DateTo:    pgtype.Date{Time: to, Valid: true, InfinityModifier: pgtype.Finite},
	})
}

// fillProfitLoss mengelompokkan akun pendapatan dan beban periode berjalan.
func (h *Handlers) fillProfitLoss(view *models.ReportView, rows []db.ListAccountTotalsByPeriodRow) {
	for _, row := range rows {
		debit := models.NumericToCents(row.TotalDebit)
		credit := models.NumericToCents(row.TotalCredit)
		switch row.AccountType {
		case db.AccountTypeRevenue:
			amount := credit - debit
			if amount != 0 {
				view.Revenue = append(view.Revenue, models.ReportLine{Code: row.Code, Name: row.Name, AmountCents: amount})
				view.TotalRevenue += amount
			}
		case db.AccountTypeExpense:
			amount := debit - credit
			if amount != 0 {
				view.Expense = append(view.Expense, models.ReportLine{Code: row.Code, Name: row.Name, AmountCents: amount})
				view.TotalExpense += amount
			}
		}
	}
	view.NetIncome = view.TotalRevenue - view.TotalExpense
}

// fillBalanceSheet mengelompokkan akun menurut sub-jenis PSAK 201. Ekuitas
// berisi saldo akun ekuitas (termasuk Saldo Laba hasil tutup buku) ditambah
// laba rugi periode yang belum ditutup, supaya aset = liabilitas + ekuitas
// selalu berlaku tanpa memalsukan angka.
func (h *Handlers) fillBalanceSheet(view *models.ReportView, rows []db.ListAccountTotalsByPeriodRow) {
	var unclosed int64
	for _, row := range rows {
		debit := models.NumericToCents(row.TotalDebit)
		credit := models.NumericToCents(row.TotalCredit)
		line := models.ReportLine{Code: row.Code, Name: row.Name}
		switch row.SubType {
		case models.SubCurrentAsset:
			line.AmountCents = debit - credit
			if line.AmountCents != 0 {
				view.CurrentAssets = append(view.CurrentAssets, line)
				view.TotalCurrentAssets += line.AmountCents
			}
		case models.SubNonCurrentAsset:
			line.AmountCents = debit - credit
			if line.AmountCents != 0 {
				view.NonCurrentAssets = append(view.NonCurrentAssets, line)
				view.TotalNonCurrentAssets += line.AmountCents
			}
		case models.SubCurrentLiability:
			line.AmountCents = credit - debit
			if line.AmountCents != 0 {
				view.CurrentLiabilities = append(view.CurrentLiabilities, line)
				view.TotalCurrentLiabilities += line.AmountCents
			}
		case models.SubLongLiability:
			line.AmountCents = credit - debit
			if line.AmountCents != 0 {
				view.NonCurrentLiabilities = append(view.NonCurrentLiabilities, line)
				view.TotalNonCurrentLiabilities += line.AmountCents
			}
		case models.SubEquity:
			line.AmountCents = credit - debit
			if line.AmountCents != 0 {
				view.Equity = append(view.Equity, line)
				view.TotalEquity += line.AmountCents
			}
		case models.SubRevenue:
			unclosed += credit - debit
		case models.SubExpense:
			unclosed -= debit - credit
		}
	}
	view.TotalAssets = view.TotalCurrentAssets + view.TotalNonCurrentAssets
	view.TotalLiabilities = view.TotalCurrentLiabilities + view.TotalNonCurrentLiabilities
	// Saldo akun pendapatan/beban yang belum dipindahkan ke saldo laba.
	view.UnclosedProfit = unclosed
	view.TotalLiabEquity = view.TotalLiabilities + view.TotalEquity + unclosed
}

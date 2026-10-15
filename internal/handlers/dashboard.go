package handlers

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"accounting/internal/database/db"
	"accounting/internal/models"
	"accounting/templates"
)

// DashboardGet menampilkan dashboard keuangan perusahaan: ringkasan posisi
// keuangan, laba periode berjalan, rasio ringkas, grafik bulanan, dan jurnal
// terbaru. Angka dihitung dari buku besar, bukan disimpan terpisah.
func (h *Handlers) DashboardGet(w http.ResponseWriter, r *http.Request) {
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	periods, err := h.DB.ListFiscalPeriods(ctx, company.ID)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	selected := selectPeriod(periods, "")
	today := time.Now()

	view := models.DashboardView{
		RoundingUnit:  company.RoundingUnit,
		RoundingLabel: models.RoundingLabel(company.RoundingUnit),
		AsOfLabel:     models.FormatDateID(today),
	}

	// Tanggal potret: akhir periode bila periode sudah lewat atau ditutup.
	asOf := today
	if selected != nil {
		view.HasPeriod = true
		view.PeriodName = selected.Name
		if selected.Status == db.PeriodStatusClosed {
			view.PeriodStatus = "Ditutup"
			if selected.EndDate.Time.Before(asOf) {
				asOf = selected.EndDate.Time
			}
		} else {
			view.PeriodStatus = "Terbuka"
		}
		view.AsOfLabel = models.FormatDateID(asOf)

		// Laba rugi periode berjalan (awal periode sampai tanggal potret).
		periodRows, err := h.periodRows(ctx, company.ID, selected.StartDate.Time, asOf)
		if err != nil {
			http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
			return
		}
		for _, row := range periodRows {
			debit := models.NumericToCents(row.TotalDebit)
			credit := models.NumericToCents(row.TotalCredit)
			switch row.AccountType {
			case db.AccountTypeRevenue:
				view.Revenue += credit - debit
			case db.AccountTypeExpense:
				view.Expense += debit - credit
			}
		}
		view.NetIncome = view.Revenue - view.Expense
	}

	// Posisi keuangan sampai tanggal potret.
	inception := time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)
	allRows, err := h.periodRows(ctx, company.ID, inception, asOf)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	var unclosed int64
	for _, row := range allRows {
		debit := models.NumericToCents(row.TotalDebit)
		credit := models.NumericToCents(row.TotalCredit)
		line := models.ReportLine{Code: row.Code, Name: row.Name}
		switch row.SubType {
		case models.SubCurrentAsset:
			line.AmountCents = debit - credit
			view.TotalAssets += line.AmountCents
			view.CurrentAssets += line.AmountCents
		case models.SubNonCurrentAsset:
			line.AmountCents = debit - credit
			view.TotalAssets += line.AmountCents
			view.NonCurrentAssets += line.AmountCents
		case models.SubCurrentLiability:
			line.AmountCents = credit - debit
			view.TotalLiabilities += line.AmountCents
			view.CurrentLiabilities += line.AmountCents
		case models.SubLongLiability:
			line.AmountCents = credit - debit
			view.TotalLiabilities += line.AmountCents
		case models.SubEquity:
			line.AmountCents = credit - debit
			view.TotalEquity += line.AmountCents
			view.Modals = append(view.Modals, line)
		case models.SubRevenue:
			unclosed += credit - debit
		case models.SubExpense:
			unclosed -= debit - credit
		}

		// Pengelompokan pos PSAK 201 untuk kartu dashboard.
		switch row.PsakLine {
		case "kas_setara_kas":
			line.AmountCents = debit - credit
			view.CashLines = append(view.CashLines, line)
			view.CashTotal += line.AmountCents
		case "piutang":
			line.AmountCents = debit - credit
			view.ReceivableLines = append(view.ReceivableLines, line)
			view.ReceivableTotal += line.AmountCents
		case "utang_usaha":
			line.AmountCents = credit - debit
			view.PayableLines = append(view.PayableLines, line)
		case "pajak":
			view.TaxPayable += credit - debit
		case "utang_bank":
			view.BankLoan += credit - debit
		}
	}
	view.UnclosedProfit = unclosed

	// Seri grafik sesuai rentang yang dipilih pengguna: per bulan (12 bulan
	// terakhir), per kuartal (8 kuartal), atau per tahun (5 tahun).
	gran := models.NormalizeRange(r.URL.Query().Get("range"))
	view.Range = gran
	view.RangeOptions = models.RangeOptions(gran)

	startSeries, keys := models.SeriesKeys(gran, asOf)
	monthly, err := h.DB.MonthlyProfitLoss(ctx, db.MonthlyProfitLossParams{
		CompanyID: company.ID,
		DateFrom:  dateValue(startSeries),
		DateTo:    dateValue(asOf),
	})
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	bucket := map[string][2]int64{}
	for _, row := range monthly {
		key := models.BucketKey(gran, row.Bulan)
		prev := bucket[key]
		bucket[key] = [2]int64{prev[0] + models.NumericToCents(row.Pendapatan),
			prev[1] + models.NumericToCents(row.Beban)}
	}
	labels := make([]string, 0, len(keys))
	revenue := make([]int64, 0, len(keys))
	expense := make([]int64, 0, len(keys))
	net := make([]int64, 0, len(keys))
	for _, key := range keys {
		v := bucket[key]
		labels = append(labels, models.SeriesLabel(gran, key))
		revenue = append(revenue, v[0])
		expense = append(expense, v[1])
		net = append(net, v[0]-v[1])
	}
	// Teks hover memakai satuan pembulatan entitas, sama dengan tabel.
	fmtValue := func(cents int64) string {
		return models.FormatRounded(cents, company.RoundingUnit) + " " + company.BaseCurrency
	}
	// Label sumbu Y diringkas (mis. "1,5 M", "500 jt") supaya muat di sisi
	// kiri grafik; angka penuh tetap muncul pada keterangan hover.
	fmtAxis := models.FormatCompactRupiah
	view.LineChart = models.BuildLineChart(labels, revenue, expense, fmtValue, fmtAxis)
	view.BarChart = models.BuildBarChart(labels, net, fmtValue, fmtAxis)
	view.SeriesTitle = models.RangeTitle(gran)
	// Keterangan skala memakai satuan pembulatan entitas, sama dengan tabel.
	if view.LineChart.HasData {
		view.LineChart.ScaleNote = "skala 0 s.d. " + models.FormatRounded(view.LineChart.MaxCents, company.RoundingUnit)
		if view.LineChart.Compressed {
			view.LineChart.ScaleNote += " · skala dipadatkan di luar rentang umum"
		}
	}
	if view.BarChart.HasData {
		view.BarChart.ScaleNote = "tertinggi " + models.FormatRounded(view.BarChart.MaxCents, company.RoundingUnit) +
			", terendah " + models.FormatRounded(view.BarChart.MinCents, company.RoundingUnit) +
			" · " + models.RoundingLabel(company.RoundingUnit)
		if view.BarChart.Compressed {
			view.BarChart.ScaleNote += " · skala dipadatkan di luar rentang umum"
		}
	}

	// Jurnal terbaru beserta nilainya.
	journals, err := h.DB.ListRecentJournals(ctx, db.ListRecentJournalsParams{
		CompanyID:  company.ID,
		LimitCount: 8,
	})
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	for _, j := range journals {
		entries, err := h.DB.GetJournalWithEntries(ctx, db.GetJournalWithEntriesParams{
			JournalID: j.ID,
			CompanyID: company.ID,
		})
		if err != nil {
			http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
			return
		}
		var total int64
		for _, e := range entries {
			total += models.NumericToCents(e.Debit)
		}
		view.Recent = append(view.Recent, models.RecentJournal{
			Reference: j.Reference,
			DateLabel: j.JournalDate.Time.Format("02 Jan 2006"),
			Memo:      j.Memo,
			Amount:    total,
		})
	}

	count, err := h.DB.CountJournals(ctx, company.ID)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	view.JournalCount = count

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.DashboardPage(h.newShell(r, user, company, role, "dashboard", "Dashboard "+company.Name),
		company, view).Render(ctx, w)
}

func dateValue(t time.Time) pgtype.Date {
	return pgtype.Date{Time: t, Valid: true, InfinityModifier: pgtype.Finite}
}

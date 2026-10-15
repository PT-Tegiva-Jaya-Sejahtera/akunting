package handlers

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"accounting/internal/database/db"
	"accounting/internal/models"
	"accounting/templates"
	"github.com/jackc/pgx/v5/pgtype"
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
// "bs" hanya posisi keuangan, mode lain keduanya. Setiap laporan memuat kolom
// periode berjalan dan kolom periode pembanding (PSAK 201 par. 38).
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

	// Periode pembanding: parameter compare, lalu periode sebelumnya,
	// lalu rentang sama panjang, lalu bulan sebelumnya.
	cmp := resolveComparative(periods, selected, custom, fromT, toT, r.URL.Query().Get("compare"), today)
	view.HasComparative = cmp.ok
	if cmp.ok {
		view.PriorPeriodName = cmp.name
	}
	if id, err := strconv.ParseInt(r.URL.Query().Get("compare"), 10, 64); err == nil && cmp.ok {
		view.ComparePeriodID = id
	}

	if mode != "bs" {
		periodRows, err := h.periodRows(r.Context(), company.ID, fromT, toT)
		if err != nil {
			http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
			return models.ReportView{}, false
		}
		var priorRows []db.ListAccountTotalsByPeriodRow
		if cmp.ok {
			priorRows, err = h.periodRows(r.Context(), company.ID, cmp.from, cmp.to)
			if err != nil {
				http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
				return models.ReportView{}, false
			}
		}
		applyProfitLoss(&view, assemblePL(periodRows), assemblePL(priorRows))
	}
	if mode != "pl" {
		// Neraca selalu disusun sejak awal catatan agar ekuitas dan aset seimbang.
		inception := time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)
		allRows, err := h.periodRows(r.Context(), company.ID, inception, toT)
		if err != nil {
			http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
			return models.ReportView{}, false
		}
		var priorAllRows []db.ListAccountTotalsByPeriodRow
		if cmp.ok {
			priorAllRows, err = h.periodRows(r.Context(), company.ID, inception, cmp.to)
			if err != nil {
				http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
				return models.ReportView{}, false
			}
		}
		applyBalanceSheet(&view, assembleBS(allRows), assembleBS(priorAllRows))
	}
	return view, true
}

// compareWindow adalah jendela waktu periode pembanding beserta label kolomnya.
type compareWindow struct {
	from, to time.Time
	name     string
	ok       bool
}

// resolveComparative menentukan periode pembanding menurut PSAK 201 par. 38
// (satu periode sebelumnya). Urutan: parameter compare (id periode eksplisit),
// lalu rentang kustom sama panjang yang mendahului, lalu periode fiskal
// sebelumnya, lalu bulan sebelumnya. Tidak ada kandidat berarti kolom
// pembanding kosong, bukan error.
func resolveComparative(periods []db.FiscalPeriod, selected *db.FiscalPeriod, custom bool, fromT, toT time.Time, param string, today time.Time) compareWindow {
	if param != "" {
		if id, err := strconv.ParseInt(param, 10, 64); err == nil {
			for i := range periods {
				if periods[i].ID == id {
					p := periods[i]
					return compareWindow{from: p.StartDate.Time, to: clampPeriodEnd(p, today), name: p.Name, ok: true}
				}
			}
		}
	}
	if custom {
		length := toT.Sub(fromT)
		to2 := fromT.AddDate(0, 0, -1)
		from2 := to2.Add(-length)
		return compareWindow{
			from: from2,
			to:   to2,
			name: "Rentang " + models.FormatDateShort(from2) + " s.d. " + models.FormatDateShort(to2),
			ok:   true,
		}
	}
	if selected != nil {
		var best *db.FiscalPeriod
		for i := range periods {
			p := periods[i]
			if p.EndDate.Time.Before(selected.StartDate.Time) {
				if best == nil || p.EndDate.Time.After(best.EndDate.Time) {
					best = &periods[i]
				}
			}
		}
		if best != nil {
			return compareWindow{from: best.StartDate.Time, to: clampPeriodEnd(*best, today), name: best.Name, ok: true}
		}
		return compareWindow{}
	}
	from2 := fromT.AddDate(0, -1, 0)
	to2 := toT.AddDate(0, -1, 0)
	return compareWindow{
		from: from2,
		to:   to2,
		name: "Rentang " + models.FormatDateShort(from2) + " s.d. " + models.FormatDateShort(to2),
		ok:   true,
	}
}

// clampPeriodEnd memotong akhir periode terbuka yang belum berakhir sampai
// hari ini, sama seperti perlakuan periode berjalan.
func clampPeriodEnd(p db.FiscalPeriod, today time.Time) time.Time {
	end := p.EndDate.Time
	if p.Status == db.PeriodStatusOpen && today.Before(end) {
		return today
	}
	return end
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

// plBlock adalah hasil pengelompokkan akun laba rugi untuk satu rentang waktu.
// Biaya keuangan dan beban pajak dipisah dari beban operasional mengikuti
// PSAK 201 par. 82 huruf b dan d.
type plBlock struct {
	revenue, operating, finance, tax []models.ReportLine
	totalRevenue, totalOperating     int64
	totalFinance, totalTax           int64
}

// assemblePL mengelompokkan akun pendapatan dan beban satu rentang waktu.
// Beban dikelompokkan menurut sifatnya (par. 99-105): setiap akun beban
// mewakili satu sifat beban, kecuali akun yang dipetakan ke beban keuangan
// atau beban pajak lewat accounts.psak_line.
func assemblePL(rows []db.ListAccountTotalsByPeriodRow) plBlock {
	var b plBlock
	for _, row := range rows {
		debit := models.NumericToCents(row.TotalDebit)
		credit := models.NumericToCents(row.TotalCredit)
		switch row.AccountType {
		case db.AccountTypeRevenue:
			amount := credit - debit
			if amount != 0 {
				b.revenue = append(b.revenue, models.ReportLine{Code: row.Code, Name: row.Name, AmountCents: amount})
				b.totalRevenue += amount
			}
		case db.AccountTypeExpense:
			amount := debit - credit
			if amount == 0 {
				continue
			}
			line := models.ReportLine{Code: row.Code, Name: row.Name, AmountCents: amount}
			switch row.PsakLine {
			case models.PsakBebanKeuangan:
				b.finance = append(b.finance, line)
				b.totalFinance += amount
			case models.PsakBebanPajak:
				b.tax = append(b.tax, line)
				b.totalTax += amount
			default:
				b.operating = append(b.operating, line)
				b.totalOperating += amount
			}
		}
	}
	return b
}

// applyProfitLoss menyalin angka periode berjalan dan pembanding ke view,
// dengan baris digabung berdasarkan kode akun supaya kedua kolom sejajar.
func applyProfitLoss(view *models.ReportView, cur, pri plBlock) {
	view.Revenue = mergeLines(cur.revenue, pri.revenue)
	view.TotalRevenue = cur.totalRevenue
	view.PriorTotalRevenue = pri.totalRevenue

	view.OperatingExpense = mergeLines(cur.operating, pri.operating)
	view.TotalOperatingExpense = cur.totalOperating
	view.PriorTotalOperatingExpense = pri.totalOperating

	view.OperatingProfit = cur.totalRevenue - cur.totalOperating
	view.PriorOperatingProfit = pri.totalRevenue - pri.totalOperating

	view.FinanceCost = mergeLines(cur.finance, pri.finance)
	view.TotalFinanceCost = cur.totalFinance
	view.PriorTotalFinanceCost = pri.totalFinance

	view.ProfitBeforeTax = view.OperatingProfit - cur.totalFinance
	view.PriorProfitBeforeTax = view.PriorOperatingProfit - pri.totalFinance

	view.TaxExpense = mergeLines(cur.tax, pri.tax)
	view.TotalTaxExpense = cur.totalTax
	view.PriorTotalTaxExpense = pri.totalTax

	view.NetIncome = view.ProfitBeforeTax - cur.totalTax
	view.PriorNetIncome = view.PriorProfitBeforeTax - pri.totalTax
}

// bsBlock adalah hasil pengelompokkan akun laporan posisi keuangan untuk satu
// tanggal neraca.
type bsBlock struct {
	currentAssets, nonCurrentAssets     []models.ReportLine
	currentLiabilities, nonLiabilities  []models.ReportLine
	equity                              []models.ReportLine
	totalCurrentAssets, totalNonCurrent int64
	totalCurrentLiab, totalNonLiab      int64
	totalEquity, unclosed               int64
}

// assembleBS mengelompokkan akun menurut sub-jenis PSAK 201. Ekuitas berisi
// saldo akun ekuitas (termasuk Saldo Laba hasil tutup buku) ditambah laba rugi
// periode yang belum ditutup, supaya aset = liabilitas + ekuitas selalu
// berlaku tanpa memalsukan angka.
func assembleBS(rows []db.ListAccountTotalsByPeriodRow) bsBlock {
	var b bsBlock
	for _, row := range rows {
		debit := models.NumericToCents(row.TotalDebit)
		credit := models.NumericToCents(row.TotalCredit)
		line := models.ReportLine{Code: row.Code, Name: row.Name}
		switch row.SubType {
		case models.SubCurrentAsset:
			line.AmountCents = debit - credit
			if line.AmountCents != 0 {
				b.currentAssets = append(b.currentAssets, line)
				b.totalCurrentAssets += line.AmountCents
			}
		case models.SubNonCurrentAsset:
			line.AmountCents = debit - credit
			if line.AmountCents != 0 {
				b.nonCurrentAssets = append(b.nonCurrentAssets, line)
				b.totalNonCurrent += line.AmountCents
			}
		case models.SubCurrentLiability:
			line.AmountCents = credit - debit
			if line.AmountCents != 0 {
				b.currentLiabilities = append(b.currentLiabilities, line)
				b.totalCurrentLiab += line.AmountCents
			}
		case models.SubLongLiability:
			line.AmountCents = credit - debit
			if line.AmountCents != 0 {
				b.nonLiabilities = append(b.nonLiabilities, line)
				b.totalNonLiab += line.AmountCents
			}
		case models.SubEquity:
			line.AmountCents = credit - debit
			if line.AmountCents != 0 {
				b.equity = append(b.equity, line)
				b.totalEquity += line.AmountCents
			}
		case models.SubRevenue:
			b.unclosed += credit - debit
		case models.SubExpense:
			b.unclosed -= debit - credit
		}
	}
	return b
}

// applyBalanceSheet menyalin angka neraca periode berjalan dan pembanding ke
// view dengan baris digabung berdasarkan kode akun.
func applyBalanceSheet(view *models.ReportView, cur, pri bsBlock) {
	view.CurrentAssets = mergeLines(cur.currentAssets, pri.currentAssets)
	view.TotalCurrentAssets = cur.totalCurrentAssets
	view.PriorTotalCurrentAssets = pri.totalCurrentAssets

	view.NonCurrentAssets = mergeLines(cur.nonCurrentAssets, pri.nonCurrentAssets)
	view.TotalNonCurrentAssets = cur.totalNonCurrent
	view.PriorTotalNonCurrentAssets = pri.totalNonCurrent

	view.TotalAssets = cur.totalCurrentAssets + cur.totalNonCurrent
	view.PriorTotalAssets = pri.totalCurrentAssets + pri.totalNonCurrent

	view.CurrentLiabilities = mergeLines(cur.currentLiabilities, pri.currentLiabilities)
	view.TotalCurrentLiabilities = cur.totalCurrentLiab
	view.PriorTotalCurrentLiabilities = pri.totalCurrentLiab

	view.NonCurrentLiabilities = mergeLines(cur.nonLiabilities, pri.nonLiabilities)
	view.TotalNonCurrentLiabilities = cur.totalNonLiab
	view.PriorTotalNonCurrentLiabilities = pri.totalNonLiab

	view.TotalLiabilities = cur.totalCurrentLiab + cur.totalNonLiab
	view.PriorTotalLiabilities = pri.totalCurrentLiab + pri.totalNonLiab

	view.Equity = mergeLines(cur.equity, pri.equity)
	view.TotalEquity = cur.totalEquity
	view.PriorTotalEquity = pri.totalEquity

	// Saldo akun pendapatan/beban yang belum dipindahkan ke saldo laba.
	view.UnclosedProfit = cur.unclosed
	view.PriorUnclosedProfit = pri.unclosed

	view.TotalLiabEquity = view.TotalLiabilities + cur.totalEquity + cur.unclosed
	view.PriorTotalLiabEquity = view.PriorTotalLiabilities + pri.totalEquity + pri.unclosed
}

// mergeLines menggabungkan baris periode berjalan dan periode pembanding
// berdasarkan kode akun. Akun yang hanya muncul di periode pembanding tetap
// tampil dengan angka periode berjalan 0, supaya kedua kolom sejajar dan tidak
// ada pos yang diam-diam hilang.
func mergeLines(cur, pri []models.ReportLine) []models.ReportLine {
	out := make([]models.ReportLine, 0, len(cur)+len(pri))
	index := make(map[string]int, len(cur))
	for _, l := range cur {
		l.PriorAmountCents = 0
		index[l.Code] = len(out)
		out = append(out, l)
	}
	for _, l := range pri {
		if i, ok := index[l.Code]; ok {
			out[i].PriorAmountCents = l.AmountCents
			continue
		}
		out = append(out, models.ReportLine{
			Code:             l.Code,
			Name:             l.Name,
			AmountCents:      0,
			PriorAmountCents: l.AmountCents,
		})
	}
	return out
}

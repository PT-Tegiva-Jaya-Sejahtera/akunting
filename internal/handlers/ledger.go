package handlers

import (
	"net/http"
	"strconv"
	"time"

	"accounting/internal/database/db"
	"accounting/internal/models"
	"accounting/templates"
)

// LedgerGet menampilkan Buku Besar: seluruh akun dengan mutasi dan saldo pada
// periode terpilih, dan bila akun dipilih (parameter account), daftar mutasi
// akun tersebut beserta saldo berjalan.
func (h *Handlers) LedgerGet(w http.ResponseWriter, r *http.Request) {
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
	selected := selectPeriod(periods, r.URL.Query().Get("period"))

	view := models.LedgerView{
		RoundingUnit:  company.RoundingUnit,
		RoundingLabel: models.RoundingLabel(company.RoundingUnit),
		Periods:       periodViews(periods),
	}

	var from, to time.Time
	switch {
	case selected != nil:
		from, to = selected.StartDate.Time, selected.EndDate.Time
		view.PeriodName = selected.Name
		view.SelectedPeriodID = selected.ID
		if selected.Status == db.PeriodStatusClosed {
			view.PeriodStatus = "Ditutup"
		} else {
			view.PeriodStatus = "Terbuka"
		}
	default:
		today := time.Now()
		from = time.Date(today.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		to = today
		view.PeriodName = "Seluruh catatan"
	}
	view.DateFromISO = from.Format("2006-01-02")
	view.DateToISO = to.Format("2006-01-02")

	rows, err := h.periodRows(ctx, company.ID, from, to)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	// Saldo awal periode: seluruh mutasi sebelum tanggal awal periode.
	openingRows, err := h.periodRows(ctx, company.ID,
		time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC), from.AddDate(0, 0, -1))
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	opening := map[int64]int64{}
	for _, row := range openingRows {
		debit := models.NumericToCents(row.TotalDebit)
		credit := models.NumericToCents(row.TotalCredit)
		saldo := debit - credit
		if row.NormalBalance == db.NormalBalanceCredit {
			saldo = credit - debit
		}
		opening[row.ID] = saldo
	}
	for _, row := range rows {
		debit := models.NumericToCents(row.TotalDebit)
		credit := models.NumericToCents(row.TotalCredit)
		arah := debit - credit
		if row.NormalBalance == db.NormalBalanceCredit {
			arah = credit - debit
		}
		line := models.LedgerRow{
			AccountID: row.ID,
			Code:      row.Code,
			Name:      row.Name,
			SubLabel:  models.SubTypeLabel(row.SubType),
			Opening:   opening[row.ID],
			Debit:     debit,
			Credit:    credit,
			Balance:   opening[row.ID] + arah,
		}
		if debit != 0 || credit != 0 || line.Opening != 0 {
			view.Rows = append(view.Rows, line)
		}
		view.TotalDebit += debit
		view.TotalCredit += credit
	}

	// Drill-down satu akun: daftar mutasi beserta saldo berjalan.
	if idParam := r.URL.Query().Get("account"); idParam != "" {
		if accountID, err := strconv.ParseInt(idParam, 10, 64); err == nil {
			for i := range view.Rows {
				if view.Rows[i].AccountID == accountID {
					acc := view.Rows[i]
					view.Account = &acc
					break
				}
			}
			if view.Account != nil {
				movements, err := h.DB.ListAccountMovements(ctx, db.ListAccountMovementsParams{
					CompanyID: company.ID,
					AccountID: accountID,
					DateFrom:  dateValue(from),
					DateTo:    dateValue(to),
				})
				if err != nil {
					http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
					return
				}
				running := view.Account.Opening
				view.Movements = append(view.Movements, models.LedgerMovement{
					DateLabel:   from.Format("02 Jan 2006"),
					Reference:   "-",
					Description: "Saldo awal periode",
					Balance:     running,
				})
				for _, m := range movements {
					debit := models.NumericToCents(m.Debit)
					credit := models.NumericToCents(m.Credit)
					if view.Account.Opening >= 0 {
						running += debit - credit
					} else {
						running += credit - debit
					}
					view.Movements = append(view.Movements, models.LedgerMovement{
						DateLabel:   m.JournalDate.Time.Format("02 Jan 2006"),
						Reference:   m.Reference,
						Memo:        m.Memo,
						Description: m.Description,
						Debit:       debit,
						Credit:      credit,
						Balance:     running,
					})
				}
			}
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.LedgerPage(h.newShell(r, user, company, role, "ledger", "Buku Besar "+company.Name),
		company, view).Render(ctx, w)
}

// AdjustmentsGet menampilkan daftar jurnal penyesuaian perusahaan.
func (h *Handlers) AdjustmentsGet(w http.ResponseWriter, r *http.Request) {
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	journals, err := h.DB.ListAdjustingJournals(ctx, db.ListAdjustingJournalsParams{
		CompanyID:  company.ID,
		LimitCount: 100,
	})
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}

	items := make([]models.AdjustmentRow, 0, len(journals))
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
		items = append(items, models.AdjustmentRow{
			ID:        j.ID,
			DateLabel: j.JournalDate.Time.Format("02 Jan 2006"),
			Reference: j.Reference,
			Memo:      j.Memo,
			Amount:    total,
		})
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	view := models.AdjustmentView{
		Rows:          items,
		RoundingUnit:  company.RoundingUnit,
		RoundingLabel: models.RoundingLabel(company.RoundingUnit),
	}
	_ = templates.AdjustmentsPage(h.newShell(r, user, company, role, "adjustments", "Jurnal Penyesuaian"),
		company, view).Render(ctx, w)
}

// WorksheetGet menampilkan Kertas Kerja (neraca lajur): neraca saldo,
// penyesuaian, saldo setelah penyesuaian, laba rugi, dan neraca.
func (h *Handlers) WorksheetGet(w http.ResponseWriter, r *http.Request) {
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
	selected := selectPeriod(periods, r.URL.Query().Get("period"))
	view := models.WorksheetView{
		Periods:       periodViews(periods),
		RoundingUnit:  company.RoundingUnit,
		RoundingLabel: models.RoundingLabel(company.RoundingUnit),
	}

	var from, to time.Time
	if selected != nil {
		from, to = selected.StartDate.Time, selected.EndDate.Time
		view.PeriodName = selected.Name
		view.SelectedPeriodID = selected.ID
		if selected.Status == db.PeriodStatusClosed {
			view.PeriodStatus = "Ditutup"
		} else {
			view.PeriodStatus = "Terbuka"
		}
	} else {
		today := time.Now()
		from = time.Date(today.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		to = today
		view.PeriodName = "Seluruh catatan"
	}
	view.DateFromISO = from.Format("2006-01-02")
	view.DateToISO = to.Format("2006-01-02")
	view.DateToLabel = models.FormatDateID(to)

	// Neraca saldo kertas kerja = saldo awal periode (setelah tutup buku
	// periode sebelumnya) ditambah mutasi periode berjalan. Jurnal penutup
	// periode sebelumnya ikut dihitung supaya akun pendapatan dan beban
	// periode lama tidak terbawa.
	saldoRows, err := h.periodRows(ctx, company.ID,
		time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC), from.AddDate(0, 0, -1))
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	periodRows, err := h.DB.ListAccountTotalsByPeriodPlain(ctx, db.ListAccountTotalsByPeriodPlainParams{
		CompanyID: company.ID,
		DateFrom:  dateValue(from),
		DateTo:    dateValue(to),
	})
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	adjRows, err := h.DB.ListAccountTotalsByPeriodAdjusting(ctx, db.ListAccountTotalsByPeriodAdjustingParams{
		CompanyID: company.ID,
		DateFrom:  dateValue(from),
		DateTo:    dateValue(to),
	})
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}

	type workRow struct {
		models.WorksheetRow
		accountType db.AccountType
	}
	perAkun := map[int64]*workRow{}
	order := []int64{}
	// tbRow menyamakan bentuk dua sumber saldo (saldo awal dan mutasi periode)
	// supaya bisa dijumlahkan pada kolom neraca saldo.
	type tbRow struct {
		id            int64
		code, name    string
		accType       db.AccountType
		debit, credit int64
	}
	awal := make([]tbRow, 0, len(saldoRows))
	for _, row := range saldoRows {
		awal = append(awal, tbRow{row.ID, row.Code, row.Name, row.AccountType,
			models.NumericToCents(row.TotalDebit), models.NumericToCents(row.TotalCredit)})
	}
	mutasi := make([]tbRow, 0, len(periodRows))
	for _, row := range periodRows {
		mutasi = append(mutasi, tbRow{row.ID, row.Code, row.Name, row.AccountType,
			models.NumericToCents(row.TotalDebit), models.NumericToCents(row.TotalCredit)})
	}
	add := func(rows []tbRow) {
		for _, row := range rows {
			wr, ok := perAkun[row.id]
			if !ok {
				wr = &workRow{WorksheetRow: models.WorksheetRow{Code: row.code, Name: row.name}, accountType: row.accType}
				perAkun[row.id] = wr
				order = append(order, row.id)
			}
			// Kolom neraca saldo = saldo awal periode + mutasi periode ini.
			wr.SaldoDebit += row.debit
			wr.SaldoCredit += row.credit
		}
	}
	add(awal)
	add(mutasi)
	for _, row := range adjRows {
		wr, ok := perAkun[row.ID]
		if !ok {
			wr = &workRow{WorksheetRow: models.WorksheetRow{Code: row.Code, Name: row.Name}, accountType: row.AccountType}
			perAkun[row.ID] = wr
			order = append(order, row.ID)
		}
		wr.AdjDebit = models.NumericToCents(row.TotalDebit)
		wr.AdjCredit = models.NumericToCents(row.TotalCredit)
	}

	for _, id := range order {
		wr := perAkun[id]
		wr.FinalDebit = wr.SaldoDebit + wr.AdjDebit
		wr.FinalCredit = wr.SaldoCredit + wr.AdjCredit
		net := wr.FinalDebit - wr.FinalCredit
		switch wr.accountType {
		case db.AccountTypeRevenue:
			wr.PLCredit = wr.FinalCredit - wr.FinalDebit
		case db.AccountTypeExpense:
			wr.PLDebit = wr.FinalDebit - wr.FinalCredit
		default:
			if net >= 0 {
				wr.BalanceDebit = net
			} else {
				wr.BalanceCredit = -net
			}
		}
		// Baris yang seluruh kolomnya nol tidak perlu ditampilkan.
		if wr.SaldoDebit == 0 && wr.SaldoCredit == 0 && wr.AdjDebit == 0 && wr.AdjCredit == 0 {
			continue
		}
		view.Rows = append(view.Rows, wr.WorksheetRow)
		view.Totals.SaldoDebit += wr.SaldoDebit
		view.Totals.SaldoCredit += wr.SaldoCredit
		view.Totals.AdjDebit += wr.AdjDebit
		view.Totals.AdjCredit += wr.AdjCredit
		view.Totals.FinalDebit += wr.FinalDebit
		view.Totals.FinalCredit += wr.FinalCredit
		view.Totals.PLDebit += wr.PLDebit
		view.Totals.PLCredit += wr.PLCredit
		view.Totals.BalanceDebit += wr.BalanceDebit
		view.Totals.BalanceCredit += wr.BalanceCredit
	}
	view.NetIncome = view.Totals.PLCredit - view.Totals.PLDebit

	// Kolom neraca harus seimbang: laba (rugi) periode berjalan masuk sebagai
	// baris penyeimbang, sama seperti pada laporan perubahan ekuitas.
	if view.NetIncome > 0 {
		view.Rows = append(view.Rows, models.WorksheetRow{
			Code: "3-3999", Name: "Laba (Rugi) Periode Berjalan", BalanceCredit: view.NetIncome,
		})
		view.Totals.BalanceCredit += view.NetIncome
	} else if view.NetIncome < 0 {
		view.Rows = append(view.Rows, models.WorksheetRow{
			Code: "3-3999", Name: "Laba (Rugi) Periode Berjalan", BalanceDebit: -view.NetIncome,
		})
		view.Totals.BalanceDebit += -view.NetIncome
	}
	view.HasData = len(view.Rows) > 0

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.WorksheetPage(h.newShell(r, user, company, role, "worksheet", "Kertas Kerja "+company.Name),
		company, view).Render(ctx, w)
}

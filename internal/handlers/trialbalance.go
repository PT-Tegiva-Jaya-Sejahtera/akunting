package handlers

import (
	"net/http"
	"time"

	"accounting/internal/database/db"
	"accounting/internal/models"
	"accounting/templates"
)

// TrialBalanceGet menampilkan Neraca Saldo (trial balance): saldo setiap akun
// pada sisi normalnya untuk periode terpilih, dengan total debit dan kredit
// yang harus sama di bagian bawah.
func (h *Handlers) TrialBalanceGet(w http.ResponseWriter, r *http.Request) {
	h.renderTrialBalance(w, r, false)
}

// PostClosingTrialBalanceGet menampilkan neraca saldo setelah penutupan:
// saldo seluruh akun pada akhir periode, sesudah jurnal penutup dialihkan ke
// Saldo Laba (akun pendapatan dan beban menjadi nol).
func (h *Handlers) PostClosingTrialBalanceGet(w http.ResponseWriter, r *http.Request) {
	h.renderTrialBalance(w, r, true)
}

// renderTrialBalance menyusun halaman neraca saldo (biasa atau setelah
// penutupan). Saldo akun = saldo awal periode (termasuk jurnal penutup
// periode sebelumnya) ditambah seluruh mutasi periode berjalan.
func (h *Handlers) renderTrialBalance(w http.ResponseWriter, r *http.Request, postClosing bool) {
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

	view := models.TrialBalanceView{
		Periods:       periodViews(periods),
		RoundingUnit:  company.RoundingUnit,
		RoundingLabel: models.RoundingLabel(company.RoundingUnit),
		IsPostClosing: postClosing,
		SelectedPeriodID: func() int64 {
			if selected != nil {
				return selected.ID
			}
			return 0
		}(),
	}

	var from, to time.Time
	if selected != nil {
		from, to = selected.StartDate.Time, selected.EndDate.Time
		view.PeriodName = selected.Name
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

	if postClosing && selected != nil && selected.Status != db.PeriodStatusClosed {
		view.Warning = "Periode " + selected.Name + " belum ditutup, jadi tabel di bawah adalah " +
			"proyeksi neraca saldo setelah penutupan akun nominal (pendapatan/beban dinolkan, " +
			"laba rugi periode berjalan masuk ke modal). Tutup buku dulu di halaman Jurnal " +
			"Penutup atau Periode untuk mengunci periode ini."
	}

	// Saldo awal periode: seluruh mutasi sebelum tanggal awal (jurnal penutup
	// periode sebelumnya ikut terhitung), lalu mutasi periode berjalan.
	openingRows, err := h.periodRows(ctx, company.ID,
		time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC), from.AddDate(0, 0, -1))
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	mutationRows, err := h.periodRows(ctx, company.ID, from, to)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}

	type saldo struct {
		code, name    string
		normal        db.NormalBalance
		accType       db.AccountType
		role          string
		debit, credit int64
	}
	perAkun := map[int64]*saldo{}
	order := []int64{}
	add := func(rows []db.ListAccountTotalsByPeriodRow) {
		for _, row := range rows {
			s, exists := perAkun[row.ID]
			if !exists {
				s = &saldo{code: row.Code, name: row.Name, normal: row.NormalBalance, accType: row.AccountType}
				perAkun[row.ID] = s
				order = append(order, row.ID)
			}
			s.debit += models.NumericToCents(row.TotalDebit)
			s.credit += models.NumericToCents(row.TotalCredit)
		}
	}
	add(openingRows)
	add(mutationRows)

	// Neraca saldo setelah penutupan: akun nominal (pendapatan, beban,
	// Ikhtisar, prive) dinolkan dan hasil laba (rugi) periode berjalan masuk
	// ke akun modal, sama seperti jurnal penutup. Angkanya identik dengan
	// kondisi nyata bila periode sudah ditutup.
	type akunMeta struct {
		name string
		tipe db.AccountType
		role string
	}
	meta := map[int64]akunMeta{}
	accounts, err := h.DB.ListAccounts(ctx, company.ID)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	for _, acc := range accounts {
		meta[acc.ID] = akunMeta{name: acc.Name, tipe: acc.AccountType, role: acc.SystemRole}
	}
	isNominal := func(id int64) bool {
		m := meta[id]
		switch m.role {
		case models.SystemRoleIkhtisar, models.SystemRolePrive:
			return true
		}
		return m.tipe == db.AccountTypeRevenue || m.tipe == db.AccountTypeExpense
	}
	if postClosing {
		// Tujuan hasil penutupan: ekuitas pertama yang bukan prive/Ikhtisar/
		// Saldo Laba (urut kode), fallback akun Saldo Laba, seperti
		// accounting.ProvisionClosingAccounts saat membuat jurnal penutup.
		var nominalNet, priveNet int64
		targetID := int64(0)
		targetFound := false
		for _, acc := range accounts {
			if acc.AccountType != db.AccountTypeEquity {
				continue
			}
			switch acc.SystemRole {
			case models.SystemRoleRetainedEarnings:
				if !targetFound {
					targetID, targetFound = acc.ID, true
				}
			case models.SystemRoleIkhtisar, models.SystemRolePrive:
			default:
				targetID, targetFound = acc.ID, true
			}
			if targetFound {
				break
			}
		}
		harian := []int64{}
		for _, id := range order {
			s := perAkun[id]
			if isNominal(id) {
				switch {
				case meta[id].role == models.SystemRolePrive:
					priveNet += s.debit - s.credit
				default:
					nominalNet += s.credit - s.debit
				}
				continue
			}
			harian = append(harian, id)
		}
		order = harian
		adjust := nominalNet - priveNet
		if adjust != 0 {
			hasTarget := targetID != 0
			if !hasTarget {
				// Tidak ada akun ekuitas sama sekali: lakukan baris
				// penyeimbang sintetis seperti Kertas Kerja.
				targetID = -1
			}
			s, exists := perAkun[targetID]
			if !exists {
				name, code := "Laba (Rugi) Belum Ditutup", ""
				if m, ok := meta[targetID]; ok {
					name, code = m.name, ""
				}
				s = &saldo{normal: db.NormalBalanceCredit, code: code, name: name}
				perAkun[targetID] = s
				order = append(order, targetID)
			}
			if adjust > 0 {
				s.credit += adjust
			} else {
				s.debit += -adjust
			}
		}
	}

	for _, id := range order {
		s := perAkun[id]
		net := s.debit - s.credit
		if net == 0 {
			continue
		}
		row := models.TrialBalanceRow{Code: s.code, Name: s.name}
		// Saldo rata: netto positif tampil di kolom debit, netto negatif di
		// kolom kredit, apa pun saldo normal akunnya. Total kedua kolom
		// tetap berimbang karena setiap jurnal berimbang.
		if net > 0 {
			row.Debit = net
		} else {
			row.Credit = -net
		}
		view.Rows = append(view.Rows, row)
		view.TotalDebit += row.Debit
		view.TotalCredit += row.Credit
	}

	active := "trialbalance"
	title := "Neraca Saldo " + company.Name
	if postClosing {
		active = "postclosing"
		title = "Neraca Saldo Setelah Penutupan " + company.Name
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.TrialBalancePage(h.newShell(r, user, company, role, active, title),
		company, view, postClosing).Render(ctx, w)
}

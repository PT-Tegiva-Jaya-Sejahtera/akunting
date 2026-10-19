package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"accounting/internal/accounting"
	"accounting/internal/database/db"
	"accounting/internal/models"
	"accounting/templates"
)

// referenceSaldoAwal adalah referensi tetap jurnal saldo awal.
const referenceSaldoAwal = "SALDO-AWAL"

// PeriodsGet menampilkan daftar periode akuntansi (tahun buku) perusahaan.
func (h *Handlers) PeriodsGet(w http.ResponseWriter, r *http.Request) {
	h.renderPeriods(w, r, "")
}

// PeriodCreatePost menambah periode akuntansi baru.
func (h *Handlers) PeriodCreatePost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "400 form tidak valid", http.StatusBadRequest)
		return
	}
	_, _, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	startISO := strings.TrimSpace(r.FormValue("start_date"))
	endISO := strings.TrimSpace(r.FormValue("end_date"))
	start, err1 := models.ParseDateID(startISO)
	end, err2 := models.ParseDateID(endISO)

	switch {
	case name == "":
		h.renderPeriods(w, r, "Nama periode wajib diisi.")
		return
	case err1 != nil || err2 != nil:
		h.renderPeriods(w, r, "Tanggal awal dan akhir periode wajib diisi.")
		return
	case !end.After(start):
		h.renderPeriods(w, r, "Tanggal akhir harus setelah tanggal awal.")
		return
	}

	existing, err := h.DB.ListFiscalPeriods(r.Context(), company.ID)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	for _, p := range existing {
		pStart, pEnd := p.StartDate.Time, p.EndDate.Time
		if !start.After(pEnd) && !end.Before(pStart) {
			h.renderPeriods(w, r, "Periode tumpang tindih dengan "+p.Name+" ("+
				models.FormatDateID(pStart)+" s.d. "+models.FormatDateID(pEnd)+").")
			return
		}
	}

	// Periode pertama entitas adalah periode yang memuat tanggal paling awal;
	// hanya satu periode bertanda is_first (indeks unik di database).
	isFirst := true
	for _, p := range existing {
		if p.IsFirst {
			isFirst = false
			break
		}
	}

	if _, err := h.DB.CreateFiscalPeriod(r.Context(), db.CreateFiscalPeriodParams{
		CompanyID: company.ID,
		Name:      name,
		StartDate: dateOnly(start),
		EndDate:   dateOnly(end),
		IsFirst:   isFirst,
	}); err != nil {
		h.renderPeriods(w, r, "Gagal menyimpan periode (rentang tanggal mungkin sudah terdaftar).")
		return
	}
	http.Redirect(w, r, periodsPath(company.UrlKey), http.StatusSeeOther)
}

// PeriodClosePost menutup periode: membuat jurnal penutup ke saldo laba lalu
// mengunci periode supaya tidak bisa dimasuki jurnal baru.
func (h *Handlers) PeriodClosePost(w http.ResponseWriter, r *http.Request) {
	user, _, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	periodID, ok := h.periodIDParam(w, r)
	if !ok {
		return
	}
	period, err := h.DB.GetFiscalPeriod(r.Context(), db.GetFiscalPeriodParams{
		ID:        periodID,
		CompanyID: company.ID,
	})
	if err != nil {
		http.Error(w, "404 periode tidak ditemukan", http.StatusNotFound)
		return
	}
	if period.Status == db.PeriodStatusClosed {
		h.renderPeriods(w, r, "Periode "+period.Name+" sudah ditutup.")
		return
	}

	ikhtisarID, priveID, modalID, err := accounting.ProvisionClosingAccounts(r.Context(), h.DB, company.ID)
	if err != nil {
		if errors.Is(err, accounting.ErrNoClosingTarget) {
			h.renderPeriods(w, r, "Tutup buku butuh akun ekuitas Modal atau akun bertanda "+
				"\"Saldo Laba\". Tandai salah satu di halaman Akun lebih dulu.")
			return
		}
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}

	rows, err := h.periodRows(r.Context(), company.ID, period.StartDate.Time, period.EndDate.Time)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	lines, net := accounting.IkhtisarClosingLines(rows, ikhtisarID, modalID, priveID)

	if len(lines) > 0 {
		params := h.systemJournalParams(company.ID, user.UserID, period.EndDate.Time,
			"PENUTUP-"+strconv.FormatInt(period.ID, 10),
			"Jurnal penutup "+period.Name+" (laba periode: "+models.FormatCents(net)+")",
			db.JournalKindClosing, period.ID, lines)
		if _, err := h.DB.CreateSystemJournalWithEntries(r.Context(), params); err != nil {
			h.renderPeriods(w, r, "Gagal membuat jurnal penutup: periode tanpa mutasi atau jurnal tidak berimbang.")
			return
		}
	}

	closed, err := h.DB.CloseFiscalPeriod(r.Context(), db.CloseFiscalPeriodParams{
		ID:        period.ID,
		CompanyID: company.ID,
		ClosedBy:  pgtype.Int8{Int64: user.UserID, Valid: true},
	})
	if err != nil || closed == 0 {
		h.renderPeriods(w, r, "Periode gagal ditutup, coba lagi.")
		return
	}
	http.Redirect(w, r, periodsPath(company.UrlKey), http.StatusSeeOther)
}

// PeriodReopenPost membuka lagi periode yang sudah ditutup. Jurnal penutup
// periode itu dihapus supaya saldo tidak terhitung dua kali.
func (h *Handlers) PeriodReopenPost(w http.ResponseWriter, r *http.Request) {
	_, _, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	periodID, ok := h.periodIDParam(w, r)
	if !ok {
		return
	}
	period, err := h.DB.GetFiscalPeriod(r.Context(), db.GetFiscalPeriodParams{
		ID:        periodID,
		CompanyID: company.ID,
	})
	if err != nil {
		http.Error(w, "404 periode tidak ditemukan", http.StatusNotFound)
		return
	}
	if period.Status == db.PeriodStatusOpen {
		h.renderPeriods(w, r, "Periode "+period.Name+" masih terbuka.")
		return
	}
	if _, err := h.DB.DeleteClosingJournal(r.Context(), db.DeleteClosingJournalParams{
		CompanyID: company.ID,
		PeriodID:  pgtype.Int8{Int64: period.ID, Valid: true},
	}); err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	if n, err := h.DB.ReopenFiscalPeriod(r.Context(), db.ReopenFiscalPeriodParams{
		ID:        period.ID,
		CompanyID: company.ID,
	}); err != nil || n == 0 {
		h.renderPeriods(w, r, "Periode gagal dibuka lagi, coba lagi.")
		return
	}
	http.Redirect(w, r, periodsPath(company.UrlKey), http.StatusSeeOther)
}

// OpeningGet menampilkan form saldo awal periode pertama.
func (h *Handlers) OpeningGet(w http.ResponseWriter, r *http.Request) {
	h.renderOpening(w, r, "")
}

// OpeningPost menyimpan saldo awal sebagai satu jurnal khusus (kind=opening)
// bertanggal awal periode pertama. Isi sebelumnya diganti, bukan ditumpuk.
func (h *Handlers) OpeningPost(w http.ResponseWriter, r *http.Request) {
	user, _, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "400 form tidak valid", http.StatusBadRequest)
		return
	}

	period, found, err := h.firstPeriod(r, company.ID)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	if !found {
		h.renderOpening(w, r, "Buat periode akuntansi dulu sebelum mengisi saldo awal.")
		return
	}
	if period.Status == db.PeriodStatusClosed {
		h.renderOpening(w, r, "Periode "+period.Name+" sudah ditutup, saldo awal tidak bisa diubah.")
		return
	}

	accounts, err := h.DB.ListAccounts(r.Context(), company.ID)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}

	var lines []accounting.Line
	var totalDebit, totalCredit int64
	for _, acc := range accounts {
		idStr := strconv.FormatInt(acc.ID, 10)
		debit, errD := models.ParseMoney(r.FormValue("debit_" + idStr))
		credit, errC := models.ParseMoney(r.FormValue("credit_" + idStr))
		if errD != nil || errC != nil {
			h.renderOpening(w, r, "Format angka saldo awal tidak valid pada akun "+acc.Code+".")
			return
		}
		if debit > 0 && credit > 0 {
			h.renderOpening(w, r, "Akun "+acc.Code+": isi salah satu saja, debit atau kredit.")
			return
		}
		if debit == 0 && credit == 0 {
			continue
		}
		totalDebit += debit
		totalCredit += credit
		lines = append(lines, accounting.Line{
			AccountID:   acc.ID,
			Description: "Saldo awal " + acc.Code,
			DebitCents:  debit,
			CreditCents: credit,
		})
	}

	if len(lines) == 0 {
		h.renderOpening(w, r, "Belum ada saldo awal yang diisi.")
		return
	}
	if totalDebit != totalCredit {
		h.renderOpening(w, r, "Saldo awal tidak berimbang: debit "+
			models.FormatCents(totalDebit)+" vs kredit "+models.FormatCents(totalCredit)+".")
		return
	}

	// Ganti jurnal saldo awal sebelumnya (maksimum satu per perusahaan).
	if _, err := h.DB.DeleteSystemJournals(r.Context(), db.DeleteSystemJournalsParams{
		CompanyID: company.ID,
		Kind:      db.JournalKindOpening,
	}); err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}

	params := h.systemJournalParams(company.ID, user.UserID, period.StartDate.Time,
		referenceSaldoAwal, "Saldo awal per "+models.FormatDateID(period.StartDate.Time),
		db.JournalKindOpening, period.ID, lines)
	if _, err := h.DB.CreateSystemJournalWithEntries(r.Context(), params); err != nil {
		h.renderOpening(w, r, "Saldo awal ditolak database. Pastikan jumlah debit sama dengan kredit.")
		return
	}
	http.Redirect(w, r, openingPath(company.UrlKey), http.StatusSeeOther)
}

// renderPeriods memuat daftar periode dan menampilkannya.
func (h *Handlers) renderPeriods(w http.ResponseWriter, r *http.Request, errMsg string) {
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	periods, err := h.DB.ListFiscalPeriods(r.Context(), company.ID)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	views := make([]models.PeriodView, 0, len(periods))
	for _, p := range periods {
		views = append(views, periodView(p))
	}
	hasOpening, err := h.hasOpeningJournal(r, company.ID)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if errMsg != "" {
		w.WriteHeader(http.StatusBadRequest)
	}
	_ = templates.PeriodsPage(h.newShell(r, user, company, role, "periods", "Periode Akuntansi"), company, views, hasOpening, errMsg).Render(r.Context(), w)
}

// renderOpening memuat form saldo awal (termasuk nilai yang sudah tersimpan).
func (h *Handlers) renderOpening(w http.ResponseWriter, r *http.Request, errMsg string) {
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	view, err := h.openingView(r, company)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if errMsg != "" {
		w.WriteHeader(http.StatusBadRequest)
	}
	_ = templates.OpeningBalancePage(h.newShell(r, user, company, role, "periods", "Saldo Awal"), company, view, errMsg).Render(r.Context(), w)
}

func (h *Handlers) openingView(r *http.Request, company db.Company) (models.OpeningView, error) {
	view := models.OpeningView{}
	period, found, err := h.firstPeriod(r, company.ID)
	if err != nil {
		return view, err
	}
	if !found {
		return view, nil
	}
	view.HasPeriod = true
	view.Period = periodView(period)

	accounts, err := h.DB.ListAccounts(r.Context(), company.ID)
	if err != nil {
		return view, err
	}

	saved := map[int64]accounting.Line{}
	journals, err := h.DB.GetSystemJournals(r.Context(), db.GetSystemJournalsParams{
		CompanyID: company.ID,
		Kind:      db.JournalKindOpening,
	})
	if err != nil {
		return view, err
	}
	if len(journals) > 0 {
		j := journals[0]
		view.JournalID = j.ID
		view.SavedAt = models.FormatDateID(j.JournalDate.Time)
		entries, err := h.DB.GetJournalWithEntries(r.Context(), db.GetJournalWithEntriesParams{
			JournalID: j.ID,
			CompanyID: company.ID,
		})
		if err != nil {
			return view, err
		}
		for _, e := range entries {
			saved[e.AccountID] = accounting.Line{
				AccountID:   e.AccountID,
				DebitCents:  models.NumericToCents(e.Debit),
				CreditCents: models.NumericToCents(e.Credit),
			}
		}
	}

	for _, acc := range accounts {
		line := models.OpeningLine{
			AccountID: acc.ID,
			Code:      acc.Code,
			Name:      acc.Name,
			SubLabel:  models.SubTypeLabel(acc.SubType),
		}
		if l, ok := saved[acc.ID]; ok {
			line.Debit = l.DebitCents
			line.Credit = l.CreditCents
			view.TotalDebit += l.DebitCents
			view.TotalCredit += l.CreditCents
		}
		view.Lines = append(view.Lines, line)
	}
	return view, nil
}

func (h *Handlers) hasOpeningJournal(r *http.Request, companyID int64) (bool, error) {
	journals, err := h.DB.GetSystemJournals(r.Context(), db.GetSystemJournalsParams{
		CompanyID: companyID,
		Kind:      db.JournalKindOpening,
	})
	if err != nil {
		return false, err
	}
	return len(journals) > 0, nil
}

// firstPeriod mengambil periode entitas dengan tanggal awal paling awal.
func (h *Handlers) firstPeriod(r *http.Request, companyID int64) (db.FiscalPeriod, bool, error) {
	periods, err := h.DB.ListFiscalPeriods(r.Context(), companyID)
	if err != nil {
		return db.FiscalPeriod{}, false, err
	}
	if len(periods) == 0 {
		return db.FiscalPeriod{}, false, nil
	}
	first := periods[0]
	for _, p := range periods[1:] {
		if p.StartDate.Time.Before(first.StartDate.Time) {
			first = p
		}
	}
	return first, true, nil
}

// systemJournalParams menyusun parameter jurnal sistem (saldo awal/penutup).
func (h *Handlers) systemJournalParams(companyID, userID int64, date time.Time,
	reference, memo string, kind db.JournalKind, periodID int64, lines []accounting.Line,
) db.CreateSystemJournalWithEntriesParams {
	params := db.CreateSystemJournalWithEntriesParams{
		CompanyID:    companyID,
		JournalDate:  dateOnly(date),
		Reference:    reference,
		Memo:         memo,
		CreatedBy:    userID,
		Kind:         kind,
		PeriodID:     pgtype.Int8{Int64: periodID, Valid: periodID != 0},
		AccountIds:   make([]int64, len(lines)),
		Descriptions: make([]string, len(lines)),
		Debits:       make([]pgtype.Numeric, len(lines)),
		Credits:      make([]pgtype.Numeric, len(lines)),
	}
	for i, l := range lines {
		params.AccountIds[i] = l.AccountID
		params.Descriptions[i] = l.Description
		params.Debits[i], _ = models.CentsToNumeric(l.DebitCents)
		params.Credits[i], _ = models.CentsToNumeric(l.CreditCents)
	}
	return params
}

func (h *Handlers) periodIDParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "periodID"), 10, 64)
	if err != nil || id == 0 {
		http.Error(w, "400 id periode tidak valid", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

// periodView menerjemahkan baris periode menjadi bentuk siap tampil.
func periodView(p db.FiscalPeriod) models.PeriodView {
	v := models.PeriodView{
		ID:         p.ID,
		Name:       p.Name,
		StartISO:   p.StartDate.Time.Format("2006-01-02"),
		EndISO:     p.EndDate.Time.Format("2006-01-02"),
		StartLabel: models.FormatDateID(p.StartDate.Time),
		EndLabel:   models.FormatDateID(p.EndDate.Time),
		Status:     string(p.Status),
		IsFirst:    p.IsFirst,
	}
	if p.Status == db.PeriodStatusClosed {
		v.StatusLabel = "Ditutup"
		if p.ClosedAt.Valid {
			v.ClosedNote = models.FormatDateID(p.ClosedAt.Time)
		}
	} else {
		v.StatusLabel = "Terbuka"
	}
	return v
}

func dateOnly(t time.Time) pgtype.Date {
	return pgtype.Date{Time: t, Valid: true, InfinityModifier: pgtype.Finite}
}

func periodsPath(companyKey string) string {
	return "/companies/" + companyKey + "/periods"
}

func openingPath(companyKey string) string {
	return periodsPath(companyKey) + "/opening"
}

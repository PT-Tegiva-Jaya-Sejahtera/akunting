package handlers

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"accounting/internal/database/db"
	appmw "accounting/internal/middleware"
	"accounting/internal/models"
	"accounting/templates"
)

// JournalsListGet menampilkan daftar jurnal terbaru perusahaan beserta kartu
// "Posisi Semua Akun": seluruh baris buku kronologis dengan total di bawah.
func (h *Handlers) JournalsListGet(w http.ResponseWriter, r *http.Request) {
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}

	journals, err := h.DB.ListRecentJournals(r.Context(), db.ListRecentJournalsParams{
		CompanyID:  company.ID,
		LimitCount: 50,
	})
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}

	overview, ok := h.buildJournalsOverview(w, r, company)
	if !ok {
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.JournalsListPage(h.newShell(r, user, company, role, "journals", "Jurnal "+company.Name), company, journals, overview).Render(r.Context(), w)
}

// journalOverviewLimit batas baris tampilan menyeluruh supaya halaman tetap
// sehat; bila tercapai, halaman menandai data yang dipotong.
const journalOverviewLimit = 2000

// buildJournalsOverview menyusun tampilan "posisi semua akun": seluruh baris
// buku pada tahun terpilih (atau semua tahun), kronologis, dengan total.
func (h *Handlers) buildJournalsOverview(w http.ResponseWriter, r *http.Request, company db.Company) (models.JournalsOverviewView, bool) {
	ctx := r.Context()
	view := models.JournalsOverviewView{}

	yearRows, err := h.DB.ListJournalYears(ctx, company.ID)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return view, false
	}
	today := time.Now()
	yearSet := map[int]bool{today.Year(): true}
	for _, y := range yearRows {
		yearSet[int(y)] = true
	}
	for y := range yearSet {
		view.Years = append(view.Years, y)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(view.Years)))

	if s := r.URL.Query().Get("year"); s != "" {
		if y, err := strconv.Atoi(s); err == nil {
			view.SelectedYear = y
		}
	} else {
		view.SelectedYear = today.Year()
	}

	from := time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)
	if view.SelectedYear > 0 {
		from = time.Date(view.SelectedYear, 1, 1, 0, 0, 0, 0, time.UTC)
		to = time.Date(view.SelectedYear, 12, 31, 0, 0, 0, 0, time.UTC)
	}

	rows, err := h.DB.ListAllJournalLines(ctx, db.ListAllJournalLinesParams{
		CompanyID:  company.ID,
		DateFrom:   dateValue(from),
		DateTo:     dateValue(to),
		LimitCount: journalOverviewLimit + 1,
	})
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return view, false
	}
	if len(rows) > journalOverviewLimit {
		rows = rows[:journalOverviewLimit]
		view.Truncated = true
	}
	for _, row := range rows {
		line := models.JournalLineAll{
			DateLabel:   row.JournalDate.Time.Format("02-01-2006"),
			Reference:   row.Reference,
			AccountCode: row.AccountCode,
			AccountName: row.AccountName,
			Description: row.Description,
			Debit:       models.NumericToCents(row.Debit),
			Credit:      models.NumericToCents(row.Credit),
			IsAdjusting: row.IsAdjusting,
		}
		view.TotalDebit += line.Debit
		view.TotalCredit += line.Credit
		view.Rows = append(view.Rows, line)
	}
	return view, true
}

// JournalNewGet menampilkan form jurnal baru berisi dua baris kosong.
func (h *Handlers) JournalNewGet(w http.ResponseWriter, r *http.Request) {
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	accounts, ok := h.accountsFor(w, r, company)
	if !ok {
		return
	}
	parties, ok := h.partiesFor(w, r, company)
	if !ok {
		return
	}

	form := models.JournalForm{
		Date:        time.Now().Format("2006-01-02"),
		IsAdjusting: r.URL.Query().Get("adjusting") == "1",
		Lines:       []models.JournalLine{{}, {}},
	}
	h.renderJournalForm(w, r, user, company, role, accounts, parties, form)
}

// JournalLineGet mengembalikan satu baris kosong untuk ditambah HTMX
// (hx-get + hx-target=#journal-lines + hx-swap=beforeend).
func (h *Handlers) JournalLineGet(w http.ResponseWriter, r *http.Request) {
	_, _, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	accounts, ok := h.accountsFor(w, r, company)
	if !ok {
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.JournalLineRow(accounts, company.ID, models.JournalLine{}).Render(r.Context(), w)
}

// JournalPreviewPost menghitung total debit/kredit dari isi form saat ini
// untuk ditampilkan langsung (HTMX hx-trigger=input changed).
func (h *Handlers) JournalPreviewPost(w http.ResponseWriter, r *http.Request) {
	_, _, _, ok := h.companyContext(w, r)
	if !ok {
		return
	}

	form, _, _ := h.parseJournalForm(r)
	var totalDebit, totalCredit int64
	for _, line := range form.Lines {
		d, err1 := models.ParseMoney(line.DebitStr)
		c, err2 := models.ParseMoney(line.CreditStr)
		if err1 == nil {
			totalDebit += d
		}
		if err2 == nil {
			totalCredit += c
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.JournalTotalsFragment(totalDebit, totalCredit).Render(r.Context(), w)
}

// JournalCreatePost memvalidasi jurnal lalu menyimpannya secara atomik
// (header + semua baris) lewat CreateJournalWithEntries.
func (h *Handlers) JournalCreatePost(w http.ResponseWriter, r *http.Request) {
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	accounts, ok := h.accountsFor(w, r, company)
	if !ok {
		return
	}

	form, lines, errMsg := h.parseJournalForm(r)
	parties, ok := h.partiesFor(w, r, company)
	if !ok {
		return
	}
	if errMsg == "" {
		errMsg = validateJournalForm(form, lines, accounts, parties)
	}
	if errMsg != "" {
		form.Error = errMsg
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_ = templates.JournalFormPage(h.newShell(r, user, company, role, "journals", "Jurnal "+company.Name), company, accounts, parties, form).Render(r.Context(), w)
		return
	}

	journalDate, _ := models.ParseDateID(form.Date)
	params := db.CreateJournalWithEntriesParams{
		CompanyID:    company.ID,
		JournalDate:  pgtype.Date{Time: journalDate, Valid: true, InfinityModifier: pgtype.Finite},
		Reference:    strings.TrimSpace(form.Reference),
		Memo:         strings.TrimSpace(form.Memo),
		CreatedBy:    user.UserID,
		IsAdjusting:  form.IsAdjusting,
		AccountIds:   make([]int64, len(lines)),
		Descriptions: make([]string, len(lines)),
		Debits:       make([]pgtype.Numeric, len(lines)),
		Credits:      make([]pgtype.Numeric, len(lines)),
	}
	// Pihak (pelanggan/supplier) dan tanggal jatuh tempo adalah atribut
	// dokumen jurnal (skema Stage 3a: kolom journals.party_id + due_date).
	if form.PartyID != 0 {
		params.PartyID = pgtype.Int8{Int64: form.PartyID, Valid: true}
	}
	if strings.TrimSpace(form.DueDate) != "" {
		due, _ := models.ParseDateID(form.DueDate)
		params.DueDate = pgtype.Date{Time: due, Valid: true, InfinityModifier: pgtype.Finite}
	}
	for i, line := range lines {
		params.AccountIds[i] = line.AccountID
		params.Descriptions[i] = line.Description
		params.Debits[i], _ = models.CentsToNumeric(line.DebitCents)
		params.Credits[i], _ = models.CentsToNumeric(line.CreditCents)
	}

	journal, err := h.DB.CreateJournalWithEntries(r.Context(), params)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			form.Error = "Referensi jurnal sudah dipakai, gunakan yang lain."
		} else {
			form.Error = "Jurnal ditolak database: total debit harus sama dengan total kredit."
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_ = templates.JournalFormPage(h.newShell(r, user, company, role, "journals", "Jurnal "+company.Name), company, accounts, parties, form).Render(r.Context(), w)
		return
	}

	http.Redirect(w, r, "/companies/"+company.UrlKey+
		"/journals/"+strconv.FormatInt(journal.ID, 10), http.StatusSeeOther)
}

// JournalDetailGet menampilkan header jurnal dan seluruh baris ledger.
func (h *Handlers) JournalDetailGet(w http.ResponseWriter, r *http.Request) {
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}

	journalID, err := strconv.ParseInt(chi.URLParam(r, "journalID"), 10, 64)
	if err != nil {
		http.Error(w, "400 id jurnal tidak valid", http.StatusBadRequest)
		return
	}

	journal, err := h.DB.GetJournalByID(r.Context(), db.GetJournalByIDParams{
		ID:        journalID,
		CompanyID: company.ID,
	})
	if err != nil {
		http.Error(w, "404 jurnal tidak ditemukan", http.StatusNotFound)
		return
	}

	entries, err := h.DB.GetJournalWithEntries(r.Context(), db.GetJournalWithEntriesParams{
		JournalID: journal.ID,
		CompanyID: company.ID,
	})
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}

	var totalDebit, totalCredit int64
	for _, e := range entries {
		totalDebit += models.NumericToCents(e.Debit)
		totalCredit += models.NumericToCents(e.Credit)
	}

	// Nama pihak (pelanggan/supplier) untuk ditampilkan di header jurnal.
	partyName := ""
	if journal.PartyID.Valid {
		parties, ok := h.partiesFor(w, r, company)
		if !ok {
			return
		}
		for _, p := range parties {
			if p.ID == journal.PartyID.Int64 {
				partyName = p.Name
				break
			}
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.JournalDetailPage(h.newShell(r, user, company, role, "journals", "Jurnal "+journal.Reference), company, journal, entries,
		totalDebit, totalCredit, partyName).Render(r.Context(), w)
}

// journalLineInput adalah baris hasil parsing yang sudah lolos format uang.
type journalLineInput struct {
	AccountID   int64
	Description string
	DebitCents  int64
	CreditCents int64
}

// parseJournalForm membaca form jurnal mentah (tanpa validasi bisnis).
func (h *Handlers) parseJournalForm(r *http.Request) (models.JournalForm, []journalLineInput, string) {
	_ = r.ParseForm()

	form := models.JournalForm{
		Date:        r.FormValue("journal_date"),
		Reference:   r.FormValue("reference"),
		Memo:        r.FormValue("memo"),
		IsAdjusting: r.FormValue("is_adjusting") == "on",
		DueDate:     strings.TrimSpace(r.FormValue("due_date")),
	}
	if s := strings.TrimSpace(r.FormValue("party_id")); s != "" && s != "0" {
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return form, nil, "Pihak yang dipilih tidak valid."
		}
		form.PartyID = id
	}

	ids := r.Form["account_id"]
	descs := r.Form["description"]
	debits := r.Form["debit"]
	credits := r.Form["credit"]

	var lines []journalLineInput
	for i := range ids {
		line := models.JournalLine{}
		if i < len(descs) {
			line.Description = descs[i]
		}
		if i < len(debits) {
			line.DebitStr = debits[i]
		}
		if i < len(credits) {
			line.CreditStr = credits[i]
		}
		line.AccountID, _ = strconv.ParseInt(strings.TrimSpace(ids[i]), 10, 64)
		form.Lines = append(form.Lines, line)

		// Baris kosong sepenuhnya dilewati, bukan divalidasi.
		if line.AccountID == 0 && strings.TrimSpace(line.Description) == "" &&
			strings.TrimSpace(line.DebitStr) == "" && strings.TrimSpace(line.CreditStr) == "" {
			continue
		}

		d, err := models.ParseMoney(line.DebitStr)
		if err != nil {
			return form, nil, "Baris " + strconv.Itoa(i+1) + ": format debit tidak valid."
		}
		c, err := models.ParseMoney(line.CreditStr)
		if err != nil {
			return form, nil, "Baris " + strconv.Itoa(i+1) + ": format kredit tidak valid."
		}
		lines = append(lines, journalLineInput{
			AccountID:   line.AccountID,
			Description: strings.TrimSpace(line.Description),
			DebitCents:  d,
			CreditCents: c,
		})
	}
	return form, lines, ""
}

// validateJournalForm menerapkan aturan double-entry sebelum menyimpan.
// Pihak harus ada di perusahaan ini; tanggal jatuh tempo opsional tetapi bila
// diisi harus berformat tanggal yang dikenal.
func validateJournalForm(form models.JournalForm, lines []journalLineInput, accounts []db.Account, parties []db.Party) string {
	if _, err := models.ParseDateID(form.Date); err != nil {
		return "Tanggal jurnal harus berformat dd/mm/yyyy."
	}
	if strings.TrimSpace(form.DueDate) != "" {
		if _, err := models.ParseDateID(form.DueDate); err != nil {
			return "Tanggal jatuh tempo harus berformat dd/mm/yyyy."
		}
	}
	if form.PartyID != 0 {
		found := false
		for _, p := range parties {
			if p.ID == form.PartyID {
				found = true
				break
			}
		}
		if !found {
			return "Pihak yang dipilih tidak dikenal di perusahaan ini."
		}
	}
	if strings.TrimSpace(form.Reference) == "" {
		return "Referensi jurnal wajib diisi."
	}
	if len(lines) < 2 {
		return "Minimal 2 baris transaksi (satu debit dan satu kredit)."
	}

	validAccounts := make(map[int64]bool, len(accounts))
	for _, a := range accounts {
		validAccounts[a.ID] = true
	}

	var totalDebit, totalCredit int64
	for i, line := range lines {
		if line.AccountID == 0 || !validAccounts[line.AccountID] {
			return "Baris " + strconv.Itoa(i+1) + ": pilih akun yang valid."
		}
		if (line.DebitCents > 0) == (line.CreditCents > 0) {
			return "Baris " + strconv.Itoa(i+1) + ": isi debit ATAU kredit, tidak boleh keduanya."
		}
		totalDebit += line.DebitCents
		totalCredit += line.CreditCents
	}

	if totalDebit == 0 {
		return "Total transaksi tidak boleh nol."
	}
	if totalDebit != totalCredit {
		return "Jurnal tidak berimbang: debit " + models.FormatCents(totalDebit) +
			" vs kredit " + models.FormatCents(totalCredit) + "."
	}
	return ""
}

func (h *Handlers) renderJournalForm(w http.ResponseWriter, r *http.Request,
	user appmw.SessionUser, company db.Company, role string, accounts []db.Account, parties []db.Party, form models.JournalForm) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.JournalFormPage(h.newShell(r, user, company, role, "journals", "Jurnal "+company.Name), company, accounts, parties, form).Render(r.Context(), w)
}

func (h *Handlers) accountsFor(w http.ResponseWriter, r *http.Request, company db.Company) ([]db.Account, bool) {
	accounts, err := h.DB.ListAccounts(r.Context(), company.ID)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return nil, false
	}
	return accounts, true
}

// partiesFor mengambil daftar pihak (pelanggan/supplier) perusahaan untuk
// dropdown form jurnal dan validasi pilihan user.
func (h *Handlers) partiesFor(w http.ResponseWriter, r *http.Request, company db.Company) ([]db.Party, bool) {
	parties, err := h.DB.ListParties(r.Context(), company.ID)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return nil, false
	}
	return parties, true
}

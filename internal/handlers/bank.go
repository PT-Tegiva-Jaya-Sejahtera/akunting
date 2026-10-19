// Rekonsiliasi kas/bank (Stage B): daftar rekening bank, impor mutasi CSV
// (pembacaan berkas lewat modul internal/exchange), pencocokan baris mutasi
// dengan baris ledger, dan jurnal selisih kurs otomatis yang ditulis ke ledger
// double-entry lewat CreateJournalWithEntries (trigger penjaga saldo tetap
// menjadi baris pengaman terakhir).
package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"accounting/internal/database/db"
	"accounting/internal/exchange"
	appmw "accounting/internal/middleware"
	"accounting/internal/models"
	"accounting/templates"
)

// systemRoleSelisihKurs menandai akun COA tujuan selisih rekonsiliasi
// (mis. keselisihan kurs atau biaya bank). Akun dibuat bila belum ada.
const systemRoleSelisihKurs = "selisih_kurs"

// BankGet menampilkan daftar rekening bank perusahaan.
func (h *Handlers) BankGet(w http.ResponseWriter, r *http.Request) {
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	h.renderBank(w, r, user, role, company, "")
}

// renderBank menyusun halaman daftar rekening bank beserta jumlah mutasi yang
// belum cocok per rekening.
func (h *Handlers) renderBank(w http.ResponseWriter, r *http.Request,
	user appmw.SessionUser, role string, company db.Company, errMsg string) {
	rows, err := h.DB.ListBankAccounts(r.Context(), company.ID)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	view := models.BankView{
		RoundingUnit:  company.RoundingUnit,
		RoundingLabel: models.RoundingLabel(company.RoundingUnit),
		CanManage:     role == appmw.RoleAdmin || role == appmw.RoleOwner,
	}
	coa, okA := h.accountsFor(w, r, company)
	if !okA {
		return
	}
	for _, a := range coa {
		if a.AccountType == "asset" {
			view.COA = append(view.COA, models.BankCoaOption{ID: a.ID, Code: a.Code, Name: a.Name})
		}
	}
	for _, a := range rows {
		view.Accounts = append(view.Accounts, models.BankAccountRow{
			ID:             a.ID,
			AccountID:      a.AccountID,
			AccountCode:    a.AccountCode,
			AccountName:    a.AccountName,
			BankName:       a.BankName,
			AccountNumber:  a.AccountNumber,
			Currency:       a.Currency,
			UnmatchedCount: a.UnmatchedCount,
		})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.BankPage(h.newShell(r, user, company, role, "bank", "Kas & Bank "+company.Name), company, view, errMsg).Render(r.Context(), w)
}

// BankCreatePost mendaftarkan rekening bank baru yang menunjuk akun COA.
func (h *Handlers) BankCreatePost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "400 form tidak valid", http.StatusBadRequest)
		return
	}
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	if role != appmw.RoleAdmin && role != appmw.RoleOwner {
		http.Error(w, "403 hanya admin atau owner yang boleh mengatur rekening bank", http.StatusForbidden)
		return
	}

	accountID, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("account_id")), 10, 64)
	if err != nil || accountID == 0 {
		h.renderBank(w, r, user, role, company, "Pilih akun COA kas di bank.")
		return
	}
	bankName := strings.TrimSpace(r.FormValue("bank_name"))
	if bankName == "" {
		h.renderBank(w, r, user, role, company, "Nama bank wajib diisi.")
		return
	}
	_, err = h.DB.CreateBankAccount(r.Context(), db.CreateBankAccountParams{
		CompanyID:     company.ID,
		AccountID:     accountID,
		BankName:      bankName,
		AccountNumber: strings.TrimSpace(r.FormValue("account_number")),
		Currency:      strings.ToUpper(strings.TrimSpace(r.FormValue("currency"))),
	})
	if err != nil {
		// 23505 = akun COA sudah dipakai rekening bank lain.
		h.renderBank(w, r, user, role, company, "Gagal menyimpan: mungkin akun COA tersebut sudah dipakai rekening lain.")
		return
	}
	http.Redirect(w, r, "/companies/"+company.UrlKey+"/bank", http.StatusSeeOther)
}

// BankReconcileGet menampilkan halaman rekonsiliasi satu rekening bank:
// saldo buku vs mutasi, daftar baris mutasi beserta statusnya, dan daftar
// kandidat untuk baris yang belum cocok.
func (h *Handlers) BankReconcileGet(w http.ResponseWriter, r *http.Request) {
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	bankID, err := strconv.ParseInt(chi.URLParam(r, "bankAccountID"), 10, 64)
	if err != nil {
		http.Error(w, "400 id rekening tidak valid", http.StatusBadRequest)
		return
	}
	bank, okB := h.bankAccount(w, r, company, bankID)
	if !okB {
		return
	}

	lines, err := h.DB.ListStatementLines(r.Context(), db.ListStatementLinesParams{
		CompanyID: company.ID, BankAccountID: bank.ID,
	})
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	candidates, err := h.DB.ListReconcileCandidates(r.Context(), db.ListReconcileCandidatesParams{
		CompanyID: company.ID, AccountID: bank.AccountID,
		RefPrefix: models.ReferencePrefixRekonsiliasi + "%",
	})
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}

	// Saldo buku akun COA sampai hari ini (debit dikurangi credit).
	totals, err := h.DB.ListAccountTotalsByPeriod(r.Context(), db.ListAccountTotalsByPeriodParams{
		CompanyID: company.ID,
		DateFrom:  pgtype.Date{Time: time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true, InfinityModifier: pgtype.Finite},
		DateTo:    pgtype.Date{Time: time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC), Valid: true, InfinityModifier: pgtype.Finite},
	})
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	var bookCents int64
	for _, t := range totals {
		if t.ID == bank.AccountID {
			bookCents = models.NumericToCents(t.TotalDebit) - models.NumericToCents(t.TotalCredit)
			break
		}
	}

	view := models.BankReconView{
		Bank: models.BankAccountRow{
			ID:            bank.ID,
			AccountID:     bank.AccountID,
			AccountCode:   bank.AccountCode,
			AccountName:   bank.AccountName,
			BankName:      bank.BankName,
			AccountNumber: bank.AccountNumber,
			Currency:      bank.Currency,
		},
		BookCents:     bookCents,
		RoundingUnit:  company.RoundingUnit,
		RoundingLabel: models.RoundingLabel(company.RoundingUnit),
		CanManage:     role == appmw.RoleAdmin || role == appmw.RoleOwner,
	}

	// Nama jurnal yang sudah terikat ke baris mutasi, untuk tampilan.
	jrefs := make(map[int64]string)
	for _, line := range lines {
		if line.MatchedJournalID.Valid {
			jrefs[line.MatchedJournalID.Int64] = ""
		}
	}
	for jid := range jrefs {
		if jr, err := h.DB.GetJournalByID(r.Context(), db.GetJournalByIDParams{ID: jid, CompanyID: company.ID}); err == nil {
			jrefs[jid] = jr.Reference
		}
	}

	var bankSum, unmatchedSum int64
	for _, line := range lines {
		in, out := models.NumericToCents(line.AmountIn), models.NumericToCents(line.AmountOut)
		stmtSigned := in - out
		view.Bank.BankName = bank.BankName
		bankSum += stmtSigned
		row := models.StatementRow{
			ID:          line.ID,
			DateLabel:   models.FormatDateID(line.LineDate.Time),
			Description: line.Description,
			Reference:   line.Reference,
			SourceFile:  line.SourceFile,
			InCents:     in,
			OutCents:    out,
		}
		if line.Balance.Valid {
			row.HasBalance = true
			row.BalanceCents = models.NumericToCents(line.Balance)
		}
		row.Status = string(line.Status)
		if line.Status == "unmatched" {
			for _, c := range candidates {
				bookSigned := models.NumericToCents(c.Debit) - models.NumericToCents(c.Credit)
				row.Candidates = append(row.Candidates, models.CandidateOption{
					EntryID:     c.ID,
					DateLabel:   models.FormatDateID(c.JournalDate.Time),
					Reference:   c.Reference,
					Description: c.Description,
					AmountCents: bookSigned,
					DiffCents:   stmtSigned - bookSigned,
				})
			}
			unmatchedSum += stmtSigned
		} else {
			row.MatchedEntryID = line.MatchedEntryID.Int64
			if line.MatchedJournalID.Valid {
				row.MatchedJournalID = line.MatchedJournalID.Int64
				row.MatchedJournalRef = jrefs[row.MatchedJournalID]
			}
		}
		view.Lines = append(view.Lines, row)
	}
	view.BankCents = bankSum
	view.UnmatchedTotal = unmatchedSum

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.BankReconcilePage(h.newShell(r, user, company, role, "bank",
		"Rekonsiliasi "+bank.BankName+" "+company.Name), company, view).Render(r.Context(), w)
}

// bankAccount memuat rekening bank milik perusahaan; bila tidak ada, halaman
// error sudah ditulis dan false dikembalikan.
func (h *Handlers) bankAccount(w http.ResponseWriter, r *http.Request, company db.Company, id int64) (db.GetBankAccountByIDRow, bool) {
	bank, err := h.DB.GetBankAccountByID(r.Context(), db.GetBankAccountByIDParams{ID: id, CompanyID: company.ID})
	if err != nil {
		http.Error(w, "404 rekening bank tidak ditemukan", http.StatusNotFound)
		return db.GetBankAccountByIDRow{}, false
	}
	return bank, true
}

// renderBankImportPreview menampilkan hasil pembacaan berkas mutasi sebelum
// disimpan (pola dua langkah seperti import modul exchange).
func (h *Handlers) renderBankImportPreview(w http.ResponseWriter, r *http.Request,
	user appmw.SessionUser, role string, company db.Company, bank db.GetBankAccountByIDRow,
	fileName string, input []models.StatementInput, totalRows int, perrs []string, token string) {
	var rows []models.ImportPreviewRow
	for _, s := range input {
		rows = append(rows, models.ImportPreviewRow{
			DateLabel:   models.FormatDateID(s.Date),
			Description: s.Description,
			Reference:   s.Reference,
			InCents:     s.InCents,
			OutCents:    s.OutCents,
		})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if len(perrs) > 0 && len(input) == 0 {
		w.WriteHeader(http.StatusBadRequest)
	}
	_ = templates.BankImportPreviewPage(h.newShell(r, user, company, role, "bank",
		"Periksa Impor Mutasi "+company.Name), company, models.BankImportPreviewView{
		BankID:       bank.ID,
		BankLabel:    bank.BankName + " " + bank.AccountNumber,
		FileName:     fileName,
		TotalRows:    totalRows,
		Valid:        len(input),
		RoundingUnit: company.RoundingUnit,
		Rows:         rows,
		Errors:       perrs,
		Token:        token,
	}).Render(r.Context(), w)
}

// BankImportPost menerima berkas mutasi bank (CSV/TXT/Excel/XML), membacanya
// lewat modul exchange, lalu menampilkan pratinjau sebelum diterapkan.
func (h *Handlers) BankImportPost(w http.ResponseWriter, r *http.Request) {
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	bankID, err := strconv.ParseInt(chi.URLParam(r, "bankAccountID"), 10, 64)
	if err != nil {
		http.Error(w, "400 id rekening tidak valid", http.StatusBadRequest)
		return
	}
	bank, okB := h.bankAccount(w, r, company, bankID)
	if !okB {
		return
	}
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		http.Error(w, "400 berkas terlalu besar atau form tidak valid", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("berkas")
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_ = templates.BankImportPreviewPage(h.newShell(r, user, company, role, "bank",
			"Periksa Impor Mutasi "+company.Name), company, models.BankImportPreviewView{
			BankID:    bank.ID,
			BankLabel: bank.BankName + " " + bank.AccountNumber,
			Errors:    []string{"Berkas belum dipilih."},
		}).Render(r.Context(), w)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 8<<20))
	if err != nil {
		http.Error(w, "400 berkas tidak dapat dibaca", http.StatusBadRequest)
		return
	}
	table, err := exchange.Read(data, header.Filename)
	if err != nil {
		http.Error(w, "400 "+err.Error(), http.StatusBadRequest)
		return
	}
	input, perrs := models.ParseStatementRows(table.Header, table.Rows)
	token, err := saveUpload(data, header.Filename)
	if err != nil {
		http.Error(w, "500 berkas unggahan tidak dapat disimpan", http.StatusInternalServerError)
		return
	}
	h.renderBankImportPreview(w, r, user, role, company, bank, header.Filename, input, len(table.Rows), perrs, token)
}

// BankImportConfirm menyimpan baris mutasi yang sudah diperiksa.
func (h *Handlers) BankImportConfirm(w http.ResponseWriter, r *http.Request) {
	_, _, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	bankID, err := strconv.ParseInt(chi.URLParam(r, "bankAccountID"), 10, 64)
	if err != nil {
		http.Error(w, "400 id rekening tidak valid", http.StatusBadRequest)
		return
	}
	bank, okB := h.bankAccount(w, r, company, bankID)
	if !okB {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "400 form tidak valid", http.StatusBadRequest)
		return
	}
	token := strings.TrimSpace(r.FormValue("token"))
	data, name, err := loadUpload(token)
	if err != nil {
		http.Error(w, "410 berkas unggahan sudah tidak tersedia, ulangi unggahan", http.StatusGone)
		return
	}
	table, err := exchange.Read(data, name)
	if err != nil {
		http.Error(w, "400 "+err.Error(), http.StatusBadRequest)
		return
	}
	input, _ := models.ParseStatementRows(table.Header, table.Rows)
	if len(input) == 0 {
		http.Error(w, "400 tidak ada baris valid untuk disimpan", http.StatusBadRequest)
		return
	}
	for _, s := range input {
		inN, _ := models.CentsToNumeric(s.InCents)
		outN, _ := models.CentsToNumeric(s.OutCents)
		var balanceN pgtype.Numeric
		if s.HasBalance {
			balanceN, _ = models.CentsToNumeric(s.BalanceCents)
		}
		if _, err := h.DB.InsertStatementLine(r.Context(), db.InsertStatementLineParams{
			CompanyID:     company.ID,
			BankAccountID: bank.ID,
			LineDate:      pgtype.Date{Time: s.Date, Valid: true, InfinityModifier: pgtype.Finite},
			Description:   s.Description,
			Reference:     s.Reference,
			AmountIn:      inN,
			AmountOut:     outN,
			Balance:       balanceN,
			SourceFile:    name,
		}); err != nil {
			http.Error(w, "500 kesalahan server saat menyimpan mutasi", http.StatusInternalServerError)
			return
		}
	}
	removeUpload(token)
	http.Redirect(w, r, fmt.Sprintf("/companies/%s/bank/%d", company.UrlKey, bank.ID), http.StatusSeeOther)
}

// ensureSelisihAccount mencari atau membuat akun COA tujuan selisih
// rekonsiliasi (kurs/biaya bank) untuk perusahaan. Kode akun diambil dari blok
// 9901xx berurutan sampai ketemu nomor yang belum dipakai (unique company,code).
func ensureSelisihAccount(ctx context.Context, q *db.Queries, companyID int64) (db.Account, error) {
	a, err := q.GetAccountBySystemRole(ctx, db.GetAccountBySystemRoleParams{
		CompanyID:  companyID,
		SystemRole: systemRoleSelisihKurs,
	})
	if err == nil {
		return a, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.Account{}, err
	}
	for i := 1; i <= 99; i++ {
		code := fmt.Sprintf("9901%02d", i)
		na, err := q.CreateAccount(ctx, db.CreateAccountParams{
			CompanyID:     companyID,
			Code:          code,
			Name:          "Selisih Nilai Tukar (kurs)",
			AccountType:   "expense",
			SubType:       "",
			NormalBalance: "debit",
			SystemRole:    systemRoleSelisihKurs,
			PsakLine:      models.PsakBebanOperasional,
		})
		if err == nil {
			return na, nil
		}
	}
	return db.Account{}, errors.New("tidak ada kode akun bebas untuk akun selisih kurs")
}

// StatementMatchPost mencocokkan satu baris mutasi dengan satu baris ledger.
// Bila jumlahnya berbeda, jurnal selisih kurs otomatis dibuat: akun kas/bank
// disesuaikan agar sama dengan mutasi bank, dan selisihnya masuk ke akun
// Selisih Nilai Tukar di sisi sebaliknya. Seluruh langkah (jurnal penyesuaian
// + pembaruan status mutasi) berjalan dalam satu transaksi.
func (h *Handlers) StatementMatchPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "400 form tidak valid", http.StatusBadRequest)
		return
	}
	user, _, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	lineID, errL := strconv.ParseInt(chi.URLParam(r, "statementID"), 10, 64)
	entryID, errE := strconv.ParseInt(strings.TrimSpace(r.FormValue("entry_id")), 10, 64)
	if errL != nil || errE != nil || entryID == 0 {
		http.Error(w, "400 pilihan pencocokan tidak valid", http.StatusBadRequest)
		return
	}
	line, err := h.DB.GetStatementLineByID(r.Context(), db.GetStatementLineByIDParams{ID: lineID, CompanyID: company.ID})
	if err != nil {
		http.Error(w, "404 baris mutasi tidak ditemukan", http.StatusNotFound)
		return
	}
	if line.Status != "unmatched" {
		http.Error(w, "409 baris mutasi sudah dicocokkan", http.StatusConflict)
		return
	}
	bank, err := h.DB.GetBankAccountByID(r.Context(), db.GetBankAccountByIDParams{
		ID: line.BankAccountID, CompanyID: company.ID,
	})
	if err != nil {
		http.Error(w, "404 rekening bank tidak ditemukan", http.StatusNotFound)
		return
	}
	entry, err := h.getFreeEntry(r.Context(), company.ID, bank.AccountID, entryID)
	if err != nil {
		http.Error(w, "409 baris ledger tidak dapat dicocokkan", http.StatusConflict)
		return
	}

	// Selisih mutasi vs buku memakai tanda kas: masuk positif, keluar negatif.
	stmtSigned := models.NumericToCents(line.AmountIn) - models.NumericToCents(line.AmountOut)
	bookSigned := models.NumericToCents(entry.Debit) - models.NumericToCents(entry.Credit)
	selisihAccount, err := ensureSelisihAccount(r.Context(), h.DB, company.ID)
	if err != nil {
		http.Error(w, "500 kesalahan server (akun selisih)", http.StatusInternalServerError)
		return
	}

	_, diffLines := models.BuildSelisihLines(
		bank.AccountID, selisihAccount.ID,
		"Selisih rekonsiliasi "+bank.BankName,
		"Selisih rekonsiliasi: "+line.Description,
		bookSigned, stmtSigned,
	)

	if len(diffLines) > 0 {
		if h.Pool == nil {
			http.Error(w, "500 pool database tidak tersedia", http.StatusInternalServerError)
			return
		}
		tx, errT := h.Pool.Begin(r.Context())
		if errT != nil {
			http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback(r.Context())
		qtx := h.DB.WithTx(tx)

		accountIds := []int64{diffLines[0].AccountID, diffLines[1].AccountID}
		descs := []string{diffLines[0].Description, diffLines[1].Description}
		debits := make([]pgtype.Numeric, 2)
		credits := make([]pgtype.Numeric, 2)
		for i, dl := range diffLines {
			debits[i], _ = models.CentsToNumeric(dl.DebitCents)
			credits[i], _ = models.CentsToNumeric(dl.CreditCents)
		}
		j, errJ := qtx.CreateJournalWithEntries(r.Context(), db.CreateJournalWithEntriesParams{
			CompanyID: company.ID,
			JournalDate: pgtype.Date{
				Time: line.LineDate.Time, Valid: true, InfinityModifier: pgtype.Finite,
			},
			Reference:    models.ReferencePrefixRekonsiliasi + strconv.FormatInt(line.ID, 10),
			Memo:         "Selisih rekonsiliasi: " + line.Description,
			CreatedBy:    user.UserID,
			IsAdjusting:  true,
			AccountIds:   accountIds,
			Descriptions: descs,
			Debits:       debits,
			Credits:      credits,
		})
		if errJ != nil {
			log.Printf("bank: jurnal selisih rekonsiliasi ditolak (line %d): %v", line.ID, errJ)
			http.Error(w, "500 jurnal selisih ditolak database", http.StatusInternalServerError)
			return
		}
		if errM := qtx.MatchStatementLine(r.Context(), db.MatchStatementLineParams{
			Adjusted:         true,
			MatchedEntryID:   pgtype.Int8{Int64: entry.ID, Valid: true},
			MatchedJournalID: j.ID,
			ID:               line.ID,
			CompanyID:        company.ID,
		}); errM != nil {
			http.Error(w, "500 menyimpan hasil rekonsiliasi gagal", http.StatusInternalServerError)
			return
		}
		if errC := tx.Commit(r.Context()); errC != nil {
			http.Error(w, "500 menyimpan hasil rekonsiliasi gagal", http.StatusInternalServerError)
			return
		}
	} else {
		if errM := h.DB.MatchStatementLine(r.Context(), db.MatchStatementLineParams{
			Adjusted:       false,
			MatchedEntryID: pgtype.Int8{Int64: entry.ID, Valid: true},
			ID:             line.ID,
			CompanyID:      company.ID,
		}); errM != nil {
			http.Error(w, "500 menyimpan hasil rekonsiliasi gagal", http.StatusInternalServerError)
			return
		}
	}

	http.Redirect(w, r, fmt.Sprintf("/companies/%s/bank/%d", company.UrlKey, bank.ID), http.StatusSeeOther)
}

// getFreeEntry memuat baris ledger yang boleh dicocokkan: pada akun COA
// rekening bank, jurnal posted yang bukan closing dan bukan hasil rekonsiliasi,
// dan belum dipakai baris mutasi lain.
func (h *Handlers) getFreeEntry(ctx context.Context, companyID, accountID, entryID int64) (db.ListReconcileCandidatesRow, error) {
	candidates, err := h.DB.ListReconcileCandidates(ctx, db.ListReconcileCandidatesParams{
		CompanyID: companyID, AccountID: accountID,
		RefPrefix: models.ReferencePrefixRekonsiliasi + "%",
	})
	if err != nil {
		return db.ListReconcileCandidatesRow{}, err
	}
	for _, c := range candidates {
		if c.ID == entryID {
			return c, nil
		}
	}
	return db.ListReconcileCandidatesRow{}, pgx.ErrNoRows
}

// StatementAutomatchPost mencocokkan seluruh baris mutasi yang tanggal DAN
// jumlahnya persis sama dengan baris ledger yang masih bebas (satu-ke-satu).
func (h *Handlers) StatementAutomatchPost(w http.ResponseWriter, r *http.Request) {
	_, _, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	bankID, err := strconv.ParseInt(chi.URLParam(r, "bankAccountID"), 10, 64)
	if err != nil {
		http.Error(w, "400 id rekening tidak valid", http.StatusBadRequest)
		return
	}
	bank, okB := h.bankAccount(w, r, company, bankID)
	if !okB {
		return
	}
	lines, err := h.DB.ListStatementLines(r.Context(), db.ListStatementLinesParams{
		CompanyID: company.ID, BankAccountID: bank.ID,
	})
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	candidates, err := h.DB.ListReconcileCandidates(r.Context(), db.ListReconcileCandidatesParams{
		CompanyID: company.ID, AccountID: bank.AccountID,
		RefPrefix: models.ReferencePrefixRekonsiliasi + "%",
	})
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	var lks []models.LineKey
	for _, l := range lines {
		if l.Status != "unmatched" {
			continue
		}
		lks = append(lks, models.LineKey{
			ID:       l.ID,
			Date:     l.LineDate.Time,
			InCents:  models.NumericToCents(l.AmountIn),
			OutCents: models.NumericToCents(l.AmountOut),
		})
	}
	var eks []models.EntryKey
	for _, c := range candidates {
		eks = append(eks, models.EntryKey{
			ID:          c.ID,
			Date:        c.JournalDate.Time,
			DebitCents:  models.NumericToCents(c.Debit),
			CreditCents: models.NumericToCents(c.Credit),
		})
	}
	for _, p := range models.BuildAutoMatches(lks, eks) {
		if err := h.DB.MatchStatementLine(r.Context(), db.MatchStatementLineParams{
			Adjusted:       false,
			MatchedEntryID: pgtype.Int8{Int64: p[1], Valid: true},
			ID:             p[0],
			CompanyID:      company.ID,
		}); err != nil {
			http.Error(w, "500 pencocokan otomatis gagal", http.StatusInternalServerError)
			return
		}
	}
	http.Redirect(w, r, fmt.Sprintf("/companies/%s/bank/%d", company.UrlKey, bank.ID), http.StatusSeeOther)
}

// StatementUnmatchPost melepaskan pencocokan: baris mutasi kembali "belum
// cocok". Jurnal selisih yang pernah dibuat tidak dihapus; hapus lewat
// halaman jurnal bila dianggap salah.
func (h *Handlers) StatementUnmatchPost(w http.ResponseWriter, r *http.Request) {
	_, _, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	lineID, err := strconv.ParseInt(chi.URLParam(r, "statementID"), 10, 64)
	if err != nil {
		http.Error(w, "400 id mutasi tidak valid", http.StatusBadRequest)
		return
	}
	line, err := h.DB.GetStatementLineByID(r.Context(), db.GetStatementLineByIDParams{ID: lineID, CompanyID: company.ID})
	if err != nil {
		http.Error(w, "404 baris mutasi tidak ditemukan", http.StatusNotFound)
		return
	}
	if err := h.DB.UnmatchStatementLine(r.Context(), db.UnmatchStatementLineParams{ID: line.ID, CompanyID: company.ID}); err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/companies/%s/bank/%d", company.UrlKey, line.BankAccountID), http.StatusSeeOther)
}

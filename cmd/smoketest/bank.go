// Bukti runtime rekonsiliasi kas/bank (Stage B). Satu perusahaan uji disi
// dengan rekening bank yang menunjuk akun COA kas di bank, tiga jurnal, lalu:
//
//  1. parsing CSV mutasi: baris salah (dua sisi, tanggal, angka) ditolak
//     dengan pesan per baris, baris benar diterima;
//  2. impor mutasi lewat route HTTP asli (multipart -> pratinjau -> konfirmasi),
//     dibaca lewat modul internal/exchange;
//  3. pencocokan otomatis menutup baris yang tanggal DAN jumlahnya persis sama;
//  4. pencocokan dengan selisih lewat handler asli membuat jurnal penyesuaian
//     "REC-<id>" (debit kas, kredit akun Selisih Nilai Tukar) dalam satu
//     transaksi dan menandai baris mutasi "adjusted";
//  5. saldo buku akun kas di bank akhirnya sama dengan total mutasi bank, dan
//     ledger double-entry seluruh perusahaan tetap seimbang (debit = kredit);
//  6. unmatch melepas pencocokan dan baris bisa dicocokkan ulang;
//  7. halaman daftar bank dan halaman rekonsiliasi dirender 200 lewat router
//     dengan middleware asli (RequireAuth + RequireCompany);
//  8. FK komposit menolak baris mutasi lintas perusahaan.
//
// Set SMOKE_SAVE_DIR untuk menyimpan HTML hasil render sebagai bukti berkas.
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"accounting/internal/auth"
	"accounting/internal/database/db"
	"accounting/internal/handlers"
	appmw "accounting/internal/middleware"
	"accounting/internal/models"
)

// evidenceBank menjalankan seluruh bukti rekonsiliasi kas/bank.
func evidenceBank(ctx context.Context, pool *pgxpool.Pool, q *db.Queries, userID int64) {
	// Unggahan sementara impor mutasi ditaruh di direktori sementara supaya
	// worktree repo tetap bersih.
	upDir, err := os.MkdirTemp("", "smoke-bank-upload")
	if err != nil {
		panic(err)
	}
	os.Setenv("EXCHANGE_DIR", upDir)
	defer os.RemoveAll(upDir)
	defer os.Unsetenv("EXCHANGE_DIR")

	// ===== 1. Perusahaan, akun, rekening bank =====
	company, err := q.CreateCompany(ctx, db.CreateCompanyParams{
		Name: "PT Bukti Bank", Slug: "pt-bukti-bank",
		BaseCurrency: "IDR", UrlKey: "buktibank",
	})
	if err != nil {
		panic(err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO company_users (company_id, user_id, role) VALUES ($1, $2, 'owner')`,
		company.ID, userID); err != nil {
		panic(err)
	}

	akunID := map[string]int64{}
	for _, a := range []struct {
		kode, nama, tipe, sub, normal, psak string
	}{
		{"1-1100", "Kas di Bank", "asset", models.SubCurrentAsset, "debit", models.PsakKasSetaraKas},
		{"4-1000", "Pendapatan Sewa", "revenue", models.SubRevenue, "credit", models.PsakPendapatan},
		{"5-1000", "Beban Operasional", "expense", models.SubExpense, "debit", models.PsakBebanOperasional},
	} {
		var id int64
		err := pool.QueryRow(ctx, `
			INSERT INTO accounts (company_id, code, name, account_type, sub_type, normal_balance, psak_line)
			VALUES ($1, $2, $3, $4::account_type, $5, $6::normal_balance, $7) RETURNING id`,
			company.ID, a.kode, a.nama, a.tipe, a.sub, a.normal, a.psak).Scan(&id)
		if err != nil {
			panic(fmt.Errorf("akun %s: %w", a.kode, err))
		}
		akunID[a.kode] = id
	}

	bank, err := q.CreateBankAccount(ctx, db.CreateBankAccountParams{
		CompanyID: company.ID, AccountID: akunID["1-1100"],
		BankName: "Bank Bukti", AccountNumber: "1234567890",
	})
	if err != nil {
		panic(err)
	}
	cek(bank.ID > 0 && bank.AccountID == akunID["1-1100"],
		"rekening bank terdaftar: Bank Bukti 1234567890 -> akun COA 1-1100 (id %d)", bank.ID)
	_, err = q.CreateBankAccount(ctx, db.CreateBankAccountParams{
		CompanyID: company.ID, AccountID: akunID["1-1100"],
		BankName: "Bank Ganda", AccountNumber: "999",
	})
	if err == nil {
		panic("akun COA yang sama harusnya ditolak UNIQUE (company_id, account_id)")
	}
	cek(true, "UNIQUE (company_id, account_id) menolak dua rekening untuk satu akun COA")

	// ===== 2. Jurnal buku besar =====
	// Saldo buku 1-1100: +5.000.000 (SET-001), -1.200.000 (BEB-001),
	// +2.450.000 (SET-002) = 6.250.000. Mutasi bank mencatat setoran ketiga
	// 2.500.000 sehingga ada selisih 50.000 yang harus masuk jurnal penyesuaian.
	jurnal := func(ref, memo string, tanggal time.Time, debitAkun, kreditAkun string, juta int64) int64 {
		j, err := q.CreateJournalWithEntries(ctx, db.CreateJournalWithEntriesParams{
			CompanyID: company.ID, JournalDate: tgl(tanggal), Reference: ref,
			Memo: memo, CreatedBy: userID,
			AccountIds:   []int64{akunID[debitAkun], akunID[kreditAkun]},
			Descriptions: []string{memo, memo},
			Debits:       []pgtype.Numeric{num(fmt.Sprintf("%d.00", juta)), num("0")},
			Credits:      []pgtype.Numeric{num("0"), num(fmt.Sprintf("%d.00", juta))},
		})
		if err != nil {
			panic(fmt.Errorf("jurnal %s: %w", ref, err))
		}
		return j.ID
	}
	j1 := jurnal("SET-001", "Setoran pendapatan sewa", time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC), "1-1100", "4-1000", 5_000_000)
	j2 := jurnal("BEB-001", "Pembayaran beban operasional", time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC), "5-1000", "1-1100", 1_200_000)
	j3 := jurnal("SET-002", "Setoran pelanggan", time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC), "1-1100", "4-1000", 2_450_000)

	// Baris ledger pada akun kas di bank untuk tiap jurnal (debit utk j1/j3,
	// credit utk j2) yang menjadi kandidat pencocokan.
	barBank := func(jid int64, sisi string) int64 {
		var id int64
		err := pool.QueryRow(ctx, fmt.Sprintf(`
			SELECT id FROM ledger_entries
			WHERE journal_id = $1 AND company_id = $2 AND account_id = $3 AND %s > 0`, sisi),
			jid, company.ID, akunID["1-1100"]).Scan(&id)
		if err != nil {
			panic(err)
		}
		return id
	}
	entryJ1 := barBank(j1, "debit")
	entryJ2 := barBank(j2, "credit")
	entryJ3 := barBank(j3, "debit")

	// ===== 3. Parsing CSV: baris salah ditolak dengan pesan per baris =====
	header := []string{"Tanggal", "Keterangan", "Masuk", "Keluar", "Saldo", "Ref"}
	salah := [][]string{
		{"05/10/2026", "Setoran benar", "2.500.000,00", "", "", "TRX-009"},
		{"06/10/2026", "Dua sisi sekaligus", "1.000.000,00", "500.000,00", "", "TRX-010"},
		{"31/02/2026", "Tanggal tidak ada", "1.000.000,00", "", "", "TRX-011"},
		{"07/10/2026", "Angka rusak", "abc", "", "", "TRX-012"},
	}
	valid, perrs := models.ParseStatementRows(header, salah)
	cek(len(valid) == 1 && len(perrs) == 3 &&
		strings.Contains(perrs[0], "Baris 2") && strings.Contains(perrs[1], "Baris 3") &&
		strings.Contains(perrs[2], "Baris 4"),
		"parsing CSV mutasi: 1 baris valid, 3 baris ditolak dengan pesan per nomor baris")

	// ===== 4. Impor mutasi lewat route HTTP asli =====
	h := handlers.NewWithPool(q, pool, false, "")
	router := chi.NewRouter()
	router.Group(func(r chi.Router) {
		r.Use(appmw.RequireAuth(q, false))
		r.Route("/companies/{companyKey}", func(r chi.Router) {
			r.Use(appmw.RequireCompany(q))
			r.Get("/bank", h.BankGet)
			r.Post("/bank/statements/{statementID}/match", h.StatementMatchPost)
			r.Post("/bank/statements/{statementID}/unmatch", h.StatementUnmatchPost)
			r.Get("/bank/{bankAccountID}", h.BankReconcileGet)
			r.Post("/bank/{bankAccountID}/import", h.BankImportPost)
			r.Post("/bank/{bankAccountID}/import/confirm", h.BankImportConfirm)
			r.Post("/bank/{bankAccountID}/automatch", h.StatementAutomatchPost)
		})
	})

	tokenSesi := "token-bukti-bank-b"
	if _, err := q.CreateSession(ctx, db.CreateSessionParams{
		UserID:    userID,
		TokenHash: auth.HashToken(tokenSesi),
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true, InfinityModifier: pgtype.Finite},
	}); err != nil {
		panic(err)
	}
	do := func(method, path string, body io.Reader, contentType string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, body)
		req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: tokenSesi})
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	csvMutasi := strings.Join([]string{
		"tanggal;keterangan;masuk;keluar;saldo;ref",
		"05/10/2026;Setoran sewa;5.000.000,00;;5.000.000,00;TRX-001",
		"08/10/2026;Beban operasional;;1.200.000,00;3.800.000,00;TRX-002",
		"10/10/2026;Setoran pelanggan;2.500.000,00;;6.300.000,00;TRX-003",
		"",
	}, "\n")
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	fw, err := mw.CreateFormFile("berkas", "mutasi.csv")
	if err != nil {
		panic(err)
	}
	if _, err := fw.Write([]byte(csvMutasi)); err != nil {
		panic(err)
	}
	if err := mw.Close(); err != nil {
		panic(err)
	}
	wImp := do("POST", fmt.Sprintf("/companies/%s/bank/%d/import", company.UrlKey, bank.ID),
		body, mw.FormDataContentType())
	if wImp.Code != http.StatusOK {
		panic(fmt.Sprintf("POST impor mutasi -> HTTP %d: %s", wImp.Code, wImp.Body.String()))
	}
	htmlPreview := wImp.Body.String()
	reTok := regexp.MustCompile(`name="token"[^>]*value="([0-9a-f]{32})"`)
	mTok := reTok.FindStringSubmatch(htmlPreview)
	cek(strings.Contains(htmlPreview, "mutasi.csv") && mTok != nil,
		"pratinjau impor mutasi bank render (HTTP 200, nama berkas + token konfirmasi ada)")

	wConf := do("POST", fmt.Sprintf("/companies/%s/bank/%d/import/confirm", company.UrlKey, bank.ID),
		strings.NewReader("token="+mTok[1]), "application/x-www-form-urlencoded")
	lokasi := fmt.Sprintf("/companies/%s/bank/%d", company.UrlKey, bank.ID)
	if wConf.Code != http.StatusSeeOther || !strings.Contains(wConf.Header().Get("Location"), lokasi) {
		panic(fmt.Sprintf("konfirmasi impor -> HTTP %d", wConf.Code))
	}
	lines, err := q.ListStatementLines(ctx, db.ListStatementLinesParams{
		CompanyID: company.ID, BankAccountID: bank.ID,
	})
	if err != nil {
		panic(err)
	}
	cek(len(lines) == 3, "konfirmasi impor menyimpan 3 baris mutasi (masuk 5 jt, keluar 1,2 jt, masuk 2,5 jt)")
	barRef := map[string]db.BankStatementLine{}
	for _, l := range lines {
		barRef[l.Reference] = l
		cek(l.Status == "unmatched" && l.SourceFile == "mutasi.csv",
			"baris mutasi %s tersimpan unmatched, sumber %s", l.Reference, l.SourceFile)
	}
	cek(models.NumericToCents(barRef["TRX-001"].AmountIn) == rp(5_000_000) &&
		models.NumericToCents(barRef["TRX-002"].AmountOut) == rp(1_200_000) &&
		models.NumericToCents(barRef["TRX-003"].AmountIn) == rp(2_500_000),
		"angka masuk/keluar tersimpan sesuai CSV (format Indonesia 5.000.000,00)")

	// ===== 5. Pencocokan otomatis: 2 baris persis, 1 belum =====
	wAuto := do("POST", fmt.Sprintf("/companies/%s/bank/%d/automatch", company.UrlKey, bank.ID), nil, "")
	if wAuto.Code != http.StatusSeeOther {
		panic(fmt.Sprintf("automatch -> HTTP %d", wAuto.Code))
	}
	lines, err = q.ListStatementLines(ctx, db.ListStatementLinesParams{
		CompanyID: company.ID, BankAccountID: bank.ID,
	})
	if err != nil {
		panic(err)
	}
	for _, l := range lines {
		barRef[l.Reference] = l
	}
	cek(barRef["TRX-001"].Status == "matched" && barRef["TRX-001"].MatchedEntryID.Int64 == entryJ1 &&
		barRef["TRX-002"].Status == "matched" && barRef["TRX-002"].MatchedEntryID.Int64 == entryJ2,
		"pencocokan otomatis menutup TRX-001 dan TRX-002 (tanggal DAN jumlah persis sama)")
	cek(barRef["TRX-003"].Status == "unmatched" && !barRef["TRX-003"].MatchedEntryID.Valid,
		"TRX-003 (selisih 50.000) tetap belum cocok setelah pencocokan otomatis")

	// ===== 6. Match dengan selisih via handler asli -> jurnal REC- =====
	wMatch := do("POST",
		fmt.Sprintf("/companies/%s/bank/statements/%d/match", company.UrlKey, barRef["TRX-003"].ID),
		strings.NewReader(fmt.Sprintf("entry_id=%d", entryJ3)), "application/x-www-form-urlencoded")
	if wMatch.Code != http.StatusSeeOther {
		panic(fmt.Sprintf("match dengan selisih -> HTTP %d: %s", wMatch.Code, wMatch.Body.String()))
	}
	line3, err := q.GetStatementLineByID(ctx, db.GetStatementLineByIDParams{
		ID: barRef["TRX-003"].ID, CompanyID: company.ID,
	})
	if err != nil {
		panic(err)
	}
	cek(line3.Status == "adjusted" && line3.MatchedEntryID.Int64 == entryJ3 && line3.MatchedJournalID.Valid,
		"TRX-003 dicocokkan dengan selisih: status adjusted, terikat jurnal penyesuaian")
	jRec, err := q.GetJournalByID(ctx, db.GetJournalByIDParams{
		ID: line3.MatchedJournalID.Int64, CompanyID: company.ID,
	})
	if err != nil {
		panic(err)
	}
	selisihAkun, err := q.GetAccountBySystemRole(ctx, db.GetAccountBySystemRoleParams{
		CompanyID: company.ID, SystemRole: "selisih_kurs",
	})
	if err != nil {
		panic(err)
	}
	var barRec []db.LedgerEntry
	rowsRec, err := pool.Query(ctx,
		`SELECT id, company_id, journal_id, account_id, description, debit, credit
		 FROM ledger_entries WHERE journal_id = $1 AND company_id = $2 ORDER BY id`,
		jRec.ID, company.ID)
	if err != nil {
		panic(err)
	}
	for rowsRec.Next() {
		var e db.LedgerEntry
		if err := rowsRec.Scan(&e.ID, &e.CompanyID, &e.JournalID, &e.AccountID,
			&e.Description, &e.Debit, &e.Credit); err != nil {
			panic(err)
		}
		barRec = append(barRec, e)
	}
	rowsRec.Close()
	cek(jRec.Reference == fmt.Sprintf("REC-%d", line3.ID) && jRec.IsAdjusting && len(barRec) == 2 &&
		barRec[0].AccountID == akunID["1-1100"] && models.NumericToCents(barRec[0].Debit) == rp(50_000) &&
		barRec[1].AccountID == selisihAkun.ID && models.NumericToCents(barRec[1].Credit) == rp(50_000),
		"jurnal selisih otomatis REC-%d: debit kas di bank 50.000, kredit Selisih Nilai Tukar (akun %s)",
		line3.ID, selisihAkun.Code)

	// ===== 7. Tie-out saldo buku = mutasi bank + ledger seimbang =====
	var bookNum pgtype.Numeric
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(le.debit - le.credit), 0)
		FROM ledger_entries le
		JOIN journals j ON j.id = le.journal_id AND j.company_id = le.company_id
		WHERE le.company_id = $1 AND le.account_id = $2 AND j.status = 'posted'`,
		company.ID, akunID["1-1100"]).Scan(&bookNum); err != nil {
		panic(err)
	}
	// lines sudah memuat semua baris mutasi tersimpan, termasuk TRX-003 yang
	// kemudian di-match ulang (amount_in/out tidak berubah saat match).
	var bankTotal int64
	for _, l := range lines {
		bankTotal += models.NumericToCents(l.AmountIn) - models.NumericToCents(l.AmountOut)
	}
	cek(models.NumericToCents(bookNum) == rp(6_300_000) && bankTotal == rp(6_300_000),
		"saldo buku 1-1100 = total mutasi bank = %s (selisih kurs 50.000 sudah masuk buku)",
		models.FormatRounded(rp(6_300_000), company.RoundingUnit))
	var sumDebit, sumKredit pgtype.Numeric
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(debit), 0), COALESCE(SUM(credit), 0) FROM ledger_entries WHERE company_id = $1`,
		company.ID).Scan(&sumDebit, &sumKredit); err != nil {
		panic(err)
	}
	cek(models.NumericToCents(sumDebit) == models.NumericToCents(sumKredit),
		"ledger double-entry perusahaan tetap seimbang: debit %s = kredit %s",
		models.FormatRounded(models.NumericToCents(sumDebit), company.RoundingUnit),
		models.FormatRounded(models.NumericToCents(sumKredit), company.RoundingUnit))

	// ===== 8. Unmatch lalu match ulang =====
	wUn := do("POST",
		fmt.Sprintf("/companies/%s/bank/statements/%d/unmatch", company.UrlKey, barRef["TRX-002"].ID),
		nil, "")
	if wUn.Code != http.StatusSeeOther {
		panic(fmt.Sprintf("unmatch -> HTTP %d", wUn.Code))
	}
	line2, err := q.GetStatementLineByID(ctx, db.GetStatementLineByIDParams{
		ID: barRef["TRX-002"].ID, CompanyID: company.ID,
	})
	if err != nil {
		panic(err)
	}
	cek(line2.Status == "unmatched" && !line2.MatchedEntryID.Valid,
		"unmatch melepas TRX-002: kembali belum cocok, kandidat bisa dipakai lagi")
	wAuto2 := do("POST", fmt.Sprintf("/companies/%s/bank/%d/automatch", company.UrlKey, bank.ID), nil, "")
	if wAuto2.Code != http.StatusSeeOther {
		panic(fmt.Sprintf("automatch ulang -> HTTP %d", wAuto2.Code))
	}
	line2, err = q.GetStatementLineByID(ctx, db.GetStatementLineByIDParams{
		ID: barRef["TRX-002"].ID, CompanyID: company.ID,
	})
	if err != nil {
		panic(err)
	}
	cek(line2.Status == "matched" && line2.MatchedEntryID.Int64 == entryJ2,
		"TRX-002 bisa dicocokkan ulang setelah unmatch")

	// ===== 9. Render halaman daftar bank + rekonsiliasi =====
	wList := do("GET", "/companies/"+company.UrlKey+"/bank", nil, "")
	if wList.Code != http.StatusOK {
		panic(fmt.Sprintf("GET /bank -> HTTP %d", wList.Code))
	}
	cek(strings.Contains(wList.Body.String(), "Bank Bukti") &&
		strings.Contains(wList.Body.String(), "1234567890"),
		"GET /companies/{key}/bank render daftar rekening (HTTP 200, nama + nomor rekening ada)")

	uang := func(c int64) string { return models.FormatRounded(c, company.RoundingUnit) }
	wRecon := do("GET", fmt.Sprintf("/companies/%s/bank/%d", company.UrlKey, bank.ID), nil, "")
	if wRecon.Code != http.StatusOK {
		panic(fmt.Sprintf("GET rekonsiliasi -> HTTP %d", wRecon.Code))
	}
	html := wRecon.Body.String()
	cek(strings.Contains(html, "Saldo menurut buku") &&
		strings.Contains(html, uang(rp(6_300_000))) &&
		strings.Contains(html, "Selisih buku vs mutasi") &&
		strings.Contains(html, "Cocok + selisih") &&
		strings.Contains(html, fmt.Sprintf("REC-%d", line3.ID)),
		"GET /companies/{key}/bank/{id} render rekonsiliasi (HTTP 200, saldo 6.300.000, badge Cocok + selisih, REC- ada)")

	// ===== 10. FK komposit menolak baris mutasi lintas perusahaan =====
	var companyAsing int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO companies (name, slug, url_key) VALUES ('PT Bank Asing', 'pt-bank-asing', 'buktibankasing') RETURNING id`,
	).Scan(&companyAsing); err != nil {
		panic(err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO bank_statement_lines (company_id, bank_account_id, line_date, description, amount_in)
		VALUES ($1, $2, $3, 'lintas perusahaan', 100)`,
		companyAsing, bank.ID, tgl(time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)))
	if err == nil {
		panic("baris mutasi lintas perusahaan harusnya ditolak FK komposit")
	}
	cek(true, "FK komposit bank_statement_lines.bank_account_id menolak rekening lintas perusahaan")

	// Simpan HTML bila diminta, sebagai bukti berkas.
	if dir := os.Getenv("SMOKE_SAVE_DIR"); dir != "" {
		if err := os.WriteFile(dir+"/bukti-bank-rekonsiliasi.html", []byte(html), 0o644); err != nil {
			panic(err)
		}
		fmt.Println("PASS HTML bukti rekonsiliasi bank disimpan di", dir)
	}
}

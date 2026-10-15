// Bukti perbaikan 3 sisa temuan Stage 3 (backfill flag otomatis, UI input
// party_id + due_date, filter party_type di subledger). Satu perusahaan uji
// dipakai untuk ketiganya:
//
//  1. akun piutang/hutang BARU dibuat lewat POST /accounts (jalur insert asli,
//     lengkap dengan CSRF + middleware), lalu penanda is_receivable/is_payable
//     dibaca kembali dari database;
//  2. form jurnal baru dirender dan di-POST dengan pihak + tanggal jatuh
//     tempo, lalu nilai di kolom journals.party_id/due_date dibaca kembali
//     dan ditampilkan di halaman detail jurnal;
//  3. jurnal dengan pihak bertipe salah (supplier di subledger piutang,
//     pelanggan di subledger hutang) tidak boleh muncul atas nama pihak
//     tersebut di subledger lawan tipenya, sementara jumlah subledger tetap
//     sama dengan saldo buku besar.
//
// Set SMOKE_SAVE_DIR untuk menyimpan HTML hasil render sebagai bukti berkas.
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
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

// evidenceSisa3 menjalankan bukti ketiga perbaikan sisa temuan Stage 3.
func evidenceSisa3(ctx context.Context, pool *pgxpool.Pool, q *db.Queries, userID int64) {
	// ===== 1. Perusahaan + pihak (pelanggan & supplier) =====
	company, err := q.CreateCompany(ctx, db.CreateCompanyParams{
		Name: "PT Bukti Sisa Tiga", Slug: "pt-bukti-sisa-tiga",
		BaseCurrency: "IDR", UrlKey: "bukti3sisa",
	})
	if err != nil {
		panic(err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO company_users (company_id, user_id, role) VALUES ($1, $2, 'owner')`,
		company.ID, userID); err != nil {
		panic(err)
	}

	pihak := func(pType, name string) int64 {
		var id int64
		err := pool.QueryRow(ctx,
			`INSERT INTO parties (company_id, party_type, name) VALUES ($1, $2::party_type, $3) RETURNING id`,
			company.ID, pType, name).Scan(&id)
		if err != nil {
			panic(fmt.Errorf("pihak %s: %w", name, err))
		}
		return id
	}
	pCust := pihak("customer", "CV Mekar")
	pSup := pihak("supplier", "PT Sumber Niaga")

	// Router dengan middleware asli seperti cmd/server: CSRF + auth + cakupan
	// perusahaan + role, supaya bukti memakai jalur HTTP yang sama dengan
	// aplikasi hidup.
	h := handlers.New(q, false, "")
	router := chi.NewRouter()
	router.Use(appmw.CSRFProtect)
	router.Group(func(r chi.Router) {
		r.Use(appmw.RequireAuth(q, false))
		r.Route("/companies/{companyKey}", func(r chi.Router) {
			r.Use(appmw.RequireCompany(q))
			r.Group(func(r chi.Router) {
				r.Use(appmw.RequireRole(appmw.RoleAdmin))
				r.Post("/accounts", h.AccountCreatePost)
			})
			r.Group(func(r chi.Router) {
				r.Use(appmw.RequireRole(appmw.RoleStaff))
				r.Get("/journals/new", h.JournalNewGet)
				r.Post("/journals", h.JournalCreatePost)
			})
			r.Get("/journals/{journalID}", h.JournalDetailGet)
			r.Get("/receivables", h.ReceivablesGet)
			r.Get("/payables", h.PayablesGet)
		})
	})

	token := "token-bukti-sisa-3"
	if _, err := q.CreateSession(ctx, db.CreateSessionParams{
		UserID:    userID,
		TokenHash: auth.HashToken(token),
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true, InfinityModifier: pgtype.Finite},
	}); err != nil {
		panic(err)
	}
	cookie := &http.Cookie{Name: auth.SessionCookieName, Value: token}
	base := "/companies/" + company.UrlKey
	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	post := func(path string, form url.Values) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		// CSRFProtect membandingkan host Origin dengan host request.
		req.Header.Set("Origin", "http://"+req.Host)
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	// ===== 2. Item 1: akun BARU lewat jalur insert langsung bertanda =====
	akunBaru := func(code, name, accType, subType, normal string) int64 {
		w := post(base+"/accounts", url.Values{
			"code": {code}, "name": {name}, "account_type": {accType},
			"sub_type": {subType}, "normal_balance": {normal},
		})
		if w.Code != http.StatusSeeOther {
			panic(fmt.Sprintf("POST /accounts %s -> HTTP %d: %s", code, w.Code, w.Body.String()))
		}
		var id int64
		if err := pool.QueryRow(ctx,
			`SELECT id FROM accounts WHERE company_id = $1 AND code = $2`,
			company.ID, code).Scan(&id); err != nil {
			panic(fmt.Errorf("akun %s tidak tercatat: %w", code, err))
		}
		return id
	}
	akunPiutang := akunBaru("1-2100", "Piutang Usaha", "asset", models.SubCurrentAsset, "debit")
	akunUtang := akunBaru("2-2100", "Utang Usaha", "liability", models.SubCurrentLiability, "credit")
	akunPendapatan := akunBaru("4-2100", "Pendapatan Jasa", "revenue", models.SubRevenue, "credit")
	akunBeban := akunBaru("5-2100", "Beban Pemeliharaan", "expense", models.SubExpense, "debit")

	flagAkun := func(id int64) (isRecv, isPay bool, psak string) {
		if err := pool.QueryRow(ctx,
			`SELECT is_receivable, is_payable, psak_line FROM accounts WHERE id = $1`,
			id).Scan(&isRecv, &isPay, &psak); err != nil {
			panic(err)
		}
		return isRecv, isPay, psak
	}
	recv, pay, psak := flagAkun(akunPiutang)
	cek(recv && !pay && psak == models.PsakPiutang,
		"akun baru 'Piutang Usaha' otomatis is_receivable=%v is_payable=%v psak_line=%q (tanpa migrasi)", recv, pay, psak)
	recv, pay, psak = flagAkun(akunUtang)
	cek(!recv && pay && psak == models.PsakUtangUsaha,
		"akun baru 'Utang Usaha' otomatis is_receivable=%v is_payable=%v psak_line=%q (tanpa migrasi)", recv, pay, psak)
	recv, pay, psak = flagAkun(akunPendapatan)
	cek(!recv && !pay, "akun baru 'Pendapatan Jasa' tidak bertanda subledger (is_receivable=%v is_payable=%v)", recv, pay)
	recv, pay, _ = flagAkun(akunBeban)
	cek(!recv && !pay, "akun baru 'Beban Pemeliharaan' tidak bertanda subledger (is_receivable=%v is_payable=%v)", recv, pay)

	// ===== 3. Item 2: form jurnal bisa memilih pihak + jatuh tempo =====
	formBaru := get(base + "/journals/new")
	if formBaru.Code != http.StatusOK {
		panic(fmt.Sprintf("GET /journals/new -> HTTP %d", formBaru.Code))
	}
	htmlForm := formBaru.Body.String()
	cek(strings.Contains(htmlForm, `name="party_id"`) && strings.Contains(htmlForm, `name="due_date"`),
		"form jurnal baru memuat input pihak (name=\"party_id\") dan jatuh tempo (name=\"due_date\")")
	cek(strings.Contains(htmlForm, "CV Mekar") && strings.Contains(htmlForm, "PT Sumber Niaga"),
		"dropdown pihak memuat pelanggan 'CV Mekar' dan supplier 'PT Sumber Niaga' dari tabel parties")

	idStr := func(id int64) string { return strconv.FormatInt(id, 10) }
	buatJurnal := func(ref, memo, party string, due string, debitAkun, kreditAkun int64, juta string) int64 {
		w := post(base+"/journals", url.Values{
			"journal_date": {"2026-10-01"},
			"reference":    {ref},
			"memo":         {memo},
			"party_id":     {party},
			"due_date":     {due},
			"account_id":   {idStr(debitAkun), idStr(kreditAkun)},
			"description":  {memo, memo},
			"debit":        {juta, ""},
			"credit":       {"", juta},
		})
		if w.Code != http.StatusSeeOther {
			panic(fmt.Sprintf("POST /journals %s -> HTTP %d: %s", ref, w.Code, w.Body.String()))
		}
		loc := w.Header().Get("Location")
		id, err := strconv.ParseInt(loc[strings.LastIndex(loc, "/")+1:], 10, 64)
		if err != nil {
			panic(fmt.Errorf("redirect %s tidak memuat id jurnal: %q", ref, loc))
		}
		return id
	}
	inv1 := buatJurnal("INV-SISA-1", "Sewa kredit pelanggan", idStr(pCust), "31/12/2026", akunPiutang, akunPendapatan, "2000000")

	var gotParty pgtype.Int8
	var gotDue pgtype.Date
	if err := pool.QueryRow(ctx,
		`SELECT party_id, due_date FROM journals WHERE id = $1 AND company_id = $2`,
		inv1, company.ID).Scan(&gotParty, &gotDue); err != nil {
		panic(err)
	}
	cek(gotParty.Valid && gotParty.Int64 == pCust && gotDue.Valid &&
		gotDue.Time.Equal(time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)),
		"jurnal INV-SISA-1 tersimpan di journals.party_id=%d dan journals.due_date=%s",
		gotParty.Int64, gotDue.Time.Format("02-01-2006"))

	detail := get(base + "/journals/" + idStr(inv1))
	if detail.Code != http.StatusOK {
		panic(fmt.Sprintf("GET /journals/%d -> HTTP %d", inv1, detail.Code))
	}
	htmlDetail := detail.Body.String()
	cek(strings.Contains(htmlDetail, "CV Mekar") && strings.Contains(htmlDetail, "31-12-2026"),
		"halaman detail jurnal menampilkan pihak 'CV Mekar' dan jatuh tempo 31-12-2026")

	// ===== 4. Item 3: pihak bertipe salah tidak tampil di subledger lawan =====
	// INV-SISA-2: jurnal piutang tetapi pihaknya supplier (salah tipe).
	inv2 := buatJurnal("INV-SISA-2", "Sewa kredit pihak salah tipe", idStr(pSup), "31/12/2026", akunPiutang, akunPendapatan, "500000")
	// BILL-SISA-1: jurnal hutang dengan pihak supplier (benar).
	bill1 := buatJurnal("BILL-SISA-1", "Tagihan pemeliharaan", idStr(pSup), "31/12/2026", akunBeban, akunUtang, "1000000")
	// BILL-SISA-2: jurnal hutang tetapi pihaknya pelanggan (salah tipe).
	bill2 := buatJurnal("BILL-SISA-2", "Tagihan pihak salah tipe", idStr(pCust), "31/12/2026", akunBeban, akunUtang, "300000")
	_, _, _ = inv2, bill1, bill2

	asOf := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	movsRecv, err := q.ListReceivableMovements(ctx, db.ListReceivableMovementsParams{CompanyID: company.ID, AsOf: tgl(asOf)})
	if err != nil {
		panic(err)
	}
	movsPay, err := q.ListPayableMovements(ctx, db.ListPayableMovementsParams{CompanyID: company.ID, AsOf: tgl(asOf)})
	if err != nil {
		panic(err)
	}

	gerakan := func(movs []db.ListReceivableMovementsRow) []barisMutasi {
		var out []barisMutasi
		for _, m := range movs {
			out = append(out, barisMutasi{m.Reference, m.PartyID, m.PartyName})
		}
		return out
	}
	cekSubledger := func(nama string, movs []barisMutasi, pihakSalah int64, refSalah string) {
		adaSalah, adaRef := false, false
		for _, m := range movs {
			if m.party == pihakSalah {
				adaSalah = true
			}
			if m.ref == refSalah {
				adaRef = true
				cek(m.party == models.PartyIDTanpaPihak && m.nama == "",
					"%s: jurnal %s (pihak bertipe salah) diperlakukan sebagai jurnal tanpa pihak (party_id=%d, nama=%q)",
					nama, refSalah, m.party, m.nama)
			}
		}
		cek(!adaSalah, "%s: tidak ada baris yang berpihak ke pihak bertipe salah (id %d)", nama, pihakSalah)
		cek(adaRef, "%s: jurnal %s tetap terhitung di subledger", nama, refSalah)
	}
	cekSubledger("subledger piutang", gerakan(movsRecv), pSup, "INV-SISA-2")
	cekSubledger("subledger hutang", gerakanPay(movsPay), pCust, "BILL-SISA-2")

	// Tie-out: jumlah subledger = saldo akun bertanda di buku besar.
	itemsRecv := make([]models.ReceivableItem, 0, len(movsRecv))
	for _, m := range movsRecv {
		itemsRecv = append(itemsRecv, models.ReceivableItem{
			JournalID: m.JournalID, Reference: m.Reference,
			PartyID: m.PartyID, PartyName: m.PartyName,
			JournalDate: m.JournalDate.Time, DueDate: m.DueDate.Time, HasDueDate: m.DueDate.Valid,
			AmountCents: models.NumericToCents(m.TotalDebit) - models.NumericToCents(m.TotalCredit),
		})
	}
	agingRecv := models.BuildReceivableAging(itemsRecv, asOf)
	itemsPay := make([]models.PayableItem, 0, len(movsPay))
	for _, m := range movsPay {
		itemsPay = append(itemsPay, models.PayableItem{
			JournalID: m.JournalID, Reference: m.Reference,
			PartyID: m.PartyID, PartyName: m.PartyName,
			JournalDate: m.JournalDate.Time, DueDate: m.DueDate.Time, HasDueDate: m.DueDate.Valid,
			AmountCents: models.NumericToCents(m.TotalCredit) - models.NumericToCents(m.TotalDebit),
		})
	}
	agingPay := models.BuildPayableAging(itemsPay, asOf)

	var glRecv, glPay pgtype.Numeric
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(le.debit - le.credit), 0)
		FROM ledger_entries le
		JOIN journals j ON j.id = le.journal_id AND j.company_id = le.company_id
		JOIN accounts a ON a.id = le.account_id AND a.company_id = le.company_id
		WHERE le.company_id = $1 AND a.is_receivable
		  AND j.status = 'posted' AND j.journal_date <= $2`,
		company.ID, tgl(asOf)).Scan(&glRecv); err != nil {
		panic(err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(le.credit - le.debit), 0)
		FROM ledger_entries le
		JOIN journals j ON j.id = le.journal_id AND j.company_id = le.company_id
		JOIN accounts a ON a.id = le.account_id AND a.company_id = le.company_id
		WHERE le.company_id = $1 AND a.is_payable
		  AND j.status = 'posted' AND j.journal_date <= $2`,
		company.ID, tgl(asOf)).Scan(&glPay); err != nil {
		panic(err)
	}
	uang := func(cents int64) string { return models.FormatRounded(cents, company.RoundingUnit) }
	cek(agingRecv.Total.Total == models.NumericToCents(glRecv) && agingRecv.Total.Total == rp(2_500_000),
		"jumlah subledger piutang %s == saldo buku besar %s (jurnal pihak salah tipe tetap terhitung)",
		uang(agingRecv.Total.Total), uang(models.NumericToCents(glRecv)))
	cek(agingPay.Total.Total == models.NumericToCents(glPay) && agingPay.Total.Total == rp(1_300_000),
		"jumlah subledger hutang %s == saldo buku besar %s (jurnal pihak salah tipe tetap terhitung)",
		uang(agingPay.Total.Total), uang(models.NumericToCents(glPay)))

	// ===== 5. Angka + nama pihak yang benar-benar tampil di HTML =====
	cekBaris := func(html, needle string, want ...string) {
		got := rowNumCells(html, needle)
		if len(got) != len(want) {
			panic(fmt.Sprintf("baris %q harus %d sel angka, dapat %d: %v", needle, len(want), len(got), got))
		}
		for i := range want {
			if got[i] != want[i] {
				panic(fmt.Sprintf("baris %q sel %d = %q, mau %q (semua: %v)", needle, i, got[i], want[i], got))
			}
		}
		fmt.Println("PASS baris HTML", needle, "=", strings.Join(got, " | "))
	}

	recvPage := get(base + "/receivables?to=15/10/2026")
	if recvPage.Code != http.StatusOK {
		panic(fmt.Sprintf("GET /receivables -> HTTP %d", recvPage.Code))
	}
	htmlRecv := recvPage.Body.String()
	cek(strings.Contains(htmlRecv, "Saldo Piutang per Pelanggan"),
		"GET /companies/{companyKey}/receivables render (HTTP 200, judul tabel ada)")
	cekBaris(htmlRecv, "CV Mekar", uang(rp(2_000_000)), "", "", "", "", uang(rp(2_000_000)))
	cekBaris(htmlRecv, "Tanpa Pelanggan", uang(rp(500_000)), "", "", "", "", uang(rp(500_000)))
	cekBaris(htmlRecv, "Jumlah Piutang", uang(rp(2_500_000)), "", "", "", "", uang(rp(2_500_000)))
	cek(!strings.Contains(htmlRecv, "PT Sumber Niaga"),
		"/receivables tidak memuat nama supplier 'PT Sumber Niaga' (filter party_type = customer)")

	payPage := get(base + "/payables?to=15/10/2026")
	if payPage.Code != http.StatusOK {
		panic(fmt.Sprintf("GET /payables -> HTTP %d", payPage.Code))
	}
	htmlPay := payPage.Body.String()
	cek(strings.Contains(htmlPay, "Saldo Hutang per Supplier"),
		"GET /companies/{companyKey}/payables render (HTTP 200, judul tabel ada)")
	cekBaris(htmlPay, "PT Sumber Niaga", uang(rp(1_000_000)), "", "", "", "", uang(rp(1_000_000)))
	cekBaris(htmlPay, "Tanpa Supplier", uang(rp(300_000)), "", "", "", "", uang(rp(300_000)))
	cekBaris(htmlPay, "Jumlah Hutang", uang(rp(1_300_000)), "", "", "", "", uang(rp(1_300_000)))
	cek(!strings.Contains(htmlPay, "CV Mekar"),
		"/payables tidak memuat nama pelanggan 'CV Mekar' (filter party_type = supplier)")

	// Simpan HTML bila diminta, sebagai bukti berkas.
	if dir := os.Getenv("SMOKE_SAVE_DIR"); dir != "" {
		if err := os.WriteFile(filepath.Join(dir, "bukti-sisa-3-jurnal.html"), []byte(htmlForm), 0o644); err != nil {
			panic(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "bukti-sisa-3-piutang.html"), []byte(htmlRecv), 0o644); err != nil {
			panic(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "bukti-sisa-3-hutang.html"), []byte(htmlPay), 0o644); err != nil {
			panic(err)
		}
		fmt.Println("PASS HTML bukti sisa 3 disimpan di", dir)
	}
}

// barisMutasi adalah satu baris hasil query mutasi subledger (piutang atau
// hutang) yang sudah dipetakan ke bentuk untuk pemeriksaan bersama.
type barisMutasi struct {
	ref   string
	party int64
	nama  string
}

// gerakanPay memetakan baris mutasi hutang ke bentuk yang sama dengan
// gerakan piutang untuk pemeriksaan bersama.
func gerakanPay(movs []db.ListPayableMovementsRow) []barisMutasi {
	out := make([]barisMutasi, 0, len(movs))
	for _, m := range movs {
		out = append(out, barisMutasi{m.Reference, m.PartyID, m.PartyName})
	}
	return out
}

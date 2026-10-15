// Bukti runtime subledger hutang usaha + aging (Stage 3b). Satu perusahaan
// uji disi dengan supplier, akun hutang bertanda is_payable, jurnal tagihan
// ber-jatuh tempo beda umur, dan satu pembayaran sebagian, lalu:
//
//  1. aging dihitung lewat query dan model yang sama dengan handler lalu
//     dibandingkan ke harapan per kolom (belum jatuh tempo, 0-30, 31-60,
//     61-90, 90+ hari), termasuk arah saldo (kredit - debit);
//  2. route /payables dirender lewat router dengan middleware asli
//     (RequireAuth + RequireCompany);
//  3. angka yang benar-benar muncul di HTML dibaca kembali per supplier;
//  4. jumlah subledger diuji sama dengan saldo akun hutang usaha di buku
//     besar (kredit - debit).
//
// Set SMOKE_SAVE_DIR untuk menyimpan HTML hasil render sebagai bukti berkas.
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// evidencePayables menjalankan seluruh bukti subledger hutang usaha.
func evidencePayables(ctx context.Context, pool *pgxpool.Pool, q *db.Queries, userID int64) {
	// ===== 1. Perusahaan, akun, pihak =====
	company, err := q.CreateCompany(ctx, db.CreateCompanyParams{
		Name: "PT Bukti Hutang", Slug: "pt-bukti-hutang",
		BaseCurrency: "IDR", UrlKey: "bukti3bhutang",
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
	for _, a := range []akunUji{
		{"1-1000", "Kas", "asset", models.SubCurrentAsset, "debit", models.PsakKasSetaraKas},
		{"2-2000", "Hutang Usaha", "liability", models.SubCurrentLiability, "credit", models.PsakUtangUsaha},
		{"5-1000", "Beban Pemeliharaan", "expense", models.SubExpense, "debit", models.PsakBebanOperasional},
	} {
		var id int64
		err := pool.QueryRow(ctx, `
			INSERT INTO accounts (company_id, code, name, account_type, sub_type, normal_balance, psak_line, is_payable)
			VALUES ($1, $2, $3, $4::account_type, $5, $6::normal_balance, $7, $8) RETURNING id`,
			company.ID, a.code, a.name, a.tipe, a.sub, a.normal, a.psak,
			a.code == "2-2000").Scan(&id)
		if err != nil {
			panic(fmt.Errorf("akun %s: %w", a.code, err))
		}
		akunID[a.code] = id
	}

	pihak := func(pType, name, code string) int64 {
		var id int64
		err := pool.QueryRow(ctx, `
			INSERT INTO parties (company_id, party_type, name, code)
			VALUES ($1, $2::party_type, $3, $4) RETURNING id`,
			company.ID, pType, name, code).Scan(&id)
		if err != nil {
			panic(fmt.Errorf("pihak %s: %w", name, err))
		}
		return id
	}
	pSumber := pihak("supplier", "PT Sumber Jaya", "S-001")
	pMitra := pihak("supplier", "CV Mitra", "S-002")
	_ = pihak("customer", "PT Sewa Uji", "C-901") // tabel partai campuran pelanggan+supplier

	// ===== 2. Jurnal tagihan supplier + pembayaran =====
	// Arah: tagihan = debit beban, kredit hutang; bayar = debit hutang,
	// kredit kas. Saldo hutang (kredit - debit) positif bila masih berhutang.
	// Harapan aging per tanggal laporan 15/10/2026:
	//   PT Sumber Jaya: BILL-S1 4 jt jatuh tempo 31/08 (45 hari) -> 31-60,
	//                   BILL-S2 2,5 jt jatuh tempo 10/10 (5 hari) -> 0-30,
	//                   PAY-S1 bayar 1 jt (FIFO menutup BILL-S1) -> sisa 3 jt.
	//   CV Mitra      : BILL-M1 3 jt jatuh tempo 31/05 (137 hari) -> 90+,
	//                   BILL-M2 1,5 jt jatuh tempo 30/11          -> belum jatuh tempo.
	//   Tanpa pihak   : BILL-Z 0,8 jt jatuh tempo 25/07 (82 hari) -> 61-90.
	jurnal := func(ref, memo string, tanggal time.Time, debitAkun, kreditAkun string, rupiah int64) int64 {
		// Kolom NUMERIC berisi Rupiah utuh; sen hanya dipakai model laporan.
		j, err := q.CreateJournalWithEntries(ctx, db.CreateJournalWithEntriesParams{
			CompanyID: company.ID, JournalDate: tgl(tanggal), Reference: ref,
			Memo: memo, CreatedBy: userID,
			AccountIds:   []int64{akunID[debitAkun], akunID[kreditAkun]},
			Descriptions: []string{memo, memo},
			Debits:       []pgtype.Numeric{num(fmt.Sprintf("%d.00", rupiah)), num("0")},
			Credits:      []pgtype.Numeric{num("0"), num(fmt.Sprintf("%d.00", rupiah))},
		})
		if err != nil {
			panic(fmt.Errorf("jurnal %s: %w", ref, err))
		}
		return j.ID
	}
	aturPihak := func(jID, partyID int64, due time.Time) {
		_, err := pool.Exec(ctx,
			`UPDATE journals SET party_id = $1, due_date = $2 WHERE id = $3 AND company_id = $4`,
			partyID, tgl(due), jID, company.ID)
		if err != nil {
			panic(fmt.Errorf("set pihak jurnal %d: %w", jID, err))
		}
	}

	billS1 := jurnal("BILL-S1", "Tagihan pemeliharaan Sumber Jaya", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), "5-1000", "2-2000", 4_000_000)
	aturPihak(billS1, pSumber, time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC))
	billS2 := jurnal("BILL-S2", "Tagihan pemeliharaan Sumber Jaya", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), "5-1000", "2-2000", 2_500_000)
	aturPihak(billS2, pSumber, time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC))
	payS1 := jurnal("PAY-S1", "Pembayaran sebagian Sumber Jaya", time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC), "2-2000", "1-1000", 1_000_000)
	aturPihak(payS1, pSumber, time.Time{})

	billM1 := jurnal("BILL-M1", "Tagihan jasa Mitra", time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), "5-1000", "2-2000", 3_000_000)
	aturPihak(billM1, pMitra, time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC))
	billM2 := jurnal("BILL-M2", "Tagihan jasa Mitra", time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC), "5-1000", "2-2000", 1_500_000)
	aturPihak(billM2, pMitra, time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC))

	billZ := jurnal("BILL-Z", "Tagihan tanpa supplier", time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), "5-1000", "2-2000", 800_000)
	if _, err := pool.Exec(ctx,
		`UPDATE journals SET due_date = $1 WHERE id = $2 AND company_id = $3`,
		tgl(time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)), billZ, company.ID); err != nil {
		panic(err)
	}
	fmt.Println("PASS seed bukti hutang: 3 akun, 3 pihak (2 supplier + 1 pelanggan), 6 jurnal")

	// ===== 3. Aging via query + model yang sama dengan handler =====
	asOf := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	movs, err := q.ListPayableMovements(ctx, db.ListPayableMovementsParams{
		CompanyID: company.ID, AsOf: tgl(asOf),
	})
	if err != nil {
		panic(err)
	}
	items := make([]models.PayableItem, 0, len(movs))
	for _, m := range movs {
		items = append(items, models.PayableItem{
			JournalID: m.JournalID, Reference: m.Reference,
			PartyID: m.PartyID, PartyName: m.PartyName,
			JournalDate: m.JournalDate.Time, DueDate: m.DueDate.Time, HasDueDate: m.DueDate.Valid,
			// Saldo hutang = kebalikan arah debit/kredit dari piutang.
			AmountCents: models.NumericToCents(m.TotalCredit) - models.NumericToCents(m.TotalDebit),
		})
	}

	// Arah saldo: tagihan positif, pembayaran negatif.
	for _, it := range items {
		switch it.Reference {
		case "PAY-S1":
			cek(it.AmountCents == -rp(1_000_000),
				"arah saldo PAY-S1: pembayaran = %d sen (negatif)", it.AmountCents)
		case "BILL-S1":
			cek(it.AmountCents == rp(4_000_000),
				"arah saldo BILL-S1: tagihan = %d sen (positif)", it.AmountCents)
		}
	}

	aging := models.BuildPayableAging(items, asOf)
	uang := func(cents int64) string { return models.FormatRounded(cents, company.RoundingUnit) }

	barisPihak := func(partyID int64) models.PayableRow {
		for _, r := range aging.Rows {
			if r.PartyID == partyID {
				return r
			}
		}
		panic(fmt.Errorf("baris pihak %d tidak ada", partyID))
	}
	s := barisPihak(pSumber)
	cek(s.Days030 == rp(2_500_000) && s.Days3160 == rp(3_000_000) && s.Total == rp(5_500_000),
		"aging PT Sumber Jaya: 0-30 = %s, 31-60 = %s, jumlah = %s (FIFO: bayar 1 jt menutup BILL-S1)",
		uang(s.Days030), uang(s.Days3160), uang(s.Total))
	m := barisPihak(pMitra)
	cek(m.NotDue == rp(1_500_000) && m.Days90Plus == rp(3_000_000) && m.Total == rp(4_500_000),
		"aging CV Mitra: belum jatuh tempo = %s, 90+ = %s, jumlah = %s",
		uang(m.NotDue), uang(m.Days90Plus), uang(m.Total))
	tanpa := barisPihak(models.PartyIDTanpaPihak)
	cek(tanpa.Days6190 == rp(800_000) && tanpa.PartyName == models.PartyNameTanpaSupplier,
		"aging jurnal tanpa supplier: 61-90 = %s, nama baris = %q",
		uang(tanpa.Days6190), tanpa.PartyName)
	cek(aging.Total.Total == rp(10_800_000) && aging.Total.Days90Plus == rp(3_000_000),
		"jumlah aging: total = %s, 90+ = %s", uang(aging.Total.Total), uang(aging.Total.Days90Plus))

	// ===== 4. Tie-out: jumlah subledger = saldo akun hutang di buku besar =====
	var glNum pgtype.Numeric
	err = pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(le.credit - le.debit), 0)
		FROM ledger_entries le
		JOIN journals j ON j.id = le.journal_id AND j.company_id = le.company_id
		JOIN accounts a ON a.id = le.account_id AND a.company_id = le.company_id
		WHERE le.company_id = $1 AND a.is_payable
		  AND j.status = 'posted' AND j.journal_date <= $2`,
		company.ID, tgl(asOf)).Scan(&glNum)
	if err != nil {
		panic(err)
	}
	cek(aging.Total.Total == models.NumericToCents(glNum),
		"jumlah subledger (%s) == saldo akun hutang buku besar (%s)",
		uang(aging.Total.Total), uang(models.NumericToCents(glNum)))

	// ===== 5. Render route dengan middleware asli + angka di HTML =====
	h := handlers.New(q, false, "")
	router := chi.NewRouter()
	router.Group(func(r chi.Router) {
		r.Use(appmw.RequireAuth(q, false))
		r.Route("/companies/{companyKey}", func(r chi.Router) {
			r.Use(appmw.RequireCompany(q))
			r.Get("/payables", h.PayablesGet)
		})
	})

	token := "token-bukti-hutang-3b"
	if _, err := q.CreateSession(ctx, db.CreateSessionParams{
		UserID:    userID,
		TokenHash: auth.HashToken(token),
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true, InfinityModifier: pgtype.Finite},
	}); err != nil {
		panic(err)
	}
	req := httptest.NewRequest("GET", "/companies/"+company.UrlKey+"/payables?to=15/10/2026", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		panic(fmt.Sprintf("GET /payables -> HTTP %d", w.Code))
	}
	html := w.Body.String()
	cek(strings.Contains(html, "Saldo Hutang per Supplier") && strings.Contains(html, "15 Oktober 2026"),
		"GET /companies/{companyKey}/payables render (HTTP 200, judul tabel + tanggal laporan ada)")

	cekBaris := func(needle string, want ...string) {
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
	cekBaris("PT Sumber Jaya", "", uang(rp(2_500_000)), uang(rp(3_000_000)), "", "", uang(rp(5_500_000)))
	cekBaris("CV Mitra", uang(rp(1_500_000)), "", "", "", uang(rp(3_000_000)), uang(rp(4_500_000)))
	cekBaris("Tanpa Supplier", "", "", "", uang(rp(800_000)), "", uang(rp(800_000)))
	cekBaris("Jumlah Hutang",
		uang(rp(1_500_000)), uang(rp(2_500_000)), uang(rp(3_000_000)),
		uang(rp(800_000)), uang(rp(3_000_000)), uang(rp(10_800_000)))

	// Simpan HTML bila diminta, sebagai bukti berkas.
	if dir := os.Getenv("SMOKE_SAVE_DIR"); dir != "" {
		if err := os.WriteFile(filepath.Join(dir, "bukti-hutang-usaha.html"), []byte(html), 0o644); err != nil {
			panic(err)
		}
		fmt.Println("PASS HTML bukti hutang disimpan di", dir)
	}
}

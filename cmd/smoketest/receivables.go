// Bukti runtime subledger piutang usaha + aging (Stage 3a). Satu perusahaan
// uji disi dengan pelanggan, akun piutang bertanda is_receivable, jurnal
// piutang ber-jatuh tempo beda umur, dan satu pembayaran sebagian, lalu:
//
//  1. aging dihitung lewat query dan model yang sama dengan handler lalu
//     dibandingkan ke harapan per kolom (belum jatuh tempo, 0-30, 31-60,
//     61-90, 90+ hari);
//  2. route /receivables dirender lewat router dengan middleware asli
//     (RequireAuth + RequireCompany);
//  3. angka yang benar-benar muncul di HTML dibaca kembali per pelanggan;
//  4. jumlah subledger diuji sama dengan saldo akun piutang di buku besar,
//     dan FK komposit pihak diuji menolak pihak lintas perusahaan.
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

// evidenceReceivables menjalankan seluruh bukti subledger piutang usaha.
func evidenceReceivables(ctx context.Context, pool *pgxpool.Pool, q *db.Queries, userID int64) {
	// ===== 1. Perusahaan, akun, pihak =====
	company, err := q.CreateCompany(ctx, db.CreateCompanyParams{
		Name: "PT Bukti Piutang", Slug: "pt-bukti-piutang",
		BaseCurrency: "IDR", UrlKey: "bukti3apiut",
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
		{"1-2000", "Piutang Usaha", "asset", models.SubCurrentAsset, "debit", models.PsakPiutang},
		{"4-1000", "Pendapatan Sewa", "revenue", models.SubRevenue, "credit", models.PsakPendapatan},
	} {
		var id int64
		err := pool.QueryRow(ctx, `
			INSERT INTO accounts (company_id, code, name, account_type, sub_type, normal_balance, psak_line, is_receivable)
			VALUES ($1, $2, $3, $4::account_type, $5, $6::normal_balance, $7, $8) RETURNING id`,
			company.ID, a.code, a.name, a.tipe, a.sub, a.normal, a.psak,
			a.code == "1-2000").Scan(&id)
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
	pAndalas := pihak("customer", "CV Andalas", "C-001")
	pBahari := pihak("customer", "PT Bahari", "C-002")
	_ = pihak("supplier", "PT Sumber Jaya", "S-001") // tabel partai campuran pelanggan+supplier

	// ===== 2. Jurnal piutang + pembayaran =====
	// Harapan aging per tanggal laporan 15/10/2026:
	//   CV Andalas : INV-A1 5 jt jatuh tempo 31/08 (45 hari) -> 31-60,
	//                INV-A2 2 jt jatuh tempo 10/10 (5 hari)  -> 0-30,
	//                PAY-A1 bayar 1 jt (FIFO menutup INV-A1) -> sisa 4 jt.
	//   PT Bahari  : INV-B1 3 jt jatuh tempo 31/05 (137 hari) -> 90+,
	//                INV-B2 1,5 jt jatuh tempo 30/11          -> belum jatuh tempo.
	//   Tanpa pihak: INV-X 0,7 jt jatuh tempo 25/07 (82 hari) -> 61-90.
	jurnal := func(ref, memo string, tanggal time.Time, debitAkun, kreditAkun string, juta int64) int64 {
		// Kolom NUMERIC berisi Rupiah utuh; sen hanya dipakai model laporan.
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
	aturPihak := func(jID, partyID int64, due time.Time) {
		_, err := pool.Exec(ctx,
			`UPDATE journals SET party_id = $1, due_date = $2 WHERE id = $3 AND company_id = $4`,
			partyID, tgl(due), jID, company.ID)
		if err != nil {
			panic(fmt.Errorf("set pihak jurnal %d: %w", jID, err))
		}
	}

	invA1 := jurnal("INV-A1", "Sewa kredit Andalas", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), "1-2000", "4-1000", 5_000_000)
	aturPihak(invA1, pAndalas, time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC))
	invA2 := jurnal("INV-A2", "Sewa kredit Andalas", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), "1-2000", "4-1000", 2_000_000)
	aturPihak(invA2, pAndalas, time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC))
	payA1 := jurnal("PAY-A1", "Pelunasan sebagian Andalas", time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC), "1-1000", "1-2000", 1_000_000)
	aturPihak(payA1, pAndalas, time.Time{})

	invB1 := jurnal("INV-B1", "Sewa kredit Bahari", time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), "1-2000", "4-1000", 3_000_000)
	aturPihak(invB1, pBahari, time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC))
	invB2 := jurnal("INV-B2", "Sewa kredit Bahari", time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC), "1-2000", "4-1000", 1_500_000)
	aturPihak(invB2, pBahari, time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC))

	invX := jurnal("INV-X", "Sewa kredit tanpa pelanggan", time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), "1-2000", "4-1000", 700_000)
	if _, err := pool.Exec(ctx,
		`UPDATE journals SET due_date = $1 WHERE id = $2 AND company_id = $3`,
		tgl(time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)), invX, company.ID); err != nil {
		panic(err)
	}
	fmt.Println("PASS seed bukti piutang: 3 akun, 3 pihak (2 pelanggan + 1 supplier), 6 jurnal")

	// ===== 3. Aging via query + model yang sama dengan handler =====
	asOf := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	movs, err := q.ListReceivableMovements(ctx, db.ListReceivableMovementsParams{
		CompanyID: company.ID, AsOf: tgl(asOf),
	})
	if err != nil {
		panic(err)
	}
	items := make([]models.ReceivableItem, 0, len(movs))
	for _, m := range movs {
		items = append(items, models.ReceivableItem{
			JournalID: m.JournalID, Reference: m.Reference,
			PartyID: m.PartyID, PartyName: m.PartyName,
			JournalDate: m.JournalDate.Time, DueDate: m.DueDate.Time, HasDueDate: m.DueDate.Valid,
			AmountCents: models.NumericToCents(m.TotalDebit) - models.NumericToCents(m.TotalCredit),
		})
	}
	aging := models.BuildReceivableAging(items, asOf)
	uang := func(cents int64) string { return models.FormatRounded(cents, company.RoundingUnit) }

	barisPihak := func(partyID int64) models.ReceivableRow {
		for _, r := range aging.Rows {
			if r.PartyID == partyID {
				return r
			}
		}
		panic(fmt.Errorf("baris pihak %d tidak ada", partyID))
	}
	a := barisPihak(pAndalas)
	cek(a.Days030 == rp(2_000_000) && a.Days3160 == rp(4_000_000) && a.Total == rp(6_000_000),
		"aging CV Andalas: 0-30 = %s, 31-60 = %s, jumlah = %s (FIFO: bayar 1 jt menutup INV-A1)",
		uang(a.Days030), uang(a.Days3160), uang(a.Total))
	b := barisPihak(pBahari)
	cek(b.NotDue == rp(1_500_000) && b.Days90Plus == rp(3_000_000) && b.Total == rp(4_500_000),
		"aging PT Bahari: belum jatuh tempo = %s, 90+ = %s, jumlah = %s",
		uang(b.NotDue), uang(b.Days90Plus), uang(b.Total))
	tanpa := barisPihak(models.PartyIDTanpaPihak)
	cek(tanpa.Days6190 == rp(700_000) && tanpa.PartyName == models.PartyNameTanpaPihak,
		"aging jurnal tanpa pelanggan: 61-90 = %s, nama baris = %q",
		uang(tanpa.Days6190), tanpa.PartyName)
	cek(aging.Total.Total == rp(11_200_000) && aging.Total.Days90Plus == rp(3_000_000),
		"jumlah aging: total = %s, 90+ = %s", uang(aging.Total.Total), uang(aging.Total.Days90Plus))

	// ===== 4. Tie-out: jumlah subledger = saldo akun piutang di buku besar =====
	var glNum pgtype.Numeric
	err = pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(le.debit - le.credit), 0)
		FROM ledger_entries le
		JOIN journals j ON j.id = le.journal_id AND j.company_id = le.company_id
		JOIN accounts a ON a.id = le.account_id AND a.company_id = le.company_id
		WHERE le.company_id = $1 AND a.is_receivable
		  AND j.status = 'posted' AND j.journal_date <= $2`,
		company.ID, tgl(asOf)).Scan(&glNum)
	if err != nil {
		panic(err)
	}
	cek(aging.Total.Total == models.NumericToCents(glNum),
		"jumlah subledger (%s) == saldo akun piutang buku besar (%s)",
		uang(aging.Total.Total), uang(models.NumericToCents(glNum)))

	// ===== 5. FK komposit menolak pihak lintas perusahaan =====
	var companyAsing int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO companies (name, slug, url_key) VALUES ('PT Pihak Asing', 'pt-pihak-asing', 'bukti3aasing') RETURNING id`,
	).Scan(&companyAsing); err != nil {
		panic(err)
	}
	var pihakAsing int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO parties (company_id, party_type, name) VALUES ($1, 'customer', 'CV Asing') RETURNING id`,
		companyAsing).Scan(&pihakAsing); err != nil {
		panic(err)
	}
	_, err = pool.Exec(ctx, `UPDATE journals SET party_id = $1 WHERE id = $2 AND company_id = $3`,
		pihakAsing, invA1, company.ID)
	if err == nil {
		panic("pihak lintas perusahaan harusnya ditolak FK komposit")
	}
	cek(true, "FK komposit journals.party_id menolak pihak lintas perusahaan")

	// ===== 6. Render route dengan middleware asli + angka di HTML =====
	h := handlers.New(q, false, "")
	router := chi.NewRouter()
	router.Group(func(r chi.Router) {
		r.Use(appmw.RequireAuth(q, false))
		r.Route("/companies/{companyKey}", func(r chi.Router) {
			r.Use(appmw.RequireCompany(q))
			r.Get("/receivables", h.ReceivablesGet)
		})
	})

	token := "token-bukti-piutang-3a"
	if _, err := q.CreateSession(ctx, db.CreateSessionParams{
		UserID:    userID,
		TokenHash: auth.HashToken(token),
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true, InfinityModifier: pgtype.Finite},
	}); err != nil {
		panic(err)
	}
	req := httptest.NewRequest("GET", "/companies/"+company.UrlKey+"/receivables?to=15/10/2026", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		panic(fmt.Sprintf("GET /receivables -> HTTP %d", w.Code))
	}
	html := w.Body.String()
	cek(strings.Contains(html, "Saldo Piutang per Pelanggan") && strings.Contains(html, "15 Oktober 2026"),
		"GET /companies/{companyKey}/receivables render (HTTP 200, judul tabel + tanggal laporan ada)")

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
	cekBaris("CV Andalas", "", uang(rp(2_000_000)), uang(rp(4_000_000)), "", "", uang(rp(6_000_000)))
	cekBaris("PT Bahari", uang(rp(1_500_000)), "", "", "", uang(rp(3_000_000)), uang(rp(4_500_000)))
	cekBaris("Tanpa Pelanggan", "", "", "", uang(rp(700_000)), "", uang(rp(700_000)))
	cekBaris("Jumlah Piutang",
		uang(rp(1_500_000)), uang(rp(2_000_000)), uang(rp(4_000_000)),
		uang(rp(700_000)), uang(rp(3_000_000)), uang(rp(11_200_000)))

	// Simpan HTML bila diminta, sebagai bukti berkas.
	if dir := os.Getenv("SMOKE_SAVE_DIR"); dir != "" {
		if err := os.WriteFile(filepath.Join(dir, "bukti-piutang-usaha.html"), []byte(html), 0o644); err != nil {
			panic(err)
		}
		fmt.Println("PASS HTML bukti piutang disimpan di", dir)
	}
}

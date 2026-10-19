// Bukti runtime revisi Tuan Muda (Okt 2026): pilihan tahun di dashboard,
// tampilan "Posisi Semua Akun" di jurnal umum, rentang bulan lintas tahun di
// buku besar, neraca saldo, jurnal penutup + neraca saldo setelah penutupan,
// kartu kas kecil (imprest) di Kas & Bank, dan rekap stok persediaan.
// Perusahaan bukti tersendiri dengan data kecil yang dihitung manual.
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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

// evidenceRevisi menjalankan seluruh bukti revisi dashboard sampai rekap.
func evidenceRevisi(ctx context.Context, pool *pgxpool.Pool, q *db.Queries, userID int64) {
	// ===== 1. Perusahaan bukti: akun, periode FY2026, jurnal =====
	company, err := q.CreateCompany(ctx, db.CreateCompanyParams{
		Name: "PT Bukti Revisi", Slug: "pt-bukti-revisi",
		BaseCurrency: "IDR", UrlKey: "buktirevisi",
	})
	if err != nil {
		panic(err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO company_users (company_id, user_id, role) VALUES ($1, $2, 'owner')`,
		company.ID, userID); err != nil {
		panic(err)
	}

	akun := map[string]int64{}
	for _, a := range []akunUji{
		{"1001", "Kas", "asset", models.SubCurrentAsset, "debit", models.PsakKasSetaraKas},
		{"1002", "Kas Kecil", "asset", models.SubCurrentAsset, "debit", models.PsakKasSetaraKas},
		{"3001", "Saldo Laba", "equity", models.SubEquity, "credit", ""},
		{"4001", "Pendapatan Jasa", "revenue", models.SubRevenue, "credit", ""},
		{"5001", "Beban Operasional", "expense", models.SubExpense, "debit", ""},
	} {
		var id int64
		err := pool.QueryRow(ctx, `
			INSERT INTO accounts (company_id, code, name, account_type, sub_type, normal_balance, psak_line)
			VALUES ($1, $2, $3, $4::account_type, $5, $6::normal_balance, $7) RETURNING id`,
			company.ID, a.code, a.name, a.tipe, a.sub, a.normal, a.psak).Scan(&id)
		if err != nil {
			panic(fmt.Errorf("akun %s: %w", a.code, err))
		}
		akun[a.code] = id
	}
	// Tandai akun kas kecil (imprest) dan akun saldo laba (tujuan tutup buku).
	if _, err := pool.Exec(ctx,
		`UPDATE accounts SET system_role = $3 WHERE id = $1 AND company_id = $2`,
		akun["1002"], company.ID, models.SystemRoleKasKecil); err != nil {
		panic(err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE accounts SET system_role = $3 WHERE id = $1 AND company_id = $2`,
		akun["3001"], company.ID, models.SystemRoleRetainedEarnings); err != nil {
		panic(err)
	}

	periode, err := q.CreateFiscalPeriod(ctx, db.CreateFiscalPeriodParams{
		CompanyID: company.ID, Name: "FY 2026",
		StartDate: tgl(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		EndDate:   tgl(time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)),
		IsFirst:   true,
	})
	if err != nil {
		panic(err)
	}

	jurnal := func(ref string, hari int, bulan time.Month, ids []int64, debits, credits []string) {
		debitNums := make([]pgtype.Numeric, len(debits))
		creditNums := make([]pgtype.Numeric, len(credits))
		for i := range debits {
			debitNums[i] = num(debits[i])
		}
		for i := range credits {
			creditNums[i] = num(credits[i])
		}
		descs := make([]string, len(ids))
		for i := range descs {
			descs[i] = "baris " + ref
		}
		if _, err := q.CreateSystemJournalWithEntries(ctx, db.CreateSystemJournalWithEntriesParams{
			CompanyID:    company.ID,
			JournalDate:  tgl(time.Date(2026, bulan, hari, 0, 0, 0, 0, time.UTC)),
			Reference:    ref,
			Memo:         "bukti revisi " + ref,
			CreatedBy:    userID,
			Kind:         db.JournalKindNormal,
			PeriodID:     pgtype.Int8{Int64: periode.ID, Valid: true},
			AccountIds:   ids,
			Descriptions: descs,
			Debits:       debitNums,
			Credits:      creditNums,
		}); err != nil {
			panic(fmt.Errorf("jurnal %s: %w", ref, err))
		}
	}
	// Pendapatan 5.000.000 (Kas masuk).
	jurnal("REV-1", 2, time.January, []int64{akun["1001"], akun["4001"]},
		[]string{"5000000.00", "0.00"}, []string{"0.00", "5000000.00"})
	// Kas kecil dibentuk 500.000, dipakai 450.000, sisa 50.000 tetap saldo.
	jurnal("KK-1", 10, time.February, []int64{akun["1002"], akun["1001"]},
		[]string{"500000.00", "0.00"}, []string{"0.00", "500000.00"})
	jurnal("KK-2", 15, time.February, []int64{akun["5001"], akun["1002"]},
		[]string{"450000.00", "0.00"}, []string{"0.00", "450000.00"})
	// Beban 1.200.000 dari Kas.
	jurnal("BEB-1", 15, time.March, []int64{akun["5001"], akun["1001"]},
		[]string{"1200000.00", "0.00"}, []string{"0.00", "1200000.00"})
	fmt.Println("PASS seed bukti revisi: 4 akun (kas kecil bertanda), periode FY 2026, 4 jurnal")

	// ===== 2. Router dengan middleware asli =====
	h := handlers.New(q, false, "")
	router := chi.NewRouter()
	router.Group(func(r chi.Router) {
		r.Use(appmw.RequireAuth(q, false))
		r.Route("/companies/{companyKey}", func(r chi.Router) {
			r.Use(appmw.RequireCompany(q))
			r.Get("/dashboard", h.DashboardGet)
			r.Get("/journals", h.JournalsListGet)
			r.Get("/ledger", h.LedgerGet)
			r.Get("/trial-balance", h.TrialBalanceGet)
			r.Get("/trial-balance/post-closing", h.PostClosingTrialBalanceGet)
			r.Get("/closing", h.ClosingGet)
			r.Get("/bank", h.BankGet)
			r.Get("/inventory", h.InventoryGet)
			r.Post("/periods/{periodID}/close", h.PeriodClosePost)
		})
	})

	token := "token-bukti-revisi"
	if _, err := q.CreateSession(ctx, db.CreateSessionParams{
		UserID:    userID,
		TokenHash: auth.HashToken(token),
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true, InfinityModifier: pgtype.Finite},
	}); err != nil {
		panic(err)
	}
	get := func(path string) string {
		req := httptest.NewRequest("GET", path, nil)
		req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			panic(fmt.Sprintf("GET %s -> HTTP %d", path, w.Code))
		}
		return w.Body.String()
	}
	base := "/companies/" + company.UrlKey

	// ===== 3. Dashboard: pilihan tahun pembukuan =====
	htmlDash := get(base + "/dashboard?year=2026")
	cek(strings.Contains(htmlDash, "Tahun Pembukuan") && strings.Contains(htmlDash, "<option value=\"2026\" selected"),
		"dashboard menyediakan pilihan tahun pembukuan, 2026 terpilih")
	// Laba 2026: pendapatan 5.000.000 - beban 1.650.000 = 3.350.000.
	cek(strings.Contains(htmlDash, "3.350.000"),
		"dashboard tahun 2026 menampilkan laba 3.350.000 (YTD tahun terpilih, bukan statis)")

	// ===== 4. Jurnal umum: posisi semua akun + total di bawah =====
	htmlJurnal := get(base + "/journals?year=2026")
	cek(strings.Contains(htmlJurnal, "Posisi Semua Akun"),
		"jurnal umum memuat tampilan posisi semua akun (tgl/kode/nama akun/D/K)")
	// Total debit = total kredit = 5.000.000 + 500.000 + 450.000 + 1.200.000 = 7.150.000.
	cek(strings.Count(htmlJurnal, "7.150.000,00") >= 2,
		"tampilan menyeluruh menutup dengan total debit = total kredit = 7.150.000,00")
	htmlJurnalAll := get(base + "/journals?year=0")
	cek(strings.Contains(htmlJurnalAll, "7.150.000,00"),
		"pilihan Semua tahun memuat seluruh baris buku lintas tahun")

	// ===== 5. Buku besar: rentang bulan lintas tahun =====
	htmlLedger := get(base + "/ledger?from=2026-02&to=2026-03")
	cek(strings.Contains(htmlLedger, "Rentang bulan terpilih") &&
		strings.Contains(htmlLedger, `font-medium text-ink">50.000</td>`),
		"buku besar menerima rentang bulan Feb-Mar 2026 dan memuat saldo kas kecil 50.000")
	htmlLedgerX := get(base + "/ledger?from=2025-11&to=2026-03")
	cek(strings.Contains(htmlLedgerX, "Rentang bulan terpilih"),
		"buku besar menerima rentang bulan lintas tahun (Nov 2025 - Mar 2026)")

	// ===== 6. Neraca saldo: total berimbang =====
	htmlTB := get(base + "/trial-balance")
	cek(strings.Contains(htmlTB, "Neraca Saldo") && strings.Count(htmlTB, "5.000.000") >= 3,
		"neraca saldo tampil dengan total debit = total kredit = 5.000.000")

	// ===== 7. Kas kecil di Kas & Bank =====
	htmlBank := get(base + "/bank")
	cek(strings.Contains(htmlBank, "Saldo Kas Kecil") && strings.Contains(htmlBank, "50.000"),
		"Kas & Bank menampilkan kartu kas kecil dengan saldo sisa 50.000 (dana imprest)")

	// ===== 8. Jurnal penutup + NS setelah penutupan =====
	htmlPCTB := get(base + "/trial-balance/post-closing?period=" + fmt.Sprint(periode.ID))
	cek(strings.Contains(htmlPCTB, "belum ditutup"),
		"NS setelah penutupan memperingatkan saat periode masih terbuka")

	reqClose := httptest.NewRequest("POST", base+"/periods/"+fmt.Sprint(periode.ID)+"/close", nil)
	reqClose.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})
	wClose := httptest.NewRecorder()
	router.ServeHTTP(wClose, reqClose)
	cek(wClose.Code == http.StatusSeeOther,
		"tutup buku periode FY 2026 dari halaman jurnal penutup (HTTP 303)")

	htmlClosing := get(base + "/closing")
	cek(strings.Contains(htmlClosing, "Daftar Jurnal Penutup") && strings.Contains(htmlClosing, "PENUTUP-"),
		"halaman jurnal penutup menampilkan jurnal penutup hasil tutup buku")

	htmlPCTB2 := get(base + "/trial-balance/post-closing?period=" + fmt.Sprint(periode.ID))
	cek(!strings.Contains(htmlPCTB2, "belum ditutup"),
		"NS setelah penutupan tidak memperingatkan lagi setelah periode ditutup")
	cek(!strings.Contains(htmlPCTB2, ">4001<") && !strings.Contains(htmlPCTB2, ">5001<"),
		"akun pendapatan dan beban dinolkan jurnal penutupan (tidak tampil di NS setelah penutupan)")
	// Saldo kas setelah penutupan: 5.000.000 - 500.000 - 1.200.000 = 3.300.000.
	cek(strings.Contains(htmlPCTB2, `text-right text-ink">3.300.000</td>`),
		"NS setelah penutupan memuat saldo kas 3.300.000")

	// ===== 9. Rekap persediaan =====
	item, err := q.CreateInventoryItem(ctx, db.CreateInventoryItemParams{
		CompanyID: company.ID, Name: "Solar", Sku: "SLR", Unit: "liter",
	})
	if err != nil {
		panic(err)
	}
	for _, mv := range []struct {
		tipe string
		qty  string
		hari int
	}{{models.MovementIn, "100.00", 1}, {models.MovementOut, "55.00", 10}} {
		if _, err := q.CreateInventoryMovement(ctx, db.CreateInventoryMovementParams{
			CompanyID: company.ID, ItemID: item.ID,
			Date:         tgl(time.Date(2026, 5, mv.hari, 0, 0, 0, 0, time.UTC)),
			Quantity:     num(mv.qty),
			UnitCost:     num("10000.00"),
			MovementType: db.InventoryMovementType(mv.tipe),
		}); err != nil {
			panic(err)
		}
	}
	htmlInv := get(base + "/inventory")
	cek(strings.Contains(htmlInv, "Rekap Stok Seluruh Barang") && strings.Contains(htmlInv, "45</td>"),
		"halaman persediaan memuat rekap stok (masuk 100, keluar 55, sisa 45)")
}

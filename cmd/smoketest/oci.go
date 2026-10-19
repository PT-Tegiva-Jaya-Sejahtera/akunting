// Bukti runtime laporan Stage 4d: Penghasilan Komprehensif Lain (PSAK 201
// par. 82A). Perusahaan bukti tersendiri dengan dua akun ekuitas ber-oci_kind
// yang diisi lewat jurnal antar-ekuitas (netral terhadap A = L + E dan tidak
// menyentuh laba rugi maupun kas), lalu Laporan Laba Rugi dirender lewat
// router dengan middleware asli dan angka blok OCI dibaca kembali dari HTML:
//
// 1. blok "Penghasilan Komprehensif Lain" tampil setelah Laba (Rugi) dengan
//    dua sub-blok: komponen akan direklasifikasi dan tidak direklasifikasi;
// 2. "Jumlah Penghasilan Komprehensif Lain" == jumlah kedua sub-blok;
// 3. "Jumlah Laba Rugi Komprehensif" == laba (rugi) + jumlah OCI;
// 4. perusahaan tanpa akun ber-oci_kind tetap menampilkan blok dengan nilai
//    nol dan laba komprehensif == laba rugi.
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

// evidenceOCI menjalankan seluruh bukti laporan Stage 4d.
func evidenceOCI(ctx context.Context, pool *pgxpool.Pool, q *db.Queries, userID int64) {
	// ===== 1. Perusahaan bukti dengan akun OCI =====
	company, err := q.CreateCompany(ctx, db.CreateCompanyParams{
		Name: "PT Bukti OCI", Slug: "pt-bukti-oci",
		BaseCurrency: "IDR", UrlKey: "buktioci",
	})
	if err != nil {
		panic(err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO company_users (company_id, user_id, role) VALUES ($1, $2, 'owner')`,
		company.ID, userID); err != nil {
		panic(err)
	}

	// Kelompok akun: kas + pendapatan + beban (laba sebelum pajak 6 juta,
	// bersih 4,68 juta setelah PPh kini 22% PSAK 212), dua akun ekuitas
	// penampung recognisi, dan dua akun ber-oci_kind sesuai par. 82A.
	type akunOCI struct{ code, name, tipe, sub, normal, psak, kind string }
	akuns := []akunOCI{
		{"1-1000", "Kas", "asset", models.SubCurrentAsset, "debit", models.PsakKasSetaraKas, ""},
		{"4-1000", "Pendapatan Sewa", "revenue", models.SubRevenue, "credit", models.PsakPendapatan, ""},
		{"5-1000", "Beban Gaji", "expense", models.SubExpense, "debit", models.PsakBebanOperasional, ""},
		{"3-1000", "Modal Disetor", "equity", models.SubEquity, "credit", models.PsakModal, ""},
		{"3-2000", "Saldo Laba", "equity", models.SubEquity, "credit", models.PsakSaldoLaba, ""},
		{"3-4000", "Selisih Kurs Penjabaran", "equity", models.SubEquity, "credit", models.PsakModal, models.OciRecyclable},
		{"3-5000", "Pengukuran Kembali Imbalan Pasti", "equity", models.SubEquity, "credit", models.PsakModal, models.OciNonRecyclable},
	}
	akunIDs := map[string]int64{}
	for _, a := range akuns {
		var id int64
		err := pool.QueryRow(ctx, `
			INSERT INTO accounts (company_id, code, name, account_type, sub_type, normal_balance, psak_line, oci_kind)
			VALUES ($1, $2, $3, $4::account_type, $5, $6::normal_balance, $7, NULLIF($8, '')) RETURNING id`,
			company.ID, a.code, a.name, a.tipe, a.sub, a.normal, a.psak, a.kind).Scan(&id)
		if err != nil {
			panic(fmt.Errorf("akun %s: %w", a.code, err))
		}
		akunIDs[a.code] = id
	}

	// Jurnal: transaksi biasa membentuk laba 6 juta (kas 10 juta masuk,
	// 4 juta keluar), lalu dua jurnal antar-ekuitas mengisi akun OCI:
	// tidak memasukkan atau mengeluarkan aset, tidak menyentuh laba rugi.
	type baris struct {
		akun          string
		debit, credit int64
	}
	type jurnal struct{ tanggal time.Time; ref, memo string; baris []baris }
	skenario := []jurnal{
		{time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), "JV-OCI-01", "Pendapatan sewa tunai", []baris{
			{"1-1000", 10_000_000, 0}, {"4-1000", 0, 10_000_000}}},
		{time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC), "JV-OCI-02", "Beban gaji tunai", []baris{
			{"5-1000", 4_000_000, 0}, {"1-1000", 0, 4_000_000}}},
		{time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), "JV-OCI-03", "Selisih kurs penjabaran (OCI, direklasifikasi)", []baris{
			{"3-1000", 8_000_000, 0}, {"3-4000", 0, 8_000_000}}},
		{time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC), "JV-OCI-04", "Pengukuran kembali imbalan pasti (OCI, tidak direklasifikasi)", []baris{
			{"3-2000", 5_000_000, 0}, {"3-5000", 0, 5_000_000}}},
	}
	for _, j := range skenario {
		ids := make([]int64, 0, len(j.baris))
		desc := make([]string, 0, len(j.baris))
		debits := make([]pgtype.Numeric, 0, len(j.baris))
		credits := make([]pgtype.Numeric, 0, len(j.baris))
		for _, b := range j.baris {
			ids = append(ids, akunIDs[b.akun])
			desc = append(desc, j.memo)
			debits = append(debits, num(fmt.Sprintf("%d.00", b.debit)))
			credits = append(credits, num(fmt.Sprintf("%d.00", b.credit)))
		}
		if _, err := q.CreateJournalWithEntries(ctx, db.CreateJournalWithEntriesParams{
			CompanyID: company.ID, JournalDate: tgl(j.tanggal), Reference: j.ref,
			Memo: j.memo, CreatedBy: userID,
			AccountIds: ids, Descriptions: desc, Debits: debits, Credits: credits,
		}); err != nil {
			panic(fmt.Errorf("jurnal %s: %w", j.ref, err))
		}
	}
	fmt.Println("PASS seed OCI: perusahaan, 7 akun (2 ber-oci_kind), 4 jurnal seimbang")

	// ===== 2. Render route dengan middleware asli =====
	h := handlers.New(q, false, "")
	router := chi.NewRouter()
	router.Group(func(r chi.Router) {
		r.Use(appmw.RequireAuth(q, false))
		r.Route("/companies/{companyKey}", func(r chi.Router) {
			r.Use(appmw.RequireCompany(q))
			r.Get("/reports/pl", h.ReportsPLGet)
		})
	})
	token := "token-bukti-laporan-oci"
	if _, err := q.CreateSession(ctx, db.CreateSessionParams{
		UserID:    userID,
		TokenHash: auth.HashToken(token),
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true, InfinityModifier: pgtype.Finite},
	}); err != nil {
		panic(err)
	}

	company2, err := q.CreateCompany(ctx, db.CreateCompanyParams{
		Name: "PT Tanpa OCI", Slug: "pt-tanpa-oci",
		BaseCurrency: "IDR", UrlKey: "tanpaoci",
	})
	if err != nil {
		panic(err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO company_users (company_id, user_id, role) VALUES ($1, $2, 'owner')`,
		company2.ID, userID); err != nil {
		panic(err)
	}
	var kas2, pend2 int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO accounts (company_id, code, name, account_type, normal_balance)
		VALUES ($1, '1001', 'Kas', 'asset', 'debit') RETURNING id`, company2.ID).Scan(&kas2); err != nil {
		panic(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO accounts (company_id, code, name, account_type, normal_balance)
		VALUES ($1, '4001', 'Pendapatan', 'revenue', 'credit') RETURNING id`, company2.ID).Scan(&pend2); err != nil {
		panic(err)
	}
	if _, err := q.CreateJournalWithEntries(ctx, db.CreateJournalWithEntriesParams{
		CompanyID: company2.ID, JournalDate: tgl(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)),
		Reference: "JV-X-01", Memo: "Pendapatan tunai", CreatedBy: userID,
		AccountIds:   []int64{kas2, pend2},
		Descriptions: []string{"Kas masuk", "Pendapatan jasa"},
		Debits:       []pgtype.Numeric{num("1000000.00"), num("0")},
		Credits:      []pgtype.Numeric{num("0"), num("1000000.00")},
	}); err != nil {
		panic(fmt.Errorf("jurnal PT Tanpa OCI: %w", err))
	}

	uang := func(cents int64) string { return models.FormatRounded(cents, company.RoundingUnit) }
	get := func(urlKey string) string {
		req := httptest.NewRequest("GET", fmt.Sprintf("/companies/%s/reports/pl?from=01/01/2026&to=31/12/2026", urlKey), nil)
		req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			panic(fmt.Sprintf("GET /reports/pl (%s) -> HTTP %d", urlKey, w.Code))
		}
		return w.Body.String()
	}
	html := get(company.UrlKey)

	// ===== 3. Blok OCI dua kelompok di HTML Laba Rugi =====
	cek(strings.Contains(html, "Laporan Laba Rugi dan Penghasilan Komprehensif Lain"),
		"HTML PL: judul memuat 'dan Penghasilan Komprehensif Lain'")
	cek(strings.Contains(html, "Penghasilan Komprehensif Lain (PSAK 201 par. 82A)"),
		"HTML PL: header blok OCI par. 82A tampil setelah Laba (Rugi)")
	labaSebelum := rowNumCells(html, "Laba Sebelum Pajak")[0]
	cek(labaSebelum == uang(rp(6_000_000)),
		"HTML PL: Laba Sebelum Pajak = %s (sesuai skenario 10 jt - 4 jt)", labaSebelum)
	pajak := rowNumCells(html, "Jumlah Beban Pajak")[0]
	cek(pajak == uang(rp(1_320_000)),
		"HTML PL: Jumlah Beban Pajak = %s (22%% x 6 jt, tarif default PSAK 212)", pajak)
	laba := rowNumCells(html, "Laba (Rugi) Periode Berjalan")[0]
	cek(laba == uang(rp(4_680_000)),
		"HTML PL: Laba (Rugi) Periode Berjalan = %s (6 jt - beban pajak 1,32 jt)", laba)
	recy := rowNumCells(html, "Jumlah Komponen Akan Direklasifikasi ke Laba Rugi")[0]
	roots := rowNumCells(html, "Jumlah Komponen Tidak Direklasifikasi ke Laba Rugi")[0]
	cek(recy == uang(rp(8_000_000)) && roots == uang(rp(5_000_000)),
		"HTML PL: subtotal dua kelompok OCI (akan rekla %s, tidak rekla %s) sesuai 2 akun bersaldo", recy, roots)
	barisiOCI := rowNumCells(html, "Jumlah Penghasilan Komprehensif Lain")[0]
	cek(barisiOCI == uang(rp(13_000_000)),
		"HTML PL: Jumlah Penghasilan Komprehensif Lain = %s (8 jt + 5 jt)", barisiOCI)
	komp := rowNumCells(html, "Jumlah Laba Rugi Komprehensif")[0]
	cek(komp == uang(rp(17_680_000)),
		"HTML PL: Jumlah Laba Rugi Komprehensif = %s = laba 4,68 jt + OCI 13 jt", komp)
	stringAyar := strings.Contains(html, "Selisih Kurs Penjabaran") && strings.Contains(html, "Pengukuran Kembali Imbalan Pasti")
	cek(stringAyar,
		"HTML PL: baris detail kedua akun OCI tampil pada blok masing-masing")

	// ===== 4. Perusahaan tanpa akun ber-oci_kind: blok nol dan konsisten =====
	html0 := get(company2.UrlKey)
	oci0 := rowNumCells(html0, "Jumlah Penghasilan Komprehensif Lain")[0]
	komp0 := rowNumCells(html0, "Jumlah Laba Rugi Komprehensif")[0]
	laba0 := rowNumCells(html0, "Laba (Rugi) Periode Berjalan")[0]
	cek(oci0 == uang(rp(0)) && komp0 == laba0,
		"HTML PL tanpa akun OCI: jumlah OCI = %s, laba komprehensif (%s) == laba rugi (%s)", oci0, komp0, laba0)

	// Simpan HTML bila diminta, sebagai bukti berkas.
	if dir := os.Getenv("SMOKE_SAVE_DIR"); dir != "" {
		if err := os.WriteFile(filepath.Join(dir, "bukti-laba-rugi-oci.html"), []byte(html), 0o644); err != nil {
			panic(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "bukti-laba-rugi-tanpa-oci.html"), []byte(html0), 0o644); err != nil {
			panic(err)
		}
		fmt.Println("PASS HTML bukti OCI disimpan di", dir)
	}
}

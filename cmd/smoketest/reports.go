// Bukti runtime laporan Stage 2b: Laporan Perubahan Ekuitas, Laporan Arus Kas,
// dan Catatan atas Laporan Keuangan. Satu perusahaan uji disi dengan dua tahun
// buku (termasuk jurnal saldo awal dan jurnal penutup), lalu:
//
//  1. angka laporan dihitung lewat query yang sama dengan handler dan
//     diuji penutupnya ke angka Neraca;
//  2. keempat route laporan dirender lewat router dengan middleware asli
//     (RequireAuth + RequireCompany);
//  3. angka yang benar-benar muncul di HTML dibaca kembali dan dibandingkan
//     silang antar laporan (kas akhir Arus Kas = kas di Neraca, saldo akhir
//     Perubahan Ekuitas = komponen ekuitas di Neraca).
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

// cek memvalidasi satu syarat; panic bila gagal, cetak PASS beserta angkanya
// bila lulus, supaya keluaran smoketest menjadi bukti yang bisa dibaca.
func cek(cond bool, format string, args ...any) {
	if !cond {
		panic("GAGAL: " + fmt.Sprintf(format, args...))
	}
	fmt.Println("PASS", fmt.Sprintf(format, args...))
}

func tgl(t time.Time) pgtype.Date {
	return pgtype.Date{Time: t, Valid: true, InfinityModifier: pgtype.Finite}
}

// rp mengubah Rupiah menjadi satuan sen yang dipakai seluruh perakit laporan.
func rp(r int64) int64 { return r * 100 }

// akunUji adalah satu baris chart of accounts untuk perusahaan bukti.
type akunUji struct {
	code, name, tipe, sub, normal, psak string
}

var akunBukti = []akunUji{
	{"1-1000", "Kas", "asset", models.SubCurrentAsset, "debit", models.PsakKasSetaraKas},
	{"1-1100", "Bank", "asset", models.SubCurrentAsset, "debit", models.PsakKasSetaraKas},
	{"1-2000", "Piutang Usaha", "asset", models.SubCurrentAsset, "debit", models.PsakPiutang},
	{"1-3000", "Persediaan", "asset", models.SubCurrentAsset, "debit", models.PsakPersediaan},
	{"1-4000", "Aset Tetap", "asset", models.SubNonCurrentAsset, "debit", models.PsakAsetTetap},
	{"1-4900", "Akumulasi Penyusutan", "asset", models.SubNonCurrentAsset, "credit", models.PsakAsetTetapAkum},
	{"2-1000", "Utang Usaha", "liability", models.SubCurrentLiability, "credit", models.PsakUtangUsaha},
	{"2-2000", "Utang Bank", "liability", models.SubCurrentLiability, "credit", models.PsakUtangBank},
	{"3-1000", "Modal Disetor", "equity", models.SubEquity, "credit", models.PsakModal},
	{"3-2000", "Saldo Laba", "equity", models.SubEquity, "credit", models.PsakSaldoLaba},
	{"3-3000", "Prive", "equity", models.SubEquity, "credit", ""},
	{"4-1000", "Pendapatan Sewa", "revenue", models.SubRevenue, "credit", models.PsakPendapatan},
	{"5-1000", "Beban Gaji", "expense", models.SubExpense, "debit", models.PsakBebanOperasional},
	{"5-2000", "Beban Listrik", "expense", models.SubExpense, "debit", models.PsakBebanOperasional},
	{"5-3000", "Beban Penyusutan", "expense", models.SubExpense, "debit", models.PsakBebanOperasional},
	{"5-4000", "Beban Bunga", "expense", models.SubExpense, "debit", models.PsakBebanKeuangan},
}

// barisUji adalah satu baris jurnal dalam Rupiah.
type barisUji struct {
	akun          string
	debit, credit int64
}

// jurnalUji adalah satu jurnal; kind selain "normal" dibuat lewat query
// CreateSystemJournalWithEntries (jurnal saldo awal dan jurnal penutup).
type jurnalUji struct {
	kind    db.JournalKind
	tanggal time.Time
	ref     string
	memo    string
	periode int64 // id periode fiskal untuk jurnal sistem
	adjust  bool
	baris   []barisUji
}

// skenarioBukti adalah dua tahun buku seimbang: tahun 2025 ditutup bukunya,
// tahun 2026 adalah periode berjalan. Angka sama dengan uji model, sehingga
// hasil runtime bisa dibandingkan langsung dengan ekspektasi uji unit.
func skenarioBukti(fy2025, fy2026 int64) []jurnalUji {
	return []jurnalUji{
		{kind: db.JournalKindOpening, tanggal: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			ref: "OPEN-2025", memo: "Saldo awal perusahaan", periode: fy2025, baris: []barisUji{
				{"1-1000", 20_000_000, 0}, {"1-1100", 30_000_000, 0},
				{"1-2000", 5_000_000, 0}, {"1-3000", 8_000_000, 0},
				{"1-4000", 60_000_000, 0}, {"1-4900", 0, 10_000_000},
				{"2-1000", 0, 6_000_000}, {"2-2000", 0, 25_000_000},
				{"3-1000", 0, 50_000_000}, {"3-2000", 0, 32_000_000},
			}},
		{kind: db.JournalKindNormal, tanggal: time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC),
			ref: "JV-2025-01", memo: "Pendapatan sewa tunai", baris: []barisUji{
				{"1-1000", 40_000_000, 0}, {"4-1000", 0, 40_000_000},
			}},
		{kind: db.JournalKindNormal, tanggal: time.Date(2025, 7, 15, 0, 0, 0, 0, time.UTC),
			ref: "JV-2025-02", memo: "Beban gaji", baris: []barisUji{
				{"5-1000", 25_000_000, 0}, {"1-1000", 0, 25_000_000},
			}},
		{kind: db.JournalKindClosing, tanggal: time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC),
			ref: "CLOSE-2025", memo: "Tutup buku 2025", periode: fy2025, baris: []barisUji{
				{"4-1000", 40_000_000, 0}, {"5-1000", 0, 25_000_000},
				{"3-2000", 0, 15_000_000},
			}},

		{kind: db.JournalKindNormal, tanggal: time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC),
			ref: "JV-2026-01", memo: "Setoran modal", baris: []barisUji{
				{"1-1000", 10_000_000, 0}, {"3-1000", 0, 10_000_000},
			}},
		{kind: db.JournalKindNormal, tanggal: time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC),
			ref: "JV-2026-02", memo: "Beli aset tetap tunai", baris: []barisUji{
				{"1-4000", 20_000_000, 0}, {"1-1000", 0, 20_000_000},
			}},
		{kind: db.JournalKindNormal, tanggal: time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC),
			ref: "JV-2026-03", memo: "Penarikan utang bank", baris: []barisUji{
				{"1-1100", 15_000_000, 0}, {"2-2000", 0, 15_000_000},
			}},
		{kind: db.JournalKindNormal, tanggal: time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC),
			ref: "JV-2026-04", memo: "Prive pemilik", baris: []barisUji{
				{"3-3000", 5_000_000, 0}, {"1-1000", 0, 5_000_000},
			}},
		{kind: db.JournalKindNormal, tanggal: time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
			ref: "JV-2026-05", memo: "Pendapatan sewa tunai", baris: []barisUji{
				{"1-1000", 25_000_000, 0}, {"4-1000", 0, 25_000_000},
			}},
		{kind: db.JournalKindNormal, tanggal: time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC),
			ref: "JV-2026-06", memo: "Pendapatan sewa kredit", baris: []barisUji{
				{"1-2000", 5_000_000, 0}, {"4-1000", 0, 5_000_000},
			}},
		{kind: db.JournalKindNormal, tanggal: time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC),
			ref: "JV-2026-07", memo: "Beli persediaan kredit", baris: []barisUji{
				{"1-3000", 7_000_000, 0}, {"2-1000", 0, 7_000_000},
			}},
		{kind: db.JournalKindNormal, tanggal: time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC),
			ref: "JV-2026-08", memo: "Bayar utang usaha", baris: []barisUji{
				{"2-1000", 3_000_000, 0}, {"1-1000", 0, 3_000_000},
			}},
		{kind: db.JournalKindNormal, tanggal: time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC),
			ref: "JV-2026-09", memo: "Beban gaji", baris: []barisUji{
				{"5-1000", 12_000_000, 0}, {"1-1000", 0, 12_000_000},
			}},
		{kind: db.JournalKindNormal, tanggal: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
			ref: "JV-2026-10", memo: "Beban listrik kredit", baris: []barisUji{
				{"5-2000", 3_000_000, 0}, {"2-1000", 0, 3_000_000},
			}},
		{kind: db.JournalKindNormal, tanggal: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC),
			ref: "JV-2026-11", memo: "Penyesuaian penyusutan", adjust: true, baris: []barisUji{
				{"5-3000", 6_000_000, 0}, {"1-4900", 0, 6_000_000},
			}},
		{kind: db.JournalKindNormal, tanggal: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC),
			ref: "JV-2026-12", memo: "Beban bunga tunai", baris: []barisUji{
				{"5-4000", 2_500_000, 0}, {"1-1000", 0, 2_500_000},
			}},
		{kind: db.JournalKindClosing, tanggal: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC),
			ref: "CLOSE-2026", memo: "Tutup buku 2026", periode: fy2026, baris: []barisUji{
				{"4-1000", 30_000_000, 0}, {"5-1000", 0, 12_000_000},
				{"5-2000", 0, 3_000_000}, {"5-3000", 0, 6_000_000},
				{"5-4000", 0, 2_500_000}, {"3-2000", 0, 6_500_000},
			}},
	}
}

// rowNumCells mengambil isi sel angka (kolom periode berjalan lebih dulu) pada
// baris tabel yang memuat needle, langsung dari HTML terender.
func rowNumCells(html, needle string) []string {
	i := strings.Index(html, needle)
	if i < 0 {
		panic("teks tidak ditemukan di HTML: " + needle)
	}
	start := strings.LastIndex(html[:i], "<tr")
	relEnd := strings.Index(html[i:], "</tr>")
	if start < 0 || relEnd < 0 {
		panic("baris tabel tidak ditemukan untuk: " + needle)
	}
	row := html[start : i+relEnd]
	re := regexp.MustCompile(`<td class="num[^"]*"[^>]*>([^<]*)</td>`)
	var out []string
	for _, m := range re.FindAllStringSubmatch(row, -1) {
		out = append(out, strings.TrimSpace(m[1]))
	}
	if len(out) == 0 {
		panic("tidak ada sel angka pada baris: " + needle)
	}
	return out
}

// keTotal mengubah satu baris ringkasan akun menjadi models.AccountTotal,
// sama dengan pemetaan di internal/handlers.
func keTotal(id int64, code, name, tipe, sub, psak string, d, c pgtype.Numeric) models.AccountTotal {
	return models.AccountTotal{
		ID: id, Code: code, Name: name, AccountType: tipe, SubType: sub, PsakLine: psak,
		DebitCents: models.NumericToCents(d), CreditCents: models.NumericToCents(c),
	}
}

// evidenceReports menjalankan seluruh bukti laporan Stage 2b.
func evidenceReports(ctx context.Context, pool *pgxpool.Pool, q *db.Queries, userID int64) {
	// ===== 1. Perusahaan, periode, akun, jurnal =====
	company, err := q.CreateCompany(ctx, db.CreateCompanyParams{
		Name: "PT Bukti Laporan", Slug: "pt-bukti-laporan",
		BaseCurrency: "IDR", UrlKey: "bukti2bsmk",
	})
	if err != nil {
		panic(err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO company_users (company_id, user_id, role) VALUES ($1, $2, 'owner')`,
		company.ID, userID); err != nil {
		panic(err)
	}

	fy2025, err := q.CreateFiscalPeriod(ctx, db.CreateFiscalPeriodParams{
		CompanyID: company.ID, Name: "Tahun Buku 2025",
		StartDate: tgl(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)),
		EndDate:   tgl(time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)),
		IsFirst:   true,
	})
	if err != nil {
		panic(err)
	}
	fy2026, err := q.CreateFiscalPeriod(ctx, db.CreateFiscalPeriodParams{
		CompanyID: company.ID, Name: "Tahun Buku 2026",
		StartDate: tgl(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		EndDate:   tgl(time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)),
	})
	if err != nil {
		panic(err)
	}

	akunIDs := map[string]int64{}
	for _, a := range akunBukti {
		var id int64
		err := pool.QueryRow(ctx, `
			INSERT INTO accounts (company_id, code, name, account_type, sub_type, normal_balance, psak_line)
			VALUES ($1, $2, $3, $4::account_type, $5, $6::normal_balance, $7) RETURNING id`,
			company.ID, a.code, a.name, a.tipe, a.sub, a.normal, a.psak).Scan(&id)
		if err != nil {
			panic(fmt.Errorf("akun %s: %w", a.code, err))
		}
		akunIDs[a.code] = id
	}

	for _, j := range skenarioBukti(fy2025.ID, fy2026.ID) {
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
		if j.kind == db.JournalKindNormal {
			_, err = q.CreateJournalWithEntries(ctx, db.CreateJournalWithEntriesParams{
				CompanyID: company.ID, JournalDate: tgl(j.tanggal), Reference: j.ref,
				Memo: j.memo, CreatedBy: userID, IsAdjusting: j.adjust,
				AccountIds: ids, Descriptions: desc, Debits: debits, Credits: credits,
			})
		} else {
			_, err = q.CreateSystemJournalWithEntries(ctx, db.CreateSystemJournalWithEntriesParams{
				CompanyID: company.ID, JournalDate: tgl(j.tanggal), Reference: j.ref,
				Memo: j.memo, CreatedBy: userID, Kind: j.kind,
				PeriodID:   pgtype.Int8{Int64: j.periode, Valid: true},
				AccountIds: ids, Descriptions: desc, Debits: debits, Credits: credits,
			})
		}
		if err != nil {
			panic(fmt.Errorf("jurnal %s: %w", j.ref, err))
		}
	}
	fmt.Println("PASS seed bukti: 2 periode, 16 akun, 17 jurnal (1 saldo awal + 2 penutup, 14 transaksi)")

	// ===== 2. Penutup angka: laporan vs Neraca, via query yang sama handler =====
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	openingRows, err := q.ListAccountTotalsOpening(ctx, db.ListAccountTotalsOpeningParams{
		CompanyID: company.ID, DateFrom: tgl(from), DateTo: tgl(to)})
	if err != nil {
		panic(err)
	}
	movRows, err := q.ListAccountTotalsMovements(ctx, db.ListAccountTotalsMovementsParams{
		CompanyID: company.ID, DateFrom: tgl(from), DateTo: tgl(to)})
	if err != nil {
		panic(err)
	}
	closRows, err := q.ListAccountTotalsClosing(ctx, db.ListAccountTotalsClosingParams{
		CompanyID: company.ID, DateFrom: tgl(from), DateTo: tgl(to)})
	if err != nil {
		panic(err)
	}
	bsRows, err := q.ListAccountTotalsByPeriod(ctx, db.ListAccountTotalsByPeriodParams{
		CompanyID: company.ID,
		DateFrom:  tgl(time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)),
		DateTo:    tgl(to),
	})
	if err != nil {
		panic(err)
	}

	toTotals := func(n int, get func(int) (int64, string, string, db.AccountType, string, string, pgtype.Numeric, pgtype.Numeric)) []models.AccountTotal {
		out := make([]models.AccountTotal, 0, n)
		for i := 0; i < n; i++ {
			id, code, name, tipe, sub, psak, d, c := get(i)
			out = append(out, keTotal(id, code, name, string(tipe), sub, psak, d, c))
		}
		return out
	}
	opening := toTotals(len(openingRows), func(i int) (int64, string, string, db.AccountType, string, string, pgtype.Numeric, pgtype.Numeric) {
		r := openingRows[i]
		return r.ID, r.Code, r.Name, r.AccountType, r.SubType, r.PsakLine, r.TotalDebit, r.TotalCredit
	})
	movements := toTotals(len(movRows), func(i int) (int64, string, string, db.AccountType, string, string, pgtype.Numeric, pgtype.Numeric) {
		r := movRows[i]
		return r.ID, r.Code, r.Name, r.AccountType, r.SubType, r.PsakLine, r.TotalDebit, r.TotalCredit
	})
	closing := toTotals(len(closRows), func(i int) (int64, string, string, db.AccountType, string, string, pgtype.Numeric, pgtype.Numeric) {
		r := closRows[i]
		return r.ID, r.Code, r.Name, r.AccountType, r.SubType, r.PsakLine, r.TotalDebit, r.TotalCredit
	})
	bsAll := toTotals(len(bsRows), func(i int) (int64, string, string, db.AccountType, string, string, pgtype.Numeric, pgtype.Numeric) {
		r := bsRows[i]
		return r.ID, r.Code, r.Name, r.AccountType, r.SubType, r.PsakLine, r.TotalDebit, r.TotalCredit
	})

	equity := models.BuildEquity(opening, movements, closing)
	cash := models.BuildCashFlow(opening, movements)

	var eqNeraca, belumDitutup, kasNeraca int64
	for _, a := range bsAll {
		switch a.AccountType {
		case models.TypeEquity:
			eqNeraca += a.CreditCents - a.DebitCents
		case models.TypeRevenue, models.TypeExpense:
			belumDitutup += a.CreditCents - a.DebitCents
		}
		if models.IsCashAccount(a) {
			kasNeraca += a.DebitCents - a.CreditCents
		}
	}

	uang := func(cents int64) string { return models.FormatRounded(cents, company.RoundingUnit) }
	cek(equity.TotalClosing == eqNeraca+belumDitutup,
		"ekuitas laporan (%s) == ekuitas Neraca (%s + laba belum ditutup %s)",
		uang(equity.TotalClosing), uang(eqNeraca), uang(belumDitutup))
	cek(equity.ProfitLoss() == rp(6_500_000),
		"laba periode Perubahan Ekuitas (%s) == 6.500.000", uang(equity.ProfitLoss()))
	cek(cash.CashEnd == kasNeraca,
		"kas akhir Arus Kas (%s) == kas dan setara kas Neraca (%s)", uang(cash.CashEnd), uang(kasNeraca))
	cek(cash.NetChange() == cash.CashEnd-cash.CashBeginning,
		"kenaikan kas (%s) == kas akhir - kas awal (%s)", uang(cash.NetChange()), uang(cash.CashEnd-cash.CashBeginning))
	cek(cash.CashBeginning == rp(65_000_000) && cash.CashEnd == rp(72_500_000),
		"kas awal (%s) dan kas akhir (%s) sesuai skenario", uang(cash.CashBeginning), uang(cash.CashEnd))

	// ===== 3. Render route dengan middleware asli =====
	h := handlers.New(q, false, "")
	router := chi.NewRouter()
	router.Group(func(r chi.Router) {
		r.Use(appmw.RequireAuth(q, false))
		r.Route("/companies/{companyKey}", func(r chi.Router) {
			r.Use(appmw.RequireCompany(q))
			r.Get("/reports/equity", h.ReportsEquityGet)
			r.Get("/reports/cashflow", h.ReportsCashFlowGet)
			r.Get("/reports/notes", h.ReportsNotesGet)
			r.Get("/reports/bs", h.ReportsBSGet)
		})
	})

	token := "token-bukti-laporan-2b"
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
	const qs = "?from=01/01/2026&to=31/12/2026"
	base := "/companies/" + company.UrlKey + "/reports"
	htmlEquity := get(base + "/equity" + qs)
	htmlCash := get(base + "/cashflow" + qs)
	htmlNotes := get(base + "/notes" + qs)
	htmlBS := get(base + "/bs" + qs)
	cek(strings.Contains(htmlEquity, "Laporan Perubahan Ekuitas"),
		"GET /reports/equity render (HTTP 200, judul ada)")
	cek(strings.Contains(htmlCash, "Laporan Arus Kas"),
		"GET /reports/cashflow render (HTTP 200, judul ada)")
	cek(strings.Contains(htmlNotes, "Catatan atas Laporan Keuangan"),
		"GET /reports/notes render (HTTP 200, judul ada)")
	cek(strings.Contains(htmlBS, "Jumlah Ekuitas"),
		"GET /reports/bs render (HTTP 200, judul ada)")

	// ===== 4. Tie-out angka yang benar-benar terender di HTML =====
	// Arus Kas: kas akhir = kas + bank di Neraca kolom berjalan, dan kas awal
	// = kas + bank di kolom pembanding (Neraca akhir 2025).
	kasAkhir := rowNumCells(htmlCash, "Kas dan setara kas pada akhir periode")[0]
	kasAwal := rowNumCells(htmlCash, "Kas dan setara kas pada awal periode")[0]
	kenaikan := rowNumCells(htmlCash, "Kenaikan (penurunan) neto kas dan setara kas")[0]
	kasBS := rowNumCells(htmlBS, `<span class="text-label">Kas</span>`)
	bankBS := rowNumCells(htmlBS, `<span class="text-label">Bank</span>`)
	cek(kasAkhir == uang(cash.CashEnd),
		"HTML Arus Kas: kas akhir (%s) == angka laporan (%s)", kasAkhir, uang(cash.CashEnd))
	cek(kasBS[0] == uang(27_500_000*100) && bankBS[0] == uang(45_000_000*100),
		"HTML Neraca: Kas (%s) + Bank (%s) kolom berjalan sesuai", kasBS[0], bankBS[0])
	cek(kasAwal == uang(cash.CashBeginning),
		"HTML Arus Kas: kas awal (%s) == angka laporan (%s)", kasAwal, uang(cash.CashBeginning))
	cek(kasBS[1] == uang(35_000_000*100) && bankBS[1] == uang(30_000_000*100),
		"HTML Neraca: Kas (%s) + Bank (%s) kolom pembanding = akhir 2025", kasBS[1], bankBS[1])
	cek(kenaikan == uang(cash.NetChange()),
		"HTML Arus Kas: kenaikan kas (%s) == kas akhir - kas awal", kenaikan)

	// Perubahan Ekuitas: baris "Saldo akhir periode" (kolom per komponen,
	// kolom laba belum ditutup, kolom jumlah) vs komponen ekuitas di Neraca.
	selAkhir := rowNumCells(htmlEquity, "Saldo akhir periode")
	if len(selAkhir) != 5 {
		panic(fmt.Sprintf("baris saldo akhir harus 5 sel, dapat %d: %v", len(selAkhir), selAkhir))
	}
	modalBS := rowNumCells(htmlBS, `<span class="text-label">Modal Disetor</span>`)[0]
	labaBS := rowNumCells(htmlBS, `<span class="text-label">Saldo Laba</span>`)[0]
	priveBS := rowNumCells(htmlBS, `<span class="text-label">Prive</span>`)[0]
	jumlahBS := rowNumCells(htmlBS, "Jumlah Ekuitas")[0]
	cek(selAkhir[0] == modalBS && selAkhir[1] == labaBS && selAkhir[2] == priveBS,
		"HTML: komponen ekuitas (Modal %s, Saldo Laba %s, Prive %s) == Neraca (%s, %s, %s)",
		selAkhir[0], selAkhir[1], selAkhir[2], modalBS, labaBS, priveBS)
	// Buku sudah ditutup (CLOSE-2026 memindahkan semua akun laba/rugi ke
	// saldo laba), tapi pajak kini PSAK 212 menambah akruan yang sengaja
	// belum dibukukan lewat jurnal: 6.500.000 x 22% = 1.430.000 belum ada
	// jurnalnya, jadi laba belum ditutup = 0 + BookedTax - CurrentTax =
	// -1.430.000, diimbangi baris liabilitas "Utang Pajak Penghasilan
	// (kini)" sehingga A = L + E tetap berimbang.
	cek(selAkhir[3] == uang(rp(-1_430_000)),
		"HTML: laba (rugi) belum ditutup pada saldo akhir = %s (buku ditutup, akruan pajak belum dibukukan)", selAkhir[3])
	cek(selAkhir[4] == jumlahBS,
		"HTML: saldo akhir periode (%s) == Jumlah Ekuitas Neraca (%s)", selAkhir[4], jumlahBS)

	// Pajak kini pada Neraca: baris liabilitas akruan tampil dengan angka
	// yang sama dengan beban pajak pada Laba Rugi, dan Neraca menutup.
	pajakBS := rowNumCells(htmlBS, "Utang Pajak Penghasilan (kini)")[0]
	jumlahAset := rowNumCells(htmlBS, "Jumlah Aset</td>")[0]
	jumlahLE := rowNumCells(htmlBS, "Jumlah Liabilitas dan Ekuitas")[0]
	cek(pajakBS == uang(rp(1_430_000)),
		"HTML Neraca: Utang Pajak Penghasilan (kini) = %s (6.500.000 x 22%%)", pajakBS)
	cek(jumlahLE == jumlahAset,
		"HTML Neraca: Jumlah Aset (%s) == Jumlah Liabilitas dan Ekuitas (%s)", jumlahAset, jumlahLE)
	cek(strings.Contains(htmlBS, "Berimbang") && !strings.Contains(htmlBS, "Selisih"),
		"HTML Neraca: baris Keseimbangan (A = L + E) menunjukkan Berimbang")

	// CaLK: kebijakan bernomor dengan referensi silang, estimasi, dan
	// pernyataan kepatuhan semuanya tampil.
	cek(strings.Contains(htmlNotes, `id="kebijakan-`) && strings.Contains(htmlNotes, `href="#kebijakan-`),
		"HTML CaLK: kebijakan akuntansi dengan referensi silang tampil")
	cek(strings.Contains(htmlNotes, "Sumber ketidakpastian estimasi"),
		"HTML CaLK: sumber ketidakpastian estimasi tampil")
	cek(strings.Contains(htmlNotes, "Pernyataan kepatuhan") && strings.Contains(htmlNotes, "PSAK 201"),
		"HTML CaLK: pernyataan kepatuhan dan dasar penyusunan tampil")

	// Simpan HTML bila diminta, sebagai bukti berkas.
	if dir := os.Getenv("SMOKE_SAVE_DIR"); dir != "" {
		for name, content := range map[string]string{
			"bukti-perubahan-ekuitas.html": htmlEquity,
			"bukti-arus-kas.html":          htmlCash,
			"bukti-calk.html":              htmlNotes,
			"bukti-neraca.html":            htmlBS,
		} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
				panic(err)
			}
		}
		fmt.Println("PASS HTML bukti disimpan di", dir)
	}
}

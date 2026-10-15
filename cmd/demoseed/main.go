// Command demoseed mengisi satu perusahaan dengan data demo PSAK 201:
// identitas laporan, dua tahun buku (2025 ditutup, 2026 berjalan), chart of
// accounts properti, saldo awal, jurnal 2025 + jurnal penutup, dan jurnal 2026.
//
// Contoh:
//
//	go run ./cmd/demoseed -slug pt-contoh -reset
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"accounting/internal/accounting"
	"accounting/internal/database/db"
	"accounting/internal/models"
)

// rp mengubah rupiah penuh menjadi sen.
func rp(v int64) int64 { return v * 100 }

type coa struct {
	code, name string
	typ        db.AccountType
	sub        string
	normal     db.NormalBalance
	role       string
}

type line struct {
	code          string
	debit, kredit int64
}

var daftarAkun = []coa{
	{"1-1000", "Kas", db.AccountTypeAsset, models.SubCurrentAsset, db.NormalBalanceDebit, ""},
	{"1-1010", "Bank BCA", db.AccountTypeAsset, models.SubCurrentAsset, db.NormalBalanceDebit, ""},
	{"1-1100", "Piutang Sewa", db.AccountTypeAsset, models.SubCurrentAsset, db.NormalBalanceDebit, ""},
	{"1-1200", "Sewa Dibayar Dimuka", db.AccountTypeAsset, models.SubCurrentAsset, db.NormalBalanceDebit, ""},
	{"1-1300", "PPN Masukan", db.AccountTypeAsset, models.SubCurrentAsset, db.NormalBalanceDebit, ""},
	{"1-1500", "Aset Tetap - Bangunan", db.AccountTypeAsset, models.SubNonCurrentAsset, db.NormalBalanceDebit, ""},
	{"1-1510", "Aset Tetap - Peralatan", db.AccountTypeAsset, models.SubNonCurrentAsset, db.NormalBalanceDebit, ""},
	{"1-1590", "Akumulasi Penyusutan", db.AccountTypeAsset, models.SubNonCurrentAsset, db.NormalBalanceCredit, ""},
	{"2-2000", "Utang Usaha", db.AccountTypeLiability, models.SubCurrentLiability, db.NormalBalanceCredit, ""},
	{"2-2010", "PPN Keluaran", db.AccountTypeLiability, models.SubCurrentLiability, db.NormalBalanceCredit, ""},
	{"2-2020", "Utang Pajak Penghasilan", db.AccountTypeLiability, models.SubCurrentLiability, db.NormalBalanceCredit, ""},
	{"2-2030", "Pendapatan Sewa Diterima Dimuka", db.AccountTypeLiability, models.SubCurrentLiability, db.NormalBalanceCredit, ""},
	{"2-2040", "Uang Jaminan Penyewa", db.AccountTypeLiability, models.SubCurrentLiability, db.NormalBalanceCredit, ""},
	{"2-2100", "Utang Bank Jangka Panjang", db.AccountTypeLiability, models.SubLongLiability, db.NormalBalanceCredit, ""},
	{"3-3000", "Modal Disetor", db.AccountTypeEquity, models.SubEquity, db.NormalBalanceCredit, ""},
	{"3-3010", "Saldo Laba", db.AccountTypeEquity, models.SubEquity, db.NormalBalanceCredit, models.SystemRoleRetainedEarnings},
	{"4-4000", "Pendapatan Sewa", db.AccountTypeRevenue, models.SubRevenue, db.NormalBalanceCredit, ""},
	{"4-4010", "Pendapatan Service Charge", db.AccountTypeRevenue, models.SubRevenue, db.NormalBalanceCredit, ""},
	{"4-4020", "Pendapatan Bunga", db.AccountTypeRevenue, models.SubRevenue, db.NormalBalanceCredit, ""},
	{"5-5000", "Beban Pemeliharaan", db.AccountTypeExpense, models.SubExpense, db.NormalBalanceDebit, ""},
	{"5-5010", "Beban Listrik dan Air", db.AccountTypeExpense, models.SubExpense, db.NormalBalanceDebit, ""},
	{"5-5020", "Beban Gaji", db.AccountTypeExpense, models.SubExpense, db.NormalBalanceDebit, ""},
	{"5-5030", "Beban Penyusutan", db.AccountTypeExpense, models.SubExpense, db.NormalBalanceDebit, ""},
	{"5-5040", "Beban Bunga", db.AccountTypeExpense, models.SubExpense, db.NormalBalanceDebit, ""},
	{"5-5050", "Beban Pajak Penghasilan", db.AccountTypeExpense, models.SubExpense, db.NormalBalanceDebit, ""},
}

func main() {
	slug := flag.String("slug", "pt-contoh", "slug perusahaan demo")
	dsnFlag := flag.String("dsn", "", "DSN PostgreSQL (default dari DATABASE_URL)")
	reset := flag.Bool("reset", false, "hapus jurnal, akun, dan periode perusahaan ini lebih dulu")
	create := flag.Bool("create", false, "buat perusahaan bila slug belum ada")
	namaBaru := flag.String("name", "PT Demo Kedua", "nama perusahaan bila dibuat baru")
	owner := flag.String("owner", "kuntegar", "username yang diberi akses owner bila perusahaan baru dibuat")
	flag.Parse()

	dsn := *dsnFlag
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		dsn = "postgres://aiagent@/accounting_dev?host=/var/run/postgresql"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatalf("gagal koneksi: %v", err)
	}
	defer pool.Close()
	q := db.New(pool)

	company, err := q.GetCompanyBySlug(ctx, *slug)
	if err != nil {
		if !*create {
			log.Fatalf("perusahaan %q tidak ditemukan: %v", *slug, err)
		}
		kunci, errK := models.NewCompanyKey()
		if errK != nil {
			log.Fatalf("gagal membuat kunci perusahaan: %v", errK)
		}
		company, err = q.CreateCompany(ctx, db.CreateCompanyParams{
			UrlKey:       kunci,
			Name:         *namaBaru,
			Slug:         *slug,
			BaseCurrency: "IDR",
		})
		if err != nil {
			log.Fatalf("gagal membuat perusahaan %q: %v", *slug, err)
		}
		var ownerID int64
		if err := pool.QueryRow(ctx, "SELECT id FROM users WHERE username = $1", *owner).Scan(&ownerID); err != nil {
			log.Fatalf("user %q tidak ditemukan: %v", *owner, err)
		}
		if err := q.GrantCompanyAccess(ctx, db.GrantCompanyAccessParams{
			CompanyID: company.ID,
			UserID:    ownerID,
			Role:      db.CompanyRoleOwner,
		}); err != nil {
			log.Fatalf("gagal memberi akses owner: %v", err)
		}
		fmt.Printf("Perusahaan baru dibuat: %s (id %d), owner %s.\n", company.Name, company.ID, *owner)
	}

	var jurnal int64
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM journals WHERE company_id = $1", company.ID).Scan(&jurnal); err != nil {
		log.Fatalf("gagal menghitung jurnal: %v", err)
	}
	if jurnal > 0 {
		if !*reset {
			log.Fatalf("perusahaan %s sudah punya %d jurnal. Jalankan dengan -reset bila memang ingin menimpanya.", company.Name, jurnal)
		}
		for _, stmt := range []string{
			"DELETE FROM journals WHERE company_id = $1",
			"DELETE FROM accounts WHERE company_id = $1",
			"DELETE FROM fiscal_periods WHERE company_id = $1",
		} {
			if _, err := pool.Exec(ctx, stmt, company.ID); err != nil {
				log.Fatalf("gagal membersihkan data demo: %v", err)
			}
		}
		fmt.Println("Data lama perusahaan demo dihapus (mode -reset).")
	}

	// 1. Identitas laporan keuangan (PSAK 201 par. 49-53).
	if err := q.UpdateCompanyProfile(ctx, db.UpdateCompanyProfileParams{
		ID:           company.ID,
		Name:         company.Name,
		Address:      "Jl. Contoh Raya No. 12, Jakarta Selatan",
		Npwp:         "01.234.567.8-012.000",
		RoundingUnit: models.RoundingThousand,
		EntityNote:   "Laporan keuangan entitas tersendiri",
	}); err != nil {
		log.Fatalf("gagal menyimpan identitas: %v", err)
	}

	// 2. Periode akuntansi: 2025 (periode pertama) dan 2026.
	p2025, err := q.CreateFiscalPeriod(ctx, db.CreateFiscalPeriodParams{
		CompanyID: company.ID,
		Name:      "Tahun Buku 2025",
		StartDate: tanggal(2025, 1, 1),
		EndDate:   tanggal(2025, 12, 31),
		IsFirst:   true,
	})
	if err != nil {
		log.Fatalf("gagal membuat periode 2025: %v", err)
	}
	p2026, err := q.CreateFiscalPeriod(ctx, db.CreateFiscalPeriodParams{
		CompanyID: company.ID,
		Name:      "Tahun Buku 2026",
		StartDate: tanggal(2026, 1, 1),
		EndDate:   tanggal(2026, 12, 31),
		IsFirst:   false,
	})
	if err != nil {
		log.Fatalf("gagal membuat periode 2026: %v", err)
	}

	// 3. Chart of accounts + peran sistem untuk akun Saldo Laba.
	accID := map[string]int64{}
	for _, a := range daftarAkun {
		acc, err := q.CreateAccount(ctx, db.CreateAccountParams{
			CompanyID:     company.ID,
			Code:          a.code,
			Name:          a.name,
			AccountType:   a.typ,
			SubType:       a.sub,
			NormalBalance: a.normal,
			SystemRole:    a.role,
			PsakLine:      models.GuessPsakLine(a.name, string(a.typ), a.sub, a.role),
		})
		if err != nil {
			log.Fatalf("gagal membuat akun %s: %v", a.code, err)
		}
		accID[a.code] = acc.ID
	}

	// 4. Pembuat jurnal: user pertama yang punya akses ke perusahaan ini.
	var userID int64
	if err := pool.QueryRow(ctx, `
		SELECT u.id FROM company_users cu
		JOIN users u ON u.id = cu.user_id
		WHERE cu.company_id = $1
		ORDER BY (cu.role = 'owner') DESC, u.id
		LIMIT 1`, company.ID).Scan(&userID); err != nil {
		log.Fatalf("perusahaan demo belum punya anggota: %v", err)
	}

	// 5. Saldo awal 1 Januari 2025 (jurnal khusus kind=opening).
	saldoAwal := []line{
		{"1-1000", rp(25_000_000), 0},
		{"1-1010", rp(750_000_000), 0},
		{"1-1100", rp(120_000_000), 0},
		{"1-1500", rp(6_000_000_000), 0},
		{"1-1510", rp(350_000_000), 0},
		{"1-1590", 0, rp(480_000_000)},
		{"2-2040", 0, rp(180_000_000)},
		{"2-2100", 0, rp(2_000_000_000)},
		{"3-3000", 0, rp(3_000_000_000)},
	}
	var d, k int64
	for _, l := range saldoAwal {
		d += l.debit
		k += l.kredit
	}
	if d > k {
		saldoAwal = append(saldoAwal, line{"3-3010", 0, d - k})
	} else if k > d {
		saldoAwal = append(saldoAwal, line{"3-3010", k - d, 0})
	}
	simpanSystem(ctx, q, company.ID, userID, tanggal(2025, 1, 1), "SALDO-AWAL",
		"Saldo awal per 1 Januari 2025", db.JournalKindOpening, p2025.ID, accID, saldoAwal)
	fmt.Println("Saldo awal 2025 tersimpan.")

	// 6. Jurnal 2025: sewa bulanan, service charge, beban, penyusutan, bunga, pajak.
	for bulan := 1; bulan <= 12; bulan++ {
		tgl := tanggal(2025, bulan, 5)
		simpan(ctx, q, company.ID, userID, tgl, ref("J2025", bulan, "SEWA"), "Penerimaan sewa bulanan", accID, []line{
			{"1-1010", rp(150_000_000), 0},
			{"4-4000", 0, rp(150_000_000)},
		})
		simpan(ctx, q, company.ID, userID, tgl, ref("J2025", bulan, "SC"), "Tagihan service charge", accID, []line{
			{"1-1100", rp(45_000_000), 0},
			{"4-4010", 0, rp(45_000_000)},
		})
		simpan(ctx, q, company.ID, userID, tanggal(2025, bulan, 10), ref("J2025", bulan, "LISTRIK"), "Tagihan listrik dan air", accID, []line{
			{"5-5010", rp(18_000_000), 0},
			{"1-1010", 0, rp(18_000_000)},
		})
		simpan(ctx, q, company.ID, userID, tanggal(2025, bulan, 25), ref("J2025", bulan, "GAJI"), "Gaji staf properti", accID, []line{
			{"5-5020", rp(42_000_000), 0},
			{"1-1010", 0, rp(42_000_000)},
		})
		simpan(ctx, q, company.ID, userID, tanggal(2025, bulan, 28), ref("J2025", bulan, "SUSUT"), "Penyusutan bulanan", accID, []line{
			{"5-5030", rp(14_000_000), 0},
			{"1-1590", 0, rp(14_000_000)},
		})
		simpan(ctx, q, company.ID, userID, tanggal(2025, bulan, 28), ref("J2025", bulan, "BUNGA"), "Bunga utang bank", accID, []line{
			{"5-5040", rp(16_000_000), 0},
			{"1-1010", 0, rp(16_000_000)},
		})
	}
	simpan(ctx, q, company.ID, userID, tanggal(2025, 6, 20), "J2025-PEMELIHARAAN", "Perbaikan lift dan taman", accID, []line{
		{"5-5000", rp(85_000_000), 0},
		{"2-2000", 0, rp(85_000_000)},
	})
	simpan(ctx, q, company.ID, userID, tanggal(2025, 12, 20), "J2025-PPH", "Beban pajak penghasilan 2025", accID, []line{
		{"5-5050", rp(90_000_000), 0},
		{"2-2020", 0, rp(90_000_000)},
	})
	fmt.Println("Jurnal 2025 tersimpan.")

	// 7. Tutup buku 2025: jurnal penutup ke Saldo Laba, lalu periode dikunci.
	rows, err := q.ListAccountTotalsByPeriod(ctx, db.ListAccountTotalsByPeriodParams{
		CompanyID: company.ID,
		DateFrom:  p2025.StartDate,
		DateTo:    p2025.EndDate,
	})
	if err != nil {
		log.Fatalf("gagal membaca saldo 2025: %v", err)
	}
	closing, net := accounting.ClosingLines(rows, accID["3-3010"])
	if len(closing) > 0 {
		simpanSystemByAkun(ctx, q, company.ID, userID, p2025.EndDate,
			"PENUTUP-"+fmt.Sprint(p2025.ID),
			fmt.Sprintf("Jurnal penutup 2025 (laba periode %s)", models.FormatCents(net)),
			db.JournalKindClosing, p2025.ID, closing)
	}
	if _, err := q.CloseFiscalPeriod(ctx, db.CloseFiscalPeriodParams{
		ID: p2025.ID, CompanyID: company.ID,
		ClosedBy: pgtype.Int8{Int64: userID, Valid: true},
	}); err != nil {
		log.Fatalf("gagal menutup periode 2025: %v", err)
	}
	fmt.Printf("Tahun buku 2025 ditutup, laba periode %s dipindahkan ke Saldo Laba.\n", models.FormatCents(net))

	// 8. Jurnal 2026 (Januari sampai September) dengan periode masih terbuka.
	for bulan := 1; bulan <= 9; bulan++ {
		tgl := tanggal(2026, bulan, 5)
		simpan(ctx, q, company.ID, userID, tgl, ref("J2026", bulan, "SEWA"), "Penerimaan sewa bulanan", accID, []line{
			{"1-1010", rp(180_000_000), 0},
			{"4-4000", 0, rp(180_000_000)},
		})
		simpan(ctx, q, company.ID, userID, tgl, ref("J2026", bulan, "SC"), "Tagihan service charge", accID, []line{
			{"1-1100", rp(52_000_000), 0},
			{"4-4010", 0, rp(52_000_000)},
		})
		simpan(ctx, q, company.ID, userID, tanggal(2026, bulan, 10), ref("J2026", bulan, "LISTRIK"), "Tagihan listrik dan air", accID, []line{
			{"5-5010", rp(20_000_000), 0},
			{"1-1010", 0, rp(20_000_000)},
		})
		simpan(ctx, q, company.ID, userID, tanggal(2026, bulan, 25), ref("J2026", bulan, "GAJI"), "Gaji staf properti", accID, []line{
			{"5-5020", rp(46_000_000), 0},
			{"1-1010", 0, rp(46_000_000)},
		})
		simpan(ctx, q, company.ID, userID, tanggal(2026, bulan, 28), ref("J2026", bulan, "SUSUT"), "Penyusutan bulanan", accID, []line{
			{"5-5030", rp(14_000_000), 0},
			{"1-1590", 0, rp(14_000_000)},
		})
		simpan(ctx, q, company.ID, userID, tanggal(2026, bulan, 28), ref("J2026", bulan, "BUNGA"), "Bunga utang bank", accID, []line{
			{"5-5040", rp(15_000_000), 0},
			{"1-1010", 0, rp(15_000_000)},
		})
	}
	simpan(ctx, q, company.ID, userID, tanggal(2026, 3, 15), "J2026-JAMINAN", "Uang jaminan penyewa baru", accID, []line{
		{"1-1010", rp(60_000_000), 0},
		{"2-2040", 0, rp(60_000_000)},
	})
	simpan(ctx, q, company.ID, userID, tanggal(2026, 8, 1), "J2026-DIMUKA", "Sewa diterima dimuka periode berikutnya", accID, []line{
		{"1-1010", rp(90_000_000), 0},
		{"2-2030", 0, rp(90_000_000)},
	})
	simpan(ctx, q, company.ID, userID, tanggal(2026, 9, 20), "J2026-PPH", "Beban pajak penghasilan sampai September 2026", accID, []line{
		{"5-5050", rp(75_000_000), 0},
		{"2-2020", 0, rp(75_000_000)},
	})
	// Jurnal penyesuaian akhir periode (is_adjusting = true).
	simpanPenyesuaian(ctx, q, company.ID, userID, tanggal(2026, 9, 30), "ADJ2026-LISTRIK",
		"Beban listrik September terutang", accID, []line{
			{"5-5010", rp(12_000_000), 0},
			{"2-2000", 0, rp(12_000_000)},
		})
	simpanPenyesuaian(ctx, q, company.ID, userID, tanggal(2026, 9, 30), "ADJ2026-SUSUT",
		"Penyesuaian penyusutan peralatan", accID, []line{
			{"5-5030", rp(5_000_000), 0},
			{"1-1590", 0, rp(5_000_000)},
		})
	simpanPenyesuaian(ctx, q, company.ID, userID, tanggal(2026, 9, 30), "ADJ2026-BUNGA",
		"Pendapatan bunga belum ditagih", accID, []line{
			{"1-1100", rp(3_500_000), 0},
			{"4-4020", 0, rp(3_500_000)},
		})
	fmt.Println("Jurnal 2026 dan 3 jurnal penyesuaian tersimpan (periode masih terbuka).")
	fmt.Printf("SELESAI. Perusahaan: %s (id %d), periode: %s & %s\n",
		company.Name, company.ID, p2025.Name, p2026.Name)
}

// simpan menyimpan jurnal biasa (kind=normal).
func simpan(ctx context.Context, q *db.Queries, companyID, userID int64, tgl pgtype.Date,
	ref, memo string, accID map[string]int64, lines []line) {
	params := db.CreateJournalWithEntriesParams{
		CompanyID:    companyID,
		JournalDate:  tgl,
		Reference:    ref,
		Memo:         memo,
		CreatedBy:    userID,
		AccountIds:   make([]int64, len(lines)),
		Descriptions: make([]string, len(lines)),
		Debits:       make([]pgtype.Numeric, len(lines)),
		Credits:      make([]pgtype.Numeric, len(lines)),
	}
	for i, l := range lines {
		params.AccountIds[i] = accID[l.code]
		params.Descriptions[i] = memo
		params.Debits[i], _ = models.CentsToNumeric(l.debit)
		params.Credits[i], _ = models.CentsToNumeric(l.kredit)
	}
	if _, err := q.CreateJournalWithEntries(ctx, params); err != nil {
		log.Fatalf("gagal menyimpan jurnal %s: %v", ref, err)
	}
}

// simpanPenyesuaian menyimpan jurnal bertanda penyesuaian (is_adjusting).
func simpanPenyesuaian(ctx context.Context, q *db.Queries, companyID, userID int64, tgl pgtype.Date,
	ref, memo string, accID map[string]int64, lines []line) {
	params := db.CreateJournalWithEntriesParams{
		CompanyID:    companyID,
		JournalDate:  tgl,
		Reference:    ref,
		Memo:         memo,
		CreatedBy:    userID,
		IsAdjusting:  true,
		AccountIds:   make([]int64, len(lines)),
		Descriptions: make([]string, len(lines)),
		Debits:       make([]pgtype.Numeric, len(lines)),
		Credits:      make([]pgtype.Numeric, len(lines)),
	}
	for i, l := range lines {
		params.AccountIds[i] = accID[l.code]
		params.Descriptions[i] = memo
		params.Debits[i], _ = models.CentsToNumeric(l.debit)
		params.Credits[i], _ = models.CentsToNumeric(l.kredit)
	}
	if _, err := q.CreateJournalWithEntries(ctx, params); err != nil {
		log.Fatalf("gagal menyimpan jurnal penyesuaian %s: %v", ref, err)
	}
}

// simpanSystem menyimpan jurnal sistem (saldo awal atau penutup) dari daftar
// akun berkode.
func simpanSystem(ctx context.Context, q *db.Queries, companyID, userID int64, tgl pgtype.Date,
	ref, memo string, kind db.JournalKind, periodID int64, accID map[string]int64, lines []line) {
	byAkun := make([]accounting.Line, len(lines))
	for i, l := range lines {
		byAkun[i] = accounting.Line{
			AccountID:   accID[l.code],
			Description: memo,
			DebitCents:  l.debit,
			CreditCents: l.kredit,
		}
	}
	simpanSystemByAkun(ctx, q, companyID, userID, tgl, ref, memo, kind, periodID, byAkun)
}

// simpanSystemByAkun menyimpan jurnal sistem dari baris yang sudah memuat
// account id (dipakai jurnal penutup hasil perhitungan accounting).
func simpanSystemByAkun(ctx context.Context, q *db.Queries, companyID, userID int64, tgl pgtype.Date,
	ref, memo string, kind db.JournalKind, periodID int64, lines []accounting.Line) {
	params := db.CreateSystemJournalWithEntriesParams{
		CompanyID:    companyID,
		JournalDate:  tgl,
		Reference:    ref,
		Memo:         memo,
		CreatedBy:    userID,
		Kind:         kind,
		PeriodID:     pgtype.Int8{Int64: periodID, Valid: true},
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
	if _, err := q.CreateSystemJournalWithEntries(ctx, params); err != nil {
		log.Fatalf("gagal menyimpan jurnal sistem %s: %v", ref, err)
	}
}

func ref(prefix string, bulan int, kode string) string {
	return fmt.Sprintf("%s-%02d-%s", prefix, bulan, kode)
}

func tanggal(y int, m int, d int) pgtype.Date {
	return pgtype.Date{Time: time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC), Valid: true, InfinityModifier: pgtype.Finite}
}

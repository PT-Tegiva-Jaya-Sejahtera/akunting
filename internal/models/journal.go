// Package models memuat struktur domain dan helper murni (tanpa dependensi
// HTTP/database) untuk alur transaksi jurnal.
package models

import (
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

// JournalLine adalah satu baris debit/kredit pada form jurnal.
// DebitStr dan CreditStr adalah teks mentah dari input user.
type JournalLine struct {
	AccountID   int64
	Description string
	DebitStr    string
	CreditStr   string
}

// JournalForm adalah isi form jurnal untuk ditampilkan ulang bila validasi gagal.
type JournalForm struct {
	Date        string // format YYYY-MM-DD
	Reference   string
	Memo        string
	IsAdjusting bool // jurnal penyesuaian, bukan jurnal umum
	PartyID     int64  // pihak (pelanggan/supplier) dari tabel parties; 0 = tanpa pihak
	DueDate     string // tanggal jatuh tempo, format YYYY-MM-DD; kosong = tidak ada
	Lines       []JournalLine
	Error       string
}

// ReportLine adalah satu baris laporan keuangan. AmountCents adalah periode
// berjalan; PriorAmountCents adalah periode pembanding (PSAK 201 par. 38).
type ReportLine struct {
	Code             string
	Name             string
	AmountCents      int64
	PriorAmountCents int64
}

// ReportView adalah data lengkap halaman laporan (laba rugi + neraca),
// dikelompokkan mengikuti klasifikasi PSAK 201.
type ReportView struct {
	DateFrom string
	DateTo   string

	// Identitas laporan keuangan (PSAK 201 par. 49-53).
	CompanyName       string
	CompanyAddr       string
	CompanyNpwp       string
	EntityNote        string
	BaseCurrency      string
	RoundingUnit      string
	RoundingLabel     string
	PeriodName        string
	PeriodStatus      string
	PeriodStatusLabel string
	DateFromLabel     string
	DateToLabel       string
	CustomRange       bool
	Periods           []PeriodView
	SelectedPeriodID  int64
	// Mode halaman laporan: "" keduanya, "pl" hanya laba rugi, "bs" hanya neraca.
	Mode string

	// Kolom pembanding (PSAK 201 par. 38). PriorPeriodName menjadi kepala
	// kolom kedua; HasComparative false berarti periode pembanding tidak ada.
	HasComparative  bool
	PriorPeriodName string
	ComparePeriodID int64

	// Laporan Laba Rugi (PSAK 201 par. 82): biaya keuangan dan beban pajak
	// disajikan sebagai baris tersendiri, bukan dicampur ke beban lain-lain.
	Revenue               []ReportLine
	TotalRevenue          int64
	OperatingExpense      []ReportLine // beban menurut sifat (par. 99-105)
	TotalOperatingExpense int64
	OperatingProfit       int64        // pendapatan - beban operasional
	FinanceCost           []ReportLine // biaya keuangan (par. 82 huruf b)
	TotalFinanceCost      int64
	ProfitBeforeTax       int64
	TaxExpense            []ReportLine // beban pajak (par. 82 huruf d)
	TotalTaxExpense       int64
	NetIncome             int64 // laba periode berjalan

	// Penghasilan komprehensif lain (PSAK 201 par. 82A): mutasi akun
	// ber-accounts.oci_kind selama periode, dipilah dua kelompok sesuai
	// par. 82A (akan vs tidak akan direklasifikasi ke laba rugi).
	OCIRecyclable    []ReportLine // akan direklasifikasi ke laba rugi
	OCINonRecyclable []ReportLine // tidak akan direklasifikasi ke laba rugi
	TotalOCIRecyclable    int64
	TotalOCINonRecyclable int64
	TotalOCI              int64 // jumlah penghasilan komprehensif lain
	ComprehensiveIncome   int64 // laba (rugi) + jumlah OCI

	// Kolom pembanding untuk total laba rugi dan penghasilan komprehensif.
	PriorTotalRevenue          int64
	PriorTotalOperatingExpense int64
	PriorOperatingProfit       int64
	PriorTotalFinanceCost      int64
	PriorProfitBeforeTax       int64
	PriorTotalTaxExpense       int64
	PriorNetIncome             int64
	PriorTotalOCIRecyclable    int64
	PriorTotalOCINonRecyclable int64
	PriorTotalOCI              int64
	PriorComprehensiveIncome   int64

	CurrentAssets              []ReportLine
	TotalCurrentAssets         int64
	NonCurrentAssets           []ReportLine
	TotalNonCurrentAssets      int64
	TotalAssets                int64
	CurrentLiabilities         []ReportLine
	TotalCurrentLiabilities    int64
	NonCurrentLiabilities      []ReportLine
	TotalNonCurrentLiabilities int64
	TotalLiabilities           int64
	Equity                     []ReportLine
	TotalEquity                int64
	UnclosedProfit             int64 // laba (rugi) periode yang belum ditutup
	TotalLiabEquity            int64 // liabilitas + ekuitas + laba belum ditutup

	// Kolom pembanding untuk total laporan posisi keuangan.
	PriorTotalCurrentAssets         int64
	PriorTotalNonCurrentAssets      int64
	PriorTotalAssets                int64
	PriorTotalCurrentLiabilities    int64
	PriorTotalNonCurrentLiabilities int64
	PriorTotalLiabilities           int64
	PriorTotalEquity                int64
	PriorUnclosedProfit             int64
	PriorTotalLiabEquity            int64
}

// Money menampilkan nilai sen memakai satuan pembulatan entitas.
func (v ReportView) Money(cents int64) string {
	return FormatRounded(cents, v.RoundingUnit)
}

// PriorMoney menampilkan nilai kolom pembanding; tanda "-" bila laporan tidak
// memiliki periode pembanding (mis. periode pertama entitas).
func (v ReportView) PriorMoney(cents int64) string {
	if !v.HasComparative {
		return "-"
	}
	return FormatRounded(cents, v.RoundingUnit)
}

// ConsolidationLabel menerjemahkan catatan jenis laporan menjadi pernyataan
// terpisah vs konsolidasi yang wajib disebut (PSAK 201 par. 49 huruf b).
func (v ReportView) ConsolidationLabel() string {
	if strings.TrimSpace(v.EntityNote) != "" {
		return v.EntityNote
	}
	return "Laporan keuangan entitas tersendiri (terpisah, bukan konsolidasi)"
}

// ErrMoneyFormat dipakai ParseMoney untuk input yang tidak valid.
var ErrMoneyFormat = errors.New("format uang tidak valid")

// ParseMoney mengubah teks uang menjadi nilai dalam sen (cent).
// Menerima "1000000.50" atau format Indonesia "1.000.000,50".
// String kosong dianggap 0. Angka negatif dan lebih dari 2 desimal ditolak.
func ParseMoney(s string) (int64, error) {
	s = strings.TrimSpace(strings.ReplaceAll(s, " ", ""))
	if s == "" {
		return 0, nil
	}
	if strings.ContainsAny(s, "-") {
		return 0, fmt.Errorf("%w: nilai negatif tidak diizinkan", ErrMoneyFormat)
	}

	var intPart, fracPart string
	if strings.Contains(s, ",") {
		// Format Indonesia: titik = ribuan, koma = desimal.
		s = strings.ReplaceAll(s, ".", "")
		s = strings.ReplaceAll(s, ",", ".")
	}

	parts := strings.Split(s, ".")
	if len(parts) > 2 {
		return 0, fmt.Errorf("%w: terlalu banyak tanda desimal", ErrMoneyFormat)
	}
	intPart = parts[0]
	if len(parts) == 2 {
		fracPart = parts[1]
	}
	if intPart == "" {
		intPart = "0"
	}
	if len(fracPart) > 2 {
		return 0, fmt.Errorf("%w: maksimal 2 angka desimal", ErrMoneyFormat)
	}
	for len(fracPart) < 2 {
		fracPart += "0"
	}
	if !allDigits(intPart) || !allDigits(fracPart) {
		return 0, fmt.Errorf("%w: hanya angka yang diizinkan", ErrMoneyFormat)
	}
	if len(intPart) > 17 {
		return 0, fmt.Errorf("%w: nilai terlalu besar", ErrMoneyFormat)
	}

	bulanan, ok := new(big.Int).SetString(intPart, 10)
	if !ok {
		return 0, ErrMoneyFormat
	}
	sen, ok := new(big.Int).SetString(fracPart, 10)
	if !ok {
		return 0, ErrMoneyFormat
	}
	bulanan.Mul(bulanan, big.NewInt(100))
	bulanan.Add(bulanan, sen)
	if !bulanan.IsInt64() {
		return 0, fmt.Errorf("%w: nilai terlalu besar", ErrMoneyFormat)
	}
	return bulanan.Int64(), nil
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// CentsToNumeric membungkus nilai sen menjadi pgtype.Numeric untuk sqlc.
func CentsToNumeric(c int64) (pgtype.Numeric, error) {
	var n pgtype.Numeric
	err := n.Scan(fmt.Sprintf("%d.%02d", c/100, mod100(c)))
	return n, err
}

// NumericToCents mengubah pgtype.Numeric dari database menjadi nilai sen.
// Rumus: nilai = Int * 10^Exp, sehingga sen = Int * 10^(Exp+2).
func NumericToCents(n pgtype.Numeric) int64 {
	if !n.Valid || n.Int == nil {
		return 0
	}
	v := new(big.Int).Set(n.Int)
	exp := int(n.Exp) + 2
	switch {
	case exp > 0:
		v.Mul(v, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(exp)), nil))
	case exp < 0:
		v.Div(v, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-exp)), nil))
	}
	return v.Int64()
}

// FormatCents menampilkan nilai sen gaya Indonesia: 100000050 -> "1.000.000,50".
func FormatCents(c int64) string {
	sign := ""
	if c < 0 {
		sign = "-"
		c = -c
	}
	whole := c / 100
	frac := mod100(c)

	wholeStr := fmt.Sprintf("%d", whole)
	var b strings.Builder
	for i, r := range wholeStr {
		if i > 0 && (len(wholeStr)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	return sign + b.String() + fmt.Sprintf(",%02d", frac)
}

func mod100(c int64) int64 {
	m := c % 100
	if m < 0 {
		m += 100
	}
	return m
}

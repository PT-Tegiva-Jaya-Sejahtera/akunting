package models

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Rekonsiliasi kas/bank (Stage B): parsing CSV mutasi bank (dibaca lewat modul
// internal/exchange), perencanaan pencocokan otomatis, dan penyusunan jurnal
// selisih kurs. Semua murni tanpa akses database supaya mudah diuji dan bisa
// dipakai bersama handler web dan smoketest.

// ReferencePrefixPrediksi jurnal selisih rekonsiliasi. Jurnal dengan awalan ini
// tidak pernah dikandidatkan lagi sebagai baris yang dicocokkan.
const ReferencePrefixRekonsiliasi = "REC-"

// StatementInput adalah satu baris mutasi bank hasil parsing CSV.
type StatementInput struct {
	Date         time.Time
	Description  string
	Reference    string
	InCents      int64 // uang masuk (kredit saldo rekening)
	OutCents     int64 // uang keluar (debit saldo rekening)
	BalanceCents int64
	HasBalance   bool
}

// Alias kolom CSV yang diterima. Konvensi rekening bank Indonesia: kredit =
// dana masuk, debit = dana keluar, jadi "kredit" dipetakan ke masuk.
var statementColumns = []struct {
	Canonical string
	Aliases   []string
}{
	{"tanggal", []string{"tanggal", "tgl", "date"}},
	{"keterangan", []string{"keterangan", "uraian", "deskripsi", "description", "memo", "keterangan transaksi", "keterangan_mutasi"}},
	{"masuk", []string{"masuk", "kredit", "deposit", "in"}},
	{"keluar", []string{"keluar", "debit", "withdrawal", "out"}},
	{"saldo", []string{"saldo", "balance"}},
	{"ref", []string{"ref", "referensi", "reference", "nomor", "no bukti"}},
}

// findColumn mencari indeks kolom pertama yang cocok dengan alias. Bila tidak
// ada, kembalikan -1.
func findColumn(header []string, canonical string) int {
	var spec *struct {
		Canonical string
		Aliases   []string
	}
	for i := range statementColumns {
		if statementColumns[i].Canonical == canonical {
			spec = &statementColumns[i]
			break
		}
	}
	for i, h := range header {
		n := normalisasiKolom(h)
		for _, a := range spec.Aliases {
			if n == a {
				return i
			}
		}
	}
	return -1
}

// normalisasiKolom merapikan nama header: kecilkan huruf, buang spasi dan strip.
func normalisasiKolom(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r == ' ', r == '-', r == '_':
			continue
		case r < 'a' || r > 'z':
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ParseStatementRows mengubah baris CSV mutasi bank (sudah dibaca
// exchange.Read) menjadi input statement. Kolom wajib: tanggal, keterangan,
// dan salah satu masuk/keluar. Muatan hasil: baris valid, dan daftar pesan
// error per nomor baris data (baris data pertama = 1).
func ParseStatementRows(header []string, rows [][]string) ([]StatementInput, []string) {
	iDate := findColumn(header, "tanggal")
	iDesc := findColumn(header, "keterangan")
	iIn := findColumn(header, "masuk")
	iOut := findColumn(header, "keluar")
	iBal := findColumn(header, "saldo")
	iRef := findColumn(header, "ref")

	var errs []string
	missing := []string{}
	if iDate < 0 {
		missing = append(missing, "tanggal")
	}
	if iDesc < 0 {
		missing = append(missing, "keterangan")
	}
	if iIn < 0 && iOut < 0 {
		missing = append(missing, "masuk atau keluar")
	}
	if len(missing) > 0 {
		return nil, []string{
			"Kolom wajib tidak ditemukan: " + strings.Join(missing, ", ") +
				". Gunakan berkas templat mutasi bank.",
		}
	}

	valid := make([]StatementInput, 0, len(rows))
	addErr := func(no int, msg string) {
		if len(errs) < 20 { // batasi supaya halaman preview tidak meledak
			errs = append(errs, fmt.Sprintf("Baris %d: %s", no, msg))
		}
	}
	for ri, row := range rows {
		no := ri + 1
		get := func(i int) string {
			if i < 0 || i >= len(row) {
				return ""
			}
			return strings.TrimSpace(row[i])
		}
		var in StatementInput
		t, err := ParseDateID(get(iDate))
		if err != nil {
			t, err = time.Parse("2006-01-02", get(iDate))
		}
		if err != nil {
			addErr(no, "tanggal harus berformat dd/mm/yyyy atau yyyy-mm-dd")
			continue
		}
		in.Date = t
		in.Description = get(iDesc)
		if in.Description == "" {
			in.Description = get(iRef)
		}
		in.Reference = get(iRef)
		inC, errIn := ParseMoney(get(iIn))
		outC, errOut := ParseMoney(get(iOut))
		if errIn != nil || errOut != nil {
			addErr(no, "angka masuk/keluar tidak dapat dibaca")
			continue
		}
		in.InCents, in.OutCents = inC, outC
		if (in.InCents > 0) == (in.OutCents > 0) {
			addErr(no, "isi tepat salah satu dari masuk atau keluar, bukan keduanya")
			continue
		}
		if bc, errB := ParseMoney(get(iBal)); errB == nil && get(iBal) != "" {
			in.BalanceCents = bc
			in.HasBalance = true
		}
		valid = append(valid, in)
	}
	return valid, errs
}

// EntryKey adalah kandidat baris ledger untuk pencocokan otomatis.
type EntryKey struct {
	ID          int64
	Date        time.Time
	DebitCents  int64 // uang masuk menurut kas (saldo normal akun kas)
	CreditCents int64
}

// LineKey adalah baris mutasi bank untuk pencocokan otomatis.
type LineKey struct {
	ID       int64
	Date     time.Time
	InCents  int64
	OutCents int64
}

// BuildAutoMatches merencanakan pencocokan otomatis satu-ke-satu: baris mutasi
// dan baris ledger yang tanggal DAN jumlahnya sama persis dijumlahkan
// (masuk = debit akun kas, keluar = credit). Baris yang sama tidak dipakai dua
// kali. Daftar hasil: slice pasangan {lineID, entryID}.
func BuildAutoMatches(lines []LineKey, entries []EntryKey) [][2]int64 {
	used := make(map[int64]bool)
	var pairs [][2]int64
	for _, l := range lines {
		for _, e := range entries {
			if used[e.ID] {
				continue
			}
			if sameDay(l.Date, e.Date) && l.InCents == e.DebitCents && l.InCents > 0 ||
				sameDay(l.Date, e.Date) && l.OutCents == e.CreditCents && l.OutCents > 0 {
				used[e.ID] = true
				pairs = append(pairs, [2]int64{l.ID, e.ID})
				break
			}
		}
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i][0] < pairs[j][0] })
	return pairs
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// BuildSelisihLines menyusun baris jurnal selisih rekonsiliasi. bookSigned dan
// stmtSigned memakai tanda kas: positif = uang masuk ke rekening, negatif =
// uang keluar. Selisih positif (bank lebih besar dari buku): debit akun kas,
// credit akun selisih (kurs) di sisi lain; selisih negatif: kebalikannya.
// Bila tidak ada selisih, lines nil.
func BuildSelisihLines(bankAccountID, selisihAccountID int64, bankDesc, selisihDesc string, bookSigned, stmtSigned int64) (int64, []SelisihLine) {
	bank := func(sign int64, amount int64) SelisihLine {
		if sign > 0 {
			return SelisihLine{AccountID: bankAccountID, Description: bankDesc, DebitCents: amount}
		}
		return SelisihLine{AccountID: bankAccountID, Description: bankDesc, CreditCents: amount}
	}
	kurs := func(sign int64, amount int64) SelisihLine {
		if sign > 0 {
			return SelisihLine{AccountID: selisihAccountID, Description: selisihDesc, CreditCents: amount}
		}
		return SelisihLine{AccountID: selisihAccountID, Description: selisihDesc, DebitCents: amount}
	}

	diff := stmtSigned - bookSigned
	if diff == 0 {
		return 0, nil
	}
	sign := int64(1)
	amp := diff
	if diff < 0 {
		sign, amp = -1, -diff
	}
	return amp, []SelisihLine{bank(sign, amp), kurs(sign, amp)}
}

// SelisihLine adalah satu baris jurnal selisih hasil rekonsiliasi.
type SelisihLine struct {
	AccountID   int64
	Description string
	DebitCents  int64
	CreditCents int64
}

// SignedAmount mengubah jumlah masuk/keluar menjadi tanda kas: masuk positif,
// keluar negatif.
func SignedAmount(inCents, outCents int64) int64 {
	return inCents - outCents
}

// ============================================================
// Tampilan (view) halaman bank & rekonsiliasi
// ============================================================

// StatusLabelStatement menerjemahkan status baris mutasi untuk tampilan.
func StatusLabelStatement(s string) string {
	switch s {
	case "unmatched":
		return "Belum cocok"
	case "matched":
		return "Cocok"
	case "adjusted":
		return "Cocok (dengan selisih)"
	default:
		return s
	}
}

// BankAccountRow adalah satu rekening bank pada halaman daftar.
type BankAccountRow struct {
	ID             int64
	AccountID      int64
	AccountCode    string
	AccountName    string
	BankName       string
	AccountNumber  string
	Currency       string
	UnmatchedCount int64
	UnmatchedTotal int64 // sen, jumlah belum cocok (masuk dikurangi keluar)
}

// BankView adalah data halaman daftar rekening bank.
type BankView struct {
	Accounts      []BankAccountRow
	COA           []BankCoaOption // akun kas aktif untuk form tambah rekening
	RoundingUnit  string
	RoundingLabel string
	CanManage     bool
}

// BankCoaOption adalah pilihan akun COA pada form tambah rekening bank.
type BankCoaOption struct {
	ID   int64
	Code string
	Name string
}

// Money menampilkan nilai sen memakai satuan pembulatan entitas.
func (v BankView) Money(cents int64) string {
	return FormatRounded(cents, v.RoundingUnit)
}

// CandidateOption adalah baris ledger yang masih bebas untuk sebuah baris
// mutasi yang belum cocok, ditampilkan sebagai pilihan di form pencocokan.
type CandidateOption struct {
	EntryID     int64
	DateLabel   string
	Reference   string
	Description string
	AmountCents int64 // tanda kas: debit dikurangi credit
	DiffCents   int64 // selisih terhadap jumlah mutasi (mutasi dikurangi buku)
}

// StatementRow adalah satu baris mutasi bank pada halaman rekonsiliasi.
type StatementRow struct {
	ID                int64
	DateLabel         string
	Description       string
	Reference         string
	SourceFile        string
	InCents           int64
	OutCents          int64
	BalanceCents      int64
	HasBalance        bool
	Status            string
	MatchedEntryID    int64
	MatchedJournalID  int64
	MatchedJournalRef string
	Candidates        []CandidateOption
}

// BankReconView adalah data halaman rekonsiliasi satu rekening bank.
type BankReconView struct {
	Bank           BankAccountRow
	Lines          []StatementRow
	BookCents      int64 // saldo buku akun COA sampai hari ini (debit-kredit)
	BankCents      int64 // jumlah mutasi tersimpan (masuk dikurangi keluar)
	UnmatchedTotal int64
	RoundingUnit   string
	RoundingLabel  string
	CanManage      bool
}

// Money menampilkan nilai sen memakai satuan pembulatan entitas.
func (v BankReconView) Money(cents int64) string {
	return FormatRounded(cents, v.RoundingUnit)
}

// BankImportPreviewView adalah data halaman pratinjau impor mutasi bank.
type BankImportPreviewView struct {
	BankID       int64
	BankLabel    string
	FileName     string
	TotalRows    int
	Valid        int
	RoundingUnit string
	Rows         []ImportPreviewRow
	Errors       []string
	Token        string
}

// ImportPreviewRow adalah satu baris pratinjau impor mutasi bank.
type ImportPreviewRow struct {
	DateLabel   string
	Description string
	Reference   string
	InCents     int64
	OutCents    int64
}

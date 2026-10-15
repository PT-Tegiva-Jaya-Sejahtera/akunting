package models

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Peran sistem pada akun (kolom accounts.system_role). Dipakai kode program
// untuk menemukan akun khusus tanpa menebak dari nama akun.
const (
	// SystemRoleRetainedEarnings adalah akun Saldo Laba, tujuan jurnal
	// penutup pada akhir periode (PSAK 201 par. 106-110).
	SystemRoleRetainedEarnings = "laba_ditahan"
)

// SystemRoleLabel menerjemahkan peran sistem ke label tampilan.
func SystemRoleLabel(role string) string {
	switch role {
	case SystemRoleRetainedEarnings:
		return "Saldo Laba (tujuan tutup buku)"
	default:
		return "-"
	}
}

// Satuan pembulatan penyajian laporan keuangan (PSAK 201 par. 53 huruf d).
const (
	RoundingFull     = "penuh"
	RoundingThousand = "ribuan"
	RoundingMillion  = "jutaan"
)

// RoundingLabel adalah keterangan satuan penyajian untuk kepala laporan.
func RoundingLabel(unit string) string {
	switch unit {
	case RoundingThousand:
		return "dalam ribuan Rupiah"
	case RoundingMillion:
		return "dalam jutaan Rupiah"
	default:
		return "dalam Rupiah"
	}
}

// RoundCents membulatkan nilai sen ke satuan penyajian.
func RoundCents(cents int64, unit string) int64 {
	switch unit {
	case RoundingThousand:
		return roundDiv(cents, 100_000) // Rp1.000 = 100.000 sen
	case RoundingMillion:
		return roundDiv(cents, 100_000_000)
	default:
		return roundDiv(cents, 100)
	}
}

func roundDiv(v, d int64) int64 {
	if d == 0 {
		return v
	}
	if v < 0 {
		return -((-v + d/2) / d)
	}
	return (v + d/2) / d
}

// FormatRounded menampilkan nilai sen memakai satuan penyajian, tanpa desimal,
// dengan pemisah ribuan gaya Indonesia: 1234567890 sen -> "12.345.679".
func FormatRounded(cents int64, unit string) string {
	v := RoundCents(cents, unit)
	sign := ""
	if v < 0 {
		sign = "-"
		v = -v
	}
	s := fmt.Sprintf("%d", v)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	return sign + b.String()
}

var monthNamesID = [...]string{
	"Januari", "Februari", "Maret", "April", "Mei", "Juni",
	"Juli", "Agustus", "September", "Oktober", "November", "Desember",
}

// ErrDateFormat dipakai ParseDateID untuk input yang tidak dikenali.
var ErrDateFormat = errors.New("format tanggal tidak valid")

// FormatDateShort menampilkan tanggal gaya Indonesia singkat: "31/12/2026".
func FormatDateShort(t time.Time) string {
	return fmt.Sprintf("%02d/%02d/%d", t.Day(), int(t.Month()), t.Year())
}

// FormatCompactRupiah menyingkat nilai sen menjadi label sumbu yang ringkas
// (mis. 1,5 M untuk 1,5 miliar, 500 jt untuk 500 juta). Dipakai label sumbu Y
// grafik supaya angka panjang tidak menutupi area gambar pada kartu sempit.
func FormatCompactRupiah(cents int64) string {
	rupiah := float64(cents) / 100
	sign := ""
	if rupiah < 0 {
		sign = "-"
		rupiah = -rupiah
	}
	angka := func(v float64) string {
		s := strconv.FormatFloat(v, 'f', 1, 64)
		s = strings.TrimSuffix(s, ".0")
		return strings.ReplaceAll(s, ".", ",")
	}
	switch {
	case rupiah >= 1_000_000_000_000:
		return sign + angka(rupiah/1_000_000_000_000) + " T"
	case rupiah >= 1_000_000_000:
		return sign + angka(rupiah/1_000_000_000) + " M"
	case rupiah >= 1_000_000:
		return sign + angka(rupiah/1_000_000) + " jt"
	case rupiah >= 1_000:
		return sign + angka(rupiah/1_000) + " rb"
	default:
		return sign + angka(rupiah)
	}
}

// DateFieldValue mengubah nilai ISO (2006-01-02) menjadi teks isian
// dd/mm/yyyy untuk kolom tanggal. Nilai yang tidak dikenali jadi kosong.
func DateFieldValue(iso string) string {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(iso))
	if err != nil {
		return ""
	}
	return FormatDateShort(t)
}

// ParseDateID membaca tanggal dari isian pengguna. Format yang diterima:
// hari/bulan/tahun (31/12/2026), hari-bulan-tahun, dan ISO tahun-bulan-hari
// (dipakai nilai bawaan formulir). Titik juga diterima sebagai pemisah.
func ParseDateID(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, ErrDateFormat
	}
	normal := strings.NewReplacer(".", "/", "-", "/").Replace(s)
	parts := strings.Split(normal, "/")
	if len(parts) != 3 {
		return time.Time{}, ErrDateFormat
	}
	a, errA := strconv.Atoi(parts[0])
	b, errB := strconv.Atoi(parts[1])
	cc, errC := strconv.Atoi(parts[2])
	if errA != nil || errB != nil || errC != nil {
		return time.Time{}, ErrDateFormat
	}
	// Tahun di depan berarti format ISO; selain itu hari di depan.
	hari, bulan, tahun := a, b, cc
	if len(parts[0]) == 4 {
		tahun, bulan, hari = a, b, cc
	}
	if tahun < 100 {
		tahun += 2000
	}
	if bulan < 1 || bulan > 12 || hari < 1 || hari > 31 {
		return time.Time{}, ErrDateFormat
	}
	t := time.Date(tahun, time.Month(bulan), hari, 0, 0, 0, 0, time.UTC)
	// Tolak tanggal yang bergeser (mis. 31/02 yang menjadi 3 Maret).
	if t.Day() != hari || int(t.Month()) != bulan {
		return time.Time{}, ErrDateFormat
	}
	return t, nil
}

// FormatDateID menampilkan tanggal gaya Indonesia: "31 Desember 2026".
func FormatDateID(t time.Time) string {
	return fmt.Sprintf("%d %s %d", t.Day(), monthNamesID[int(t.Month())-1], t.Year())
}

// PeriodView adalah satu periode akuntansi siap tampil.
type PeriodView struct {
	ID          int64
	Name        string
	StartISO    string
	EndISO      string
	StartLabel  string
	EndLabel    string
	Status      string // open | closed
	StatusLabel string
	IsFirst     bool
	ClosedNote  string
}

// OpeningLine adalah satu baris form/daftar saldo awal.
type OpeningLine struct {
	AccountID int64
	Code      string
	Name      string
	SubLabel  string
	Debit     int64 // sen
	Credit    int64 // sen
}

// OpeningView adalah data halaman saldo awal.
type OpeningView struct {
	Period      PeriodView
	HasPeriod   bool
	Lines       []OpeningLine
	TotalDebit  int64
	TotalCredit int64
	SavedAt     string // tanggal jurnal saldo awal bila sudah ada
	JournalID   int64
}

// Pemetaan pos minimum laporan (PSAK 201 par. 54 dan 82) yang dipakai saat
// akun baru dibuat, supaya dashboard bisa mengelompokkan kas, piutang, utang,
// beban keuangan, dan beban pajak tanpa menunggu pengaturan manual.
const (
	PsakKasSetaraKas     = "kas_setara_kas"
	PsakPiutang          = "piutang"
	PsakPersediaan       = "persediaan"
	PsakAsetTetap        = "aset_tetap"
	PsakAsetTetapAkum    = "aset_tetap_akumulasi"
	PsakAsetLainnya      = "aset_lainnya"
	PsakUtangUsaha       = "utang_usaha"
	PsakUtangBank        = "utang_bank"
	PsakPajak            = "pajak"
	PsakModal            = "modal"
	PsakSaldoLaba        = "saldo_laba"
	PsakPendapatan       = "pendapatan"
	PsakBebanKeuangan    = "beban_keuangan"
	PsakBebanPajak       = "beban_pajak"
	PsakBebanOperasional = "beban_operasional"
)

// PsakLineLabel menerjemahkan kode pemetaan pos ke label tampilan.
func PsakLineLabel(code string) string {
	switch code {
	case PsakKasSetaraKas:
		return "Kas dan setara kas"
	case PsakPiutang:
		return "Piutang"
	case PsakPersediaan:
		return "Persediaan"
	case PsakAsetTetap:
		return "Aset tetap"
	case PsakAsetTetapAkum:
		return "Akumulasi penyusutan"
	case PsakAsetLainnya:
		return "Aset lainnya"
	case PsakUtangUsaha:
		return "Utang usaha"
	case PsakUtangBank:
		return "Utang bank"
	case PsakPajak:
		return "Utang pajak"
	case PsakModal:
		return "Modal"
	case PsakSaldoLaba:
		return "Saldo laba"
	case PsakPendapatan:
		return "Pendapatan"
	case PsakBebanKeuangan:
		return "Beban keuangan"
	case PsakBebanPajak:
		return "Beban pajak"
	case PsakBebanOperasional:
		return "Beban operasional"
	default:
		return "Belum dipetakan"
	}
}

// SubledgerFlags menurunkan penanda subledger akun: is_receivable untuk akun
// piutang usaha dan is_payable untuk akun hutang usaha. Aturannya sama dengan
// backfill migrasi 0014 (is_receivable) dan 0015 (is_payable), supaya akun
// baru yang dibuat dari form atau seeder langsung bertanda tanpa menunggu
// migrasi ulang. Seperti GuessPsakLine, hasilnya diturunkan dari psak_line
// dan sub-type akun, tanpa hardcode id akun.
func SubledgerFlags(accountType string, subType string, psakLine string) (isReceivable bool, isPayable bool) {
	isReceivable = accountType == TypeAsset && psakLine == PsakPiutang
	isPayable = accountType == TypeLiability && subType == SubCurrentLiability && psakLine == PsakUtangUsaha
	return isReceivable, isPayable
}

// GuessPsakLine menebak pos minimum laporan dari nama, jenis, sub-jenis, dan
// peran sistem akun. Hasilnya boleh diperbaiki manual dari halaman Akun.
func GuessPsakLine(name string, accountType string, subType string, systemRole string) string {
	n := strings.ToLower(name)
	switch accountType {
	case "revenue":
		return PsakPendapatan
	case "expense":
		if strings.Contains(n, "bunga") {
			return PsakBebanKeuangan
		}
		if strings.Contains(n, "pajak") || strings.Contains(n, "pph") {
			return PsakBebanPajak
		}
		return PsakBebanOperasional
	case "equity":
		if systemRole == SystemRoleRetainedEarnings {
			return PsakSaldoLaba
		}
		return PsakModal
	case "asset":
		if strings.Contains(n, "kas") || strings.Contains(n, "bank") {
			return PsakKasSetaraKas
		}
		if strings.Contains(n, "piutang") {
			return PsakPiutang
		}
		if strings.Contains(n, "persediaan") {
			return PsakPersediaan
		}
		if subType == SubNonCurrentAsset && strings.Contains(n, "akumulasi") {
			return PsakAsetTetapAkum
		}
		if subType == SubNonCurrentAsset {
			return PsakAsetTetap
		}
		return PsakAsetLainnya
	case "liability":
		if strings.Contains(n, "bank") {
			return PsakUtangBank
		}
		if strings.Contains(n, "pajak") || strings.Contains(n, "ppn") || strings.Contains(n, "pph") {
			return PsakPajak
		}
		return PsakUtangUsaha
	}
	return ""
}

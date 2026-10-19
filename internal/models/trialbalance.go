package models

// Neraca Saldo (trial balance): saldo setiap akun pada sisi normalnya untuk
// satu periode akuntansi, dengan total debit dan kredit yang harus sama.
// Dipakai juga untuk neraca saldo setelah penutupan (IsPostClosing).

// TrialBalanceRow adalah satu baris neraca saldo. Akun bersaldo normal debit
// mengisi Debit, akun bersaldo normal kredit mengisi Credit; bila saldo
// akun berlawanan dengan sisi normalnya (rare), angka tampil di kolom
// sebelahnya sebagaimana adanya.
type TrialBalanceRow struct {
	Code   string
	Name   string
	Debit  int64
	Credit int64
}

// TrialBalanceView adalah data halaman neraca saldo.
type TrialBalanceView struct {
	Periods          []PeriodView
	SelectedPeriodID int64
	PeriodName       string
	PeriodStatus     string
	DateFromISO      string
	DateToISO        string
	Rows             []TrialBalanceRow
	TotalDebit       int64
	TotalCredit      int64
	RoundingUnit     string
	RoundingLabel    string
	// IsPostClosing menandai halaman "neraca saldo setelah penutupan";
	// Warning diisi bila periode yang dipilih belum ditutup sehingga angka
	// masih memuat laba (rugi) yang belum dialihkan ke Saldo Laba.
	IsPostClosing bool
	Warning       string
}

// Money menampilkan nilai sen memakai satuan pembulatan entitas.
func (v TrialBalanceView) Money(cents int64) string {
	return FormatRounded(cents, v.RoundingUnit)
}

package models

// Halaman Jurnal Penutup: daftar jurnal penutup (kind=closing) per periode
// akuntansi beserta baris-barisnya, dan pintasan tutup buku / buka lagi.

// ClosingLine adalah satu baris jurnal penutup.
type ClosingLine struct {
	AccountCode string
	AccountName string
	Description string
	Debit       int64
	Credit      int64
}

// ClosingJournal adalah satu jurnal penutup beserta baris-barisnya.
type ClosingJournal struct {
	PeriodName string
	DateLabel  string
	// DateShort adalah tanggal jurnal dd/mm/yyyy untuk kolom Tanggal.
	DateShort string
	Reference string
	Memo      string
	Status    string // status periode terkait: open/closed
	PeriodID  int64
	Lines     []ClosingLine
	Total     int64
}

// ClosingView adalah data halaman jurnal penutup. Periods dipakai untuk
// keterangan; ClosedCount menghitung periode yang sudah ditutup.
type ClosingView struct {
	Journals      []ClosingJournal
	OpenPeriods   []PeriodView // periode yang masih terbuka (bisa ditutup)
	ClosedCount   int
	RoundingUnit  string
	RoundingLabel string
}

// Money menampilkan nilai sen memakai satuan pembulatan entitas.
func (v ClosingView) Money(cents int64) string {
	return FormatRounded(cents, v.RoundingUnit)
}

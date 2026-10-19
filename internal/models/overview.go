package models

// Tampilan "posisi semua akun" di halaman Jurnal Umum: seluruh baris buku
// (semua jurnal) secara kronologis, satu baris per entri ledger, dengan
// total debit dan kredit di bagian bawah tabel.

// JournalLineAll adalah satu baris tampilan menyeluruh: tanggal jurnal,
// kode dan nama akun, keterangan, serta kolom debit/kredit.
type JournalLineAll struct {
	DateLabel   string
	Reference   string
	AccountCode string
	AccountName string
	Description string
	Debit       int64
	Credit      int64
	IsAdjusting bool
}

// JournalsOverviewView adalah data kartu "Posisi Semua Akun". SelectedYear 0
// berarti semua tahun. Truncated menandai baris yang dipotong limit query.
type JournalsOverviewView struct {
	Years        []int
	SelectedYear int // 0 = semua tahun
	Rows         []JournalLineAll
	TotalDebit   int64
	TotalCredit  int64
	Truncated    bool
}

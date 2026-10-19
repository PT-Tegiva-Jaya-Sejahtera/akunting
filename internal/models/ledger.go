package models

// LedgerRow adalah satu baris akun pada halaman Buku Besar.
type LedgerRow struct {
	AccountID int64
	Code      string
	Name      string
	SubLabel  string
	Opening   int64 // sen, saldo awal periode
	Debit     int64 // sen
	Credit    int64 // sen
	Balance   int64 // sen, saldo akhir periode (mengikuti saldo normal akun)
	IsActive  bool
}

// LedgerMovement adalah satu baris mutasi akun pada buku besar.
type LedgerMovement struct {
	DateLabel   string
	Reference   string
	Memo        string
	Description string
	Debit       int64
	Credit      int64
	Balance     int64 // saldo berjalan
}

// LedgerView adalah data halaman Buku Besar.
type LedgerView struct {
	PeriodName       string
	PeriodStatus     string
	DateFromISO      string
	DateToISO        string
	Periods          []PeriodView
	SelectedPeriodID int64
	// Pilihan rentang bulan bebas (lintas tahun): format "2006-01". Bila
	// keduanya terisi, rentang bulan menimpa periode akuntansi.
	MonthFrom     string
	MonthTo       string
	RangeLabel    string
	Rows          []LedgerRow
	TotalDebit    int64
	TotalCredit   int64
	Account       *LedgerRow
	Movements     []LedgerMovement
	RoundingUnit  string
	RoundingLabel string
}

// Money menampilkan nilai sen memakai satuan pembulatan entitas.
func (v LedgerView) Money(cents int64) string {
	return FormatRounded(cents, v.RoundingUnit)
}

// WorksheetRow adalah satu baris Kertas Kerja (neraca lajur) dengan lima
// kelompok kolom: neraca saldo, penyesuaian, setelah penyesuaian, laba rugi,
// dan neraca.
type WorksheetRow struct {
	Code string
	Name string

	SaldoDebit    int64
	SaldoCredit   int64
	AdjDebit      int64
	AdjCredit     int64
	FinalDebit    int64
	FinalCredit   int64
	PLDebit       int64
	PLCredit      int64
	BalanceDebit  int64
	BalanceCredit int64
}

// WorksheetTotals adalah jumlah seluruh kolom kertas kerja.
type WorksheetTotals struct {
	SaldoDebit    int64
	SaldoCredit   int64
	AdjDebit      int64
	AdjCredit     int64
	FinalDebit    int64
	FinalCredit   int64
	PLDebit       int64
	PLCredit      int64
	BalanceDebit  int64
	BalanceCredit int64
}

// WorksheetView adalah data halaman Kertas Kerja.
type WorksheetView struct {
	PeriodName       string
	PeriodStatus     string
	DateFromISO      string
	DateToISO        string
	DateToLabel      string
	Periods          []PeriodView
	SelectedPeriodID int64
	Rows             []WorksheetRow
	Totals           WorksheetTotals
	NetIncome        int64
	RoundingUnit     string
	RoundingLabel    string
	HasData          bool
}

// Money menampilkan nilai sen memakai satuan pembulatan entitas.
func (v WorksheetView) Money(cents int64) string {
	return FormatRounded(cents, v.RoundingUnit)
}

// AdjustmentRow adalah satu jurnal penyesuaian pada daftar.
type AdjustmentRow struct {
	ID        int64
	DateLabel string
	Reference string
	Memo      string
	Amount    int64
}

// AdjustmentView adalah data halaman Jurnal Penyesuaian.
type AdjustmentView struct {
	Rows          []AdjustmentRow
	RoundingUnit  string
	RoundingLabel string
}

// Money menampilkan nilai sen memakai satuan pembulatan entitas.
func (v AdjustmentView) Money(cents int64) string {
	return FormatRounded(cents, v.RoundingUnit)
}

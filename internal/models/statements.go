// Perakit tiga laporan baru PSAK: Laporan Perubahan Ekuitas (PSAK 201 par.
// 106-110), Laporan Arus Kas metode tidak langsung (PSAK 207), dan Catatan
// atas Laporan Keuangan (PSAK 201 par. 112-138). Semua fungsi di file ini
// murni (menerima saldo/mutasi akun dalam satuan sen) supaya bisa diuji tanpa
// database.
package models

import (
	"sort"
	"strings"
)

// Jenis akun (accounts.account_type) sebagai teks, supaya paket ini tidak
// bergantung pada paket database.
const (
	TypeAsset     = "asset"
	TypeLiability = "liability"
	TypeEquity    = "equity"
	TypeRevenue   = "revenue"
	TypeExpense   = "expense"
)

// AccountTotal adalah ringkasan saldo atau mutasi satu akun dalam satuan sen.
// DebitCents/CreditCents adalah jumlah debit/kredit pada rentang yang dipakai.
type AccountTotal struct {
	ID          int64
	Code        string
	Name        string
	AccountType string
	SubType     string
	PsakLine    string
	DebitCents  int64
	CreditCents int64
}

// profitSigned mengembalikan kontribusi akun terhadap laba periode: kredit
// dikurangi debit. Pendapatan bertambah (kredit) menambah laba; beban bertambah
// (debit) mengurangi laba. Sama dengan rumus "laba belum ditutup" pada Neraca
// (assembleBS), supaya angka kedua laporan selalu konsisten.
func (a AccountTotal) profitSigned() int64 {
	switch a.AccountType {
	case TypeRevenue, TypeExpense:
		return a.CreditCents - a.DebitCents
	}
	return 0
}

// balanceSigned mengembalikan saldo akun menurut sisi normalnya: kredit-ditambah
// untuk liabilitas/ekuitas/pendapatan, debit-ditambah untuk aset/beban.
func (a AccountTotal) balanceSigned() int64 {
	switch a.AccountType {
	case TypeLiability, TypeEquity, TypeRevenue:
		return a.CreditCents - a.DebitCents
	default:
		return a.DebitCents - a.CreditCents
	}
}

// ---------------------------------------------------------------------------
// Laporan Perubahan Ekuitas (PSAK 201 par. 106-110)
// ---------------------------------------------------------------------------

// EquityColumn adalah satu komponen ekuitas (satu akun ekuitas) beserta
// rekonsiliasi saldo awal ke saldo akhirnya (par. 106 huruf d).
type EquityColumn struct {
	Code           string
	Name           string
	PsakLine       string // pemetaan pos: modal, saldo_laba, atau komponen lain
	Opening        int64  // saldo awal periode
	Contribution   int64  // setoran modal dan transaksi dengan pemilik yang menambah
	Distribution   int64  // prive dan dividen (distribusi kepada pemilik)
	ProfitTransfer int64  // laba periode yang dipindahkan lewat jurnal penutup
	Closing        int64  // saldo akhir periode
}

// EquityStatement adalah isi Laporan Perubahan Ekuitas. Kolom "Laba (Rugi)
// Belum Ditutup" mewakili laba periode yang belum dipindahkan ke saldo laba,
// sejajar dengan baris yang sama di Neraca, supaya jumlah ekuitas laporan ini
// selalu menutup ke jumlah ekuitas Neraca.
type EquityStatement struct {
	Columns []EquityColumn

	UnclosedOpening  int64 // laba periode sebelumnya yang belum ditutup
	UnclosedProfit   int64 // laba (rugi) periode berjalan (par. 106(d)(i))
	UnclosedTransfer int64 // laba yang dipindahkan ke saldo laba oleh jurnal penutup
	UnclosedClosing  int64

	TotalOpening      int64
	TotalContribution int64
	TotalDistribution int64
	TotalClosing      int64
}

// ProfitLoss adalah laba (rugi) periode berjalan, angka yang sama dengan laba
// pada Laporan Laba Rugi.
func (s EquityStatement) ProfitLoss() int64 { return s.UnclosedProfit }

// TotalProfit adalah baris "Laba (rugi) periode berjalan" pada kolom total.
func (s EquityStatement) TotalProfit() int64 { return s.UnclosedProfit }

// Distributions adalah jumlah distribusi kepada pemilik selama periode
// (PSAK 201 par. 107).
func (s EquityStatement) Distributions() int64 { return s.TotalDistribution }

// BuildEquity menyusun Laporan Perubahan Ekuitas dari saldo awal, mutasi
// periode berjalan (tanpa jurnal saldo awal/penutup), dan mutasi jurnal penutup.
//
// Sumber setiap baris:
//  1. Saldo awal: saldo akun ekuitas sebelum periode, termasuk jurnal saldo awal.
//  2. Setoran modal: kredit ke akun ekuitas dari jurnal biasa/penyesuaian.
//  3. Prive dan dividen: debit ke akun ekuitas dari jurnal biasa/penyesuaian.
//  4. Laba (rugi) periode: laba periode menurut akun pendapatan/beban.
//  5. Pemindahan laba: mutasi akun ekuitas dari jurnal penutup.
func BuildEquity(opening, movements, closing []AccountTotal) EquityStatement {
	index := map[string]int{}
	var s EquityStatement

	for _, a := range opening {
		if a.AccountType == TypeEquity {
			s.Columns[equityColumn(index, &s, a)].Opening += a.CreditCents - a.DebitCents
		}
		s.UnclosedOpening += a.profitSigned()
	}
	for _, a := range movements {
		switch a.AccountType {
		case TypeEquity:
			col := &s.Columns[equityColumn(index, &s, a)]
			col.Contribution += a.CreditCents
			col.Distribution += a.DebitCents
		default:
			s.UnclosedProfit += a.profitSigned()
		}
	}
	for _, a := range closing {
		if a.AccountType == TypeEquity {
			s.Columns[equityColumn(index, &s, a)].ProfitTransfer += a.CreditCents - a.DebitCents
		}
	}

	for i := range s.Columns {
		c := &s.Columns[i]
		c.Closing = c.Opening + c.Contribution - c.Distribution + c.ProfitTransfer
		s.UnclosedTransfer += c.ProfitTransfer
		s.TotalOpening += c.Opening
		s.TotalContribution += c.Contribution
		s.TotalDistribution += c.Distribution
		s.TotalClosing += c.Closing
	}
	s.UnclosedClosing = s.UnclosedOpening + s.UnclosedProfit - s.UnclosedTransfer
	s.TotalOpening += s.UnclosedOpening
	s.TotalClosing += s.UnclosedClosing

	// Urutan kolom: modal disetor, saldo laba, komponen lain menurut kode akun
	// (PSAK 201 par. 108: komponen ekuitas = kelas modal disetor, saldo laba).
	sort.SliceStable(s.Columns, func(i, j int) bool {
		return equityRank(s.Columns[i]) < equityRank(s.Columns[j])
	})
	return s
}

// equityColumn mengambil atau membuat indeks kolom komponen untuk satu akun
// ekuitas.
func equityColumn(index map[string]int, s *EquityStatement, a AccountTotal) int {
	if i, ok := index[a.Code]; ok {
		return i
	}
	s.Columns = append(s.Columns, EquityColumn{Code: a.Code, Name: a.Name, PsakLine: a.PsakLine})
	index[a.Code] = len(s.Columns) - 1
	return len(s.Columns) - 1
}

// equityRank mengurutkan kolom: modal, saldo laba, lalu komponen lain per kode
// (PSAK 201 par. 108: komponen ekuitas = kelas modal disetor, saldo laba).
func equityRank(c EquityColumn) int {
	switch c.PsakLine {
	case PsakModal:
		return 0
	case PsakSaldoLaba:
		return 1
	}
	return 2
}

// ---------------------------------------------------------------------------
// Laporan Arus Kas (PSAK 207), metode tidak langsung (par. 18 huruf b)
// ---------------------------------------------------------------------------

// CashFlow adalah Laporan Arus Kas satu periode. Setiap baris memakai
// ReportLine; AmountCents adalah arus kas periode berjalan.
type CashFlow struct {
	NetIncome      int64
	NonCash        []ReportLine // penyesuaian non-kas (penyusutan, amortisasi)
	WorkingCapital []ReportLine // perubahan modal kerja
	Investing      []ReportLine // aktivitas investasi
	Financing      []ReportLine // aktivitas pendanaan
	CashBeginning  int64
	CashEnd        int64
}

// TotalNonCash menjumlahkan penyesuaian non-kas.
func (c CashFlow) TotalNonCash() int64 { return sumAmounts(c.NonCash) }

// TotalWorkingCapital menjumlahkan perubahan modal kerja.
func (c CashFlow) TotalWorkingCapital() int64 { return sumAmounts(c.WorkingCapital) }

// OperatingNet adalah arus kas neto dari aktivitas operasi (par. 18(b)).
func (c CashFlow) OperatingNet() int64 {
	return c.NetIncome + c.TotalNonCash() + c.TotalWorkingCapital()
}

// TotalInvesting adalah arus kas neto dari aktivitas investasi (par. 21).
func (c CashFlow) TotalInvesting() int64 { return sumAmounts(c.Investing) }

// TotalFinancing adalah arus kas neto dari aktivitas pendanaan (par. 21).
func (c CashFlow) TotalFinancing() int64 { return sumAmounts(c.Financing) }

// NetChange adalah kenaikan (penurunan) neto kas dan setara kas.
func (c CashFlow) NetChange() int64 {
	return c.OperatingNet() + c.TotalInvesting() + c.TotalFinancing()
}

func sumAmounts(lines []ReportLine) int64 {
	var n int64
	for _, l := range lines {
		n += l.AmountCents
	}
	return n
}

// IsCashAccount menentukan apakah akun termasuk kas dan setara kas (PSAK 207
// par. 06-09): akun aset yang dipetakan ke pos "kas dan setara kas", atau akun
// aset lancar yang namanya berisi "kas" atau "bank".
func IsCashAccount(a AccountTotal) bool {
	if a.AccountType != TypeAsset {
		return false
	}
	if a.PsakLine == PsakKasSetaraKas {
		return true
	}
	if a.SubType == SubCurrentAsset {
		n := strings.ToLower(a.Name)
		return strings.Contains(n, "kas") || strings.Contains(n, "bank")
	}
	return false
}

// Golongan arus kas untuk klasifikasi aktivitas (PSAK 207 par. 10).
const (
	CategoryOperating = "operasi"
	CategoryInvesting = "investasi"
	CategoryFinancing = "pendanaan"
)

// cashCategory mengklasifikasikan satu akun non-kas ke golongan arus kasnya.
// Aset tidak lancar (termasuk akumulasi penyusutan) = investasi; ekuitas dan
// utang bank = pendanaan; seluruh sisanya (termasuk beban keuangan dan beban
// pajak, sesuai kebijakan konsisten par. 29-34) = operasi.
func cashCategory(a AccountTotal) string {
	switch {
	case a.SubType == SubNonCurrentAsset,
		a.PsakLine == PsakAsetTetap,
		a.PsakLine == PsakAsetTetapAkum:
		return CategoryInvesting
	case a.AccountType == TypeEquity,
		a.PsakLine == PsakUtangBank:
		return CategoryFinancing
	default:
		return CategoryOperating
	}
}

// BuildCashFlow menyusun Laporan Arus Kas metode tidak langsung dari saldo awal
// dan mutasi akun periode berjalan (tanpa jurnal saldo awal/penutup, yang tidak
// memengaruhi kas).
//
// Struktur (PSAK 207 par. 18(b), 20):
//   - aktivitas operasi: laba periode, disesuaikan pos non-kas (penyusutan)
//     dan perubahan modal kerja;
//   - aktivitas investasi: perubahan nilai buku aset tidak lancar;
//   - aktivitas pendanaan: transaksi dengan pemilik dan perubahan utang bank.
//
// Kepatuhan penyajian: seluruh akun non-kas terhitung tepat satu kali, sehingga
// kenaikan kas pada laporan ini selalu menutup ke perubahan kas di Neraca.
func BuildCashFlow(opening, movements []AccountTotal) CashFlow {
	var c CashFlow
	var cashMove int64
	for _, a := range opening {
		if IsCashAccount(a) {
			c.CashBeginning += a.DebitCents - a.CreditCents
		}
	}

	// Akun akumulasi penyusutan: kenaikannya adalah beban non-kas yang harus
	// ditambahkan kembali ke laba (par. 20 huruf b).
	var depreciation int64

	wc := map[string]*ReportLine{}
	inv := map[string]*ReportLine{}
	fin := map[string]*ReportLine{}

	for _, a := range movements {
		switch {
		case IsCashAccount(a):
			cashMove += a.DebitCents - a.CreditCents
		case a.AccountType == TypeRevenue || a.AccountType == TypeExpense:
			c.NetIncome += a.profitSigned()
		case a.PsakLine == PsakAsetTetapAkum:
			depreciation += a.CreditCents - a.DebitCents
		default:
			// Kontribusi akun terhadap arus kas = kredit dikurangi debit:
			// aset bertambah = arus keluar, liabilitas bertambah = arus
			// masuk. Karena setiap jurnal seimbang, seluruh akun non-kas
			// dengan aturan ini menutup persis ke perubahan kas.
			amount := a.CreditCents - a.DebitCents
			switch cashCategory(a) {
			case CategoryInvesting:
				addLine(inv, investingLabel(a), amount)
			case CategoryFinancing:
				switch a.AccountType {
				case TypeEquity:
					// Kontribusi pemilik (kredit) dan distribusi (debit)
					// dipisah mengikuti par. 17 huruf a dan b.
					addLine(fin, "Setoran modal pemilik", a.CreditCents)
					addLine(fin, "Prive dan dividen dibayarkan", -a.DebitCents)
				default:
					addLine(fin, "Perubahan utang bank (neto)", amount)
				}
			default:
				addLine(wc, workingCapitalLabel(a), amount)
			}
		}
	}

	if depreciation != 0 {
		c.NonCash = append(c.NonCash, ReportLine{Name: "Penyusutan dan amortisasi", AmountCents: depreciation})
	}
	c.WorkingCapital = mapToLines(wc, false)
	c.Investing = mapToLines(inv, false)
	c.Financing = mapToLines(fin, true)

	// Kas akhir = kas awal + mutasi kas periode berjalan.
	c.CashEnd = c.CashBeginning + cashMove
	return c
}

// investingLabel memberi label baris investasi per kelompok akun.
func investingLabel(a AccountTotal) string {
	switch a.PsakLine {
	case PsakAsetTetap:
		return "Perolehan dan pelepasan aset tetap (nilai buku, neto)"
	case PsakAsetTetapAkum:
		return "Perubahan aset tetap lainnya (neto)"
	default:
		return "Perolehan dan pelepasan aset tidak lancar lainnya (neto)"
	}
}

// workingCapitalLabel memberi label baris perubahan modal kerja per kelompok
// akun; akun tanpa pemetaan tampil per nama akun.
func workingCapitalLabel(a AccountTotal) string {
	switch a.PsakLine {
	case PsakPiutang:
		return "Perubahan piutang usaha dan piutang lainnya"
	case PsakPersediaan:
		return "Perubahan persediaan"
	case PsakUtangUsaha:
		return "Perubahan utang usaha"
	case PsakPajak:
		return "Perubahan utang pajak"
	default:
		return "Perubahan " + a.Name
	}
}

// addLine menambahkan jumlah ke baris bernama pada peta (akumulasi per nama).
func addLine(m map[string]*ReportLine, name string, amount int64) {
	if amount == 0 {
		return
	}
	if line, ok := m[name]; ok {
		line.AmountCents += amount
		return
	}
	m[name] = &ReportLine{Name: name, AmountCents: amount}
}

// mapToLines mengubah peta baris menjadi slice terurut nama. Baris "Setoran
// modal pemilik" dan "Prive dan dividen dibayarkan" selalu tampil lebih dulu
// pada kelompok pendanaan.
func mapToLines(m map[string]*ReportLine, financingOrder bool) []ReportLine {
	out := make([]ReportLine, 0, len(m))
	if financingOrder {
		for _, name := range []string{"Setoran modal pemilik", "Prive dan dividen dibayarkan"} {
			if line, ok := m[name]; ok {
				out = append(out, *line)
				delete(m, name)
			}
		}
	}
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		out = append(out, *m[name])
	}
	return out
}

// ---------------------------------------------------------------------------
// Wadah tampilan halaman laporan
// ---------------------------------------------------------------------------

// EquityView adalah halaman Laporan Perubahan Ekuitas. Periode berjalan dan
// periode pembanding disajikan sebagai dua matriks bertumpuk (par. 38).
type EquityView struct {
	ReportView
	Cur EquityStatement
	Pri *EquityStatement
}

// CashFlowView adalah halaman Laporan Arus Kas dengan kolom pembanding.
// Slices NonCash/WorkingCapital/Investing/Financing sudah memuat kolom
// periode berjalan (AmountCents) dan periode pembanding (PriorAmountCents).
type CashFlowView struct {
	ReportView
	Cur CashFlow
	Pri *CashFlow

	NetIncome          int64
	PriorNetIncome     int64
	NonCash            []ReportLine
	WorkingCapital     []ReportLine
	Investing          []ReportLine
	Financing          []ReportLine
	CashBeginning      int64
	CashEnd            int64
	PriorCashBeginning int64
	PriorCashEnd       int64
}

// MergeLines menggabungkan baris periode berjalan dan periode pembanding
// berdasarkan nama pos supaya kedua kolom sejajar.
func (v *CashFlowView) MergeLines() {
	v.NetIncome = v.Cur.NetIncome
	v.CashBeginning = v.Cur.CashBeginning
	v.CashEnd = v.Cur.CashEnd
	var pri CashFlow
	if v.Pri != nil {
		pri = *v.Pri
		v.PriorNetIncome = pri.NetIncome
		v.PriorCashBeginning = pri.CashBeginning
		v.PriorCashEnd = pri.CashEnd
	}
	v.NonCash = mergeLinesByName(v.Cur.NonCash, pri.NonCash)
	v.WorkingCapital = mergeLinesByName(v.Cur.WorkingCapital, pri.WorkingCapital)
	v.Investing = mergeLinesByName(v.Cur.Investing, pri.Investing)
	v.Financing = mergeLinesByName(v.Cur.Financing, pri.Financing)
}

// mergeLinesByName menggabungkan dua daftar baris berdasarkan nama pos.
func mergeLinesByName(cur, pri []ReportLine) []ReportLine {
	out := make([]ReportLine, 0, len(cur)+len(pri))
	index := map[string]int{}
	for _, l := range cur {
		l.PriorAmountCents = 0
		index[l.Name] = len(out)
		out = append(out, l)
	}
	for _, l := range pri {
		if i, ok := index[l.Name]; ok {
			out[i].PriorAmountCents = l.AmountCents
			continue
		}
		out = append(out, ReportLine{Name: l.Name, AmountCents: 0, PriorAmountCents: l.AmountCents})
	}
	return out
}

func sumMergedLines(lines []ReportLine) (cur int64, pri int64) {
	for _, l := range lines {
		cur += l.AmountCents
		pri += l.PriorAmountCents
	}
	return cur, pri
}

// TotalNonCash adalah jumlah penyesuaian non-kas periode berjalan.
func (v CashFlowView) TotalNonCash() int64 { c, _ := sumMergedLines(v.NonCash); return c }

// PriorTotalNonCash adalah jumlah penyesuaian non-kas periode pembanding.
func (v CashFlowView) PriorTotalNonCash() int64 { _, p := sumMergedLines(v.NonCash); return p }

// TotalWorkingCapital adalah jumlah perubahan modal kerja periode berjalan.
func (v CashFlowView) TotalWorkingCapital() int64 {
	c, _ := sumMergedLines(v.WorkingCapital)
	return c
}

// PriorTotalWorkingCapital adalah jumlah perubahan modal kerja periode pembanding.
func (v CashFlowView) PriorTotalWorkingCapital() int64 {
	_, p := sumMergedLines(v.WorkingCapital)
	return p
}

// TotalInvesting adalah arus kas neto aktivitas investasi periode berjalan.
func (v CashFlowView) TotalInvesting() int64 { c, _ := sumMergedLines(v.Investing); return c }

// PriorTotalInvesting adalah arus kas neto aktivitas investasi periode pembanding.
func (v CashFlowView) PriorTotalInvesting() int64 { _, p := sumMergedLines(v.Investing); return p }

// TotalFinancing adalah arus kas neto aktivitas pendanaan periode berjalan.
func (v CashFlowView) TotalFinancing() int64 { c, _ := sumMergedLines(v.Financing); return c }

// PriorTotalFinancing adalah arus kas neto aktivitas pendanaan periode pembanding.
func (v CashFlowView) PriorTotalFinancing() int64 { _, p := sumMergedLines(v.Financing); return p }

// OperatingNet adalah arus kas neto aktivitas operasi periode berjalan.
func (v CashFlowView) OperatingNet() int64 {
	return v.NetIncome + v.TotalNonCash() + v.TotalWorkingCapital()
}

// PriorOperatingNet adalah arus kas neto aktivitas operasi periode pembanding.
func (v CashFlowView) PriorOperatingNet() int64 {
	return v.PriorNetIncome + v.PriorTotalNonCash() + v.PriorTotalWorkingCapital()
}

// NetChange adalah kenaikan (penurunan) neto kas dan setara kas periode berjalan.
func (v CashFlowView) NetChange() int64 {
	return v.OperatingNet() + v.TotalInvesting() + v.TotalFinancing()
}

// PriorNetChange adalah kenaikan (penurunan) neto kas periode pembanding.
func (v CashFlowView) PriorNetChange() int64 {
	return v.PriorOperatingNet() + v.PriorTotalInvesting() + v.PriorTotalFinancing()
}

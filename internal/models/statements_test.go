// Uji perakit Laporan Perubahan Ekuitas dan Laporan Arus Kas, termasuk
// identitas penutup: perubahan ekuitas menutup ke ekuitas Neraca dan arus kas
// menutup ke perubahan kas di Neraca. Skenario uji adalah kumpulan jurnal
// seimbang yang diagregasi per bucket (saldo awal, mutasi, jurnal penutup)
// persis seperti hasil query database.
package models

import "testing"

// rp mengubah Rupiah menjadi satuan sen yang dipakai seluruh perakit laporan.
func rp(juta int64) int64 { return juta * 100 }

type testLine struct {
	code   string
	debit  int64 // Rupiah
	credit int64 // Rupiah
}

// testJournal adalah satu jurnal utuh; bucket menentukan kelompoknya menurut
// query database: "opening" (saldo awal), "movements" (mutasi periode), dan
// "closing" (jurnal penutup).
type testJournal struct {
	bucket string
	lines  []testLine
}

var testChart = []AccountTotal{
	{Code: "1-1000", Name: "Kas", AccountType: TypeAsset, SubType: SubCurrentAsset, PsakLine: PsakKasSetaraKas},
	{Code: "1-1100", Name: "Bank", AccountType: TypeAsset, SubType: SubCurrentAsset, PsakLine: PsakKasSetaraKas},
	{Code: "1-2000", Name: "Piutang Usaha", AccountType: TypeAsset, SubType: SubCurrentAsset, PsakLine: PsakPiutang},
	{Code: "1-3000", Name: "Persediaan", AccountType: TypeAsset, SubType: SubCurrentAsset, PsakLine: PsakPersediaan},
	{Code: "1-4000", Name: "Aset Tetap", AccountType: TypeAsset, SubType: SubNonCurrentAsset, PsakLine: PsakAsetTetap},
	{Code: "1-4900", Name: "Akumulasi Penyusutan", AccountType: TypeAsset, SubType: SubNonCurrentAsset, PsakLine: PsakAsetTetapAkum},
	{Code: "2-1000", Name: "Utang Usaha", AccountType: TypeLiability, SubType: SubCurrentLiability, PsakLine: PsakUtangUsaha},
	{Code: "2-2000", Name: "Utang Bank", AccountType: TypeLiability, SubType: SubCurrentLiability, PsakLine: PsakUtangBank},
	{Code: "3-1000", Name: "Modal Disetor", AccountType: TypeEquity, SubType: SubEquity, PsakLine: PsakModal},
	{Code: "3-2000", Name: "Saldo Laba", AccountType: TypeEquity, SubType: SubEquity, PsakLine: PsakSaldoLaba},
	{Code: "3-3000", Name: "Prive", AccountType: TypeEquity, SubType: SubEquity},
	{Code: "4-1000", Name: "Pendapatan Sewa", AccountType: TypeRevenue, SubType: SubRevenue, PsakLine: PsakPendapatan},
	{Code: "5-1000", Name: "Beban Gaji", AccountType: TypeExpense, SubType: SubExpense, PsakLine: PsakBebanOperasional},
	{Code: "5-2000", Name: "Beban Listrik", AccountType: TypeExpense, SubType: SubExpense, PsakLine: PsakBebanOperasional},
	{Code: "5-3000", Name: "Beban Penyusutan", AccountType: TypeExpense, SubType: SubExpense, PsakLine: PsakBebanOperasional},
	{Code: "5-4000", Name: "Beban Bunga", AccountType: TypeExpense, SubType: SubExpense, PsakLine: PsakBebanKeuangan},
}

// fullScenario adalah dua tahun buku. Tahun pertama (bucket opening, sudah
// ditutup bukunya) memberi saldo awal nyata; tahun kedua (bucket movements dan
// closing) adalah periode berjalan yang diuji. Seluruh jurnal seimbang.
func fullScenario() []testJournal {
	return []testJournal{
		// Saldo awal perusahaan (jurnal saldo awal, 2025-01-01).
		{bucket: "opening", lines: []testLine{
			{"1-1000", 20_000_000, 0}, {"1-1100", 30_000_000, 0},
			{"1-2000", 5_000_000, 0}, {"1-3000", 8_000_000, 0},
			{"1-4000", 60_000_000, 0},
			{"1-4900", 0, 10_000_000}, {"2-1000", 0, 6_000_000},
			{"2-2000", 0, 25_000_000}, {"3-1000", 0, 50_000_000},
			{"3-2000", 0, 32_000_000},
		}},
		// Transaksi 2025 (masuk bucket saldo awal untuk periode 2026).
		{bucket: "opening", lines: []testLine{
			{"1-1000", 40_000_000, 0}, {"4-1000", 0, 40_000_000},
		}},
		{bucket: "opening", lines: []testLine{
			{"5-1000", 25_000_000, 0}, {"1-1000", 0, 25_000_000},
		}},
		// Jurnal penutup 2025-12-31 (sudah dihitung di saldo awal 2026).
		{bucket: "opening", lines: []testLine{
			{"4-1000", 40_000_000, 0}, {"5-1000", 0, 25_000_000},
			{"3-2000", 0, 15_000_000},
		}},

		// ===== Periode berjalan 2026 (bucket movements) =====
		{bucket: "movements", lines: []testLine{ // setoran modal
			{"1-1000", 10_000_000, 0}, {"3-1000", 0, 10_000_000},
		}},
		{bucket: "movements", lines: []testLine{ // beli aset tetap tunai
			{"1-4000", 20_000_000, 0}, {"1-1000", 0, 20_000_000},
		}},
		{bucket: "movements", lines: []testLine{ // penarikan utang bank
			{"1-1100", 15_000_000, 0}, {"2-2000", 0, 15_000_000},
		}},
		{bucket: "movements", lines: []testLine{ // prive
			{"3-3000", 5_000_000, 0}, {"1-1000", 0, 5_000_000},
		}},
		{bucket: "movements", lines: []testLine{ // pendapatan tunai
			{"1-1000", 25_000_000, 0}, {"4-1000", 0, 25_000_000},
		}},
		{bucket: "movements", lines: []testLine{ // pendapatan kredit
			{"1-2000", 5_000_000, 0}, {"4-1000", 0, 5_000_000},
		}},
		{bucket: "movements", lines: []testLine{ // beli persediaan kredit
			{"1-3000", 7_000_000, 0}, {"2-1000", 0, 7_000_000},
		}},
		{bucket: "movements", lines: []testLine{ // bayar utang usaha
			{"2-1000", 3_000_000, 0}, {"1-1000", 0, 3_000_000},
		}},
		{bucket: "movements", lines: []testLine{ // beban gaji
			{"5-1000", 12_000_000, 0}, {"1-1000", 0, 12_000_000},
		}},
		{bucket: "movements", lines: []testLine{ // beban listrik kredit
			{"5-2000", 3_000_000, 0}, {"2-1000", 0, 3_000_000},
		}},
		{bucket: "movements", lines: []testLine{ // penyesuaian penyusutan
			{"5-3000", 6_000_000, 0}, {"1-4900", 0, 6_000_000},
		}},
		{bucket: "movements", lines: []testLine{ // beban bunga tunai
			{"5-4000", 2_500_000, 0}, {"1-1000", 0, 2_500_000},
		}},

		// Jurnal penutup 2026-09-30: laba 6.500.000 ke saldo laba.
		{bucket: "closing", lines: []testLine{
			{"4-1000", 30_000_000, 0}, {"5-1000", 0, 12_000_000},
			{"5-2000", 0, 3_000_000}, {"5-3000", 0, 6_000_000},
			{"5-4000", 0, 2_500_000}, {"3-2000", 0, 6_500_000},
		}},
	}
}

// aggregate menjumlahkan debit/kredit per akun untuk bucket tertentu, sama
// dengan hasil query ListAccountTotalsOpening/Movements/Closing.
func aggregate(journals []testJournal, buckets ...string) []AccountTotal {
	in := map[string]bool{}
	for _, b := range buckets {
		in[b] = true
	}
	out := make([]AccountTotal, 0, len(testChart))
	for _, a := range testChart {
		tot := a
		for _, j := range journals {
			if !in[j.bucket] {
				continue
			}
			for _, l := range j.lines {
				if l.code != a.Code {
					continue
				}
				tot.DebitCents += rp(l.debit)
				tot.CreditCents += rp(l.credit)
			}
		}
		out = append(out, tot)
	}
	return out
}

// cashDelta menghitung perubahan kas langsung dari jurnal, tanpa lewat
// perakit laporan, sebagai pembanding independen untuk uji penutup.
func cashDelta(journals []testJournal, buckets ...string) int64 {
	var n int64
	for _, a := range aggregate(journals, buckets...) {
		if IsCashAccount(a) {
			n += a.DebitCents - a.CreditCents
		}
	}
	return n
}

// neracaEquity menghitung jumlah ekuitas menurut aturan Neraca (assembleBS):
// saldo akun ekuitas ditambah laba (rugi) yang belum ditutup.
func neracaEquity(journals []testJournal) (equity, unclosed int64) {
	for _, a := range aggregate(journals, "opening", "movements", "closing") {
		switch a.AccountType {
		case TypeEquity:
			equity += a.CreditCents - a.DebitCents
		case TypeRevenue, TypeExpense:
			unclosed += a.CreditCents - a.DebitCents
		}
	}
	return equity, unclosed
}

func lineByName(t *testing.T, lines []ReportLine, name string) ReportLine {
	t.Helper()
	for _, l := range lines {
		if l.Name == name {
			return l
		}
	}
	t.Fatalf("baris %q tidak ditemukan di %+v", name, lines)
	return ReportLine{}
}

func columnByCode(t *testing.T, s EquityStatement, code string) EquityColumn {
	t.Helper()
	for _, c := range s.Columns {
		if c.Code == code {
			return c
		}
	}
	t.Fatalf("kolom ekuitas %q tidak ditemukan", code)
	return EquityColumn{}
}

func TestBuildEquityMenutupKeNeraca(t *testing.T) {
	scenario := fullScenario()
	s := BuildEquity(
		aggregate(scenario, "opening"),
		aggregate(scenario, "movements"),
		aggregate(scenario, "closing"),
	)

	if s.UnclosedOpening != 0 {
		t.Fatalf("saldo awal laba belum ditutup = %d, mau 0 (tahun sebelumnya sudah ditutup)", s.UnclosedOpening)
	}
	if s.ProfitLoss() != rp(6_500_000) {
		t.Fatalf("laba periode = %d, mau %d", s.ProfitLoss(), rp(6_500_000))
	}

	modal := columnByCode(t, s, "3-1000")
	if modal.Opening != rp(50_000_000) || modal.Contribution != rp(10_000_000) || modal.Closing != rp(60_000_000) {
		t.Fatalf("kolom modal salah: %+v", modal)
	}
	laba := columnByCode(t, s, "3-2000")
	if laba.Opening != rp(47_000_000) {
		t.Fatalf("saldo awal saldo laba = %d, mau %d (32 juta + laba 2025)", laba.Opening, rp(47_000_000))
	}
	if laba.ProfitTransfer != rp(6_500_000) || laba.Closing != rp(53_500_000) {
		t.Fatalf("kolom saldo laba salah: %+v", laba)
	}
	prive := columnByCode(t, s, "3-3000")
	if prive.Distribution != rp(5_000_000) || prive.Closing != rp(-5_000_000) {
		t.Fatalf("kolom prive salah: %+v", prive)
	}

	// Rekonsiliasi setiap kolom: awal + setoran - distribusi + pemindahan.
	for _, c := range s.Columns {
		want := c.Opening + c.Contribution - c.Distribution + c.ProfitTransfer
		if c.Closing != want {
			t.Fatalf("kolom %s tidak menutup: closing=%d want=%d", c.Code, c.Closing, want)
		}
	}

	if s.UnclosedTransfer != rp(6_500_000) || s.UnclosedClosing != 0 {
		t.Fatalf("kolom laba belum ditutup salah: transfer=%d closing=%d", s.UnclosedTransfer, s.UnclosedClosing)
	}

	// Penutup ke Neraca: jumlah ekuitas laporan ini = ekuitas Neraca.
	equity, unclosed := neracaEquity(scenario)
	if s.TotalClosing != equity+unclosed {
		t.Fatalf("ekuitas tidak menutup ke Neraca: laporan=%d neraca=%d+%d", s.TotalClosing, equity, unclosed)
	}
	if s.TotalClosing != rp(108_500_000) {
		t.Fatalf("jumlah ekuitas akhir = %d, mau %d", s.TotalClosing, rp(108_500_000))
	}
}

func TestBuildEquityTanpaJurnalPenutup(t *testing.T) {
	scenario := fullScenario()
	var noClosing []testJournal
	for _, j := range scenario {
		if j.bucket != "closing" {
			noClosing = append(noClosing, j)
		}
	}
	s := BuildEquity(
		aggregate(noClosing, "opening"),
		aggregate(noClosing, "movements"),
		aggregate(noClosing, "closing"),
	)
	if s.UnclosedClosing != rp(6_500_000) {
		t.Fatalf("laba belum ditutup = %d, mau %d", s.UnclosedClosing, rp(6_500_000))
	}
	equity, unclosed := neracaEquity(noClosing)
	if s.TotalClosing != equity+unclosed {
		t.Fatalf("ekuitas tidak menutup ke Neraca: laporan=%d neraca=%d+%d", s.TotalClosing, equity, unclosed)
	}
}

func TestBuildCashFlowMenutupKeNeraca(t *testing.T) {
	scenario := fullScenario()
	c := BuildCashFlow(aggregate(scenario, "opening"), aggregate(scenario, "movements"))

	if c.CashBeginning != rp(65_000_000) {
		t.Fatalf("kas awal = %d, mau %d", c.CashBeginning, rp(65_000_000))
	}
	if c.CashEnd != rp(72_500_000) {
		t.Fatalf("kas akhir = %d, mau %d", c.CashEnd, rp(72_500_000))
	}
	if c.NetIncome != rp(6_500_000) {
		t.Fatalf("laba periode = %d, mau %d", c.NetIncome, rp(6_500_000))
	}

	// Penyesuaian non-kas: hanya penyusutan 6 juta.
	if len(c.NonCash) != 1 || c.NonCash[0].Name != "Penyusutan dan amortisasi" || c.NonCash[0].AmountCents != rp(6_000_000) {
		t.Fatalf("penyesuaian non-kas salah: %+v", c.NonCash)
	}

	// Tanda perubahan modal kerja: aset naik = arus keluar, liabilitas naik =
	// arus masuk (bug tanda yang pernah ada di sini).
	if got := lineByName(t, c.WorkingCapital, "Perubahan piutang usaha dan piutang lainnya").AmountCents; got != rp(-5_000_000) {
		t.Fatalf("perubahan piutang = %d, mau %d", got, rp(-5_000_000))
	}
	if got := lineByName(t, c.WorkingCapital, "Perubahan persediaan").AmountCents; got != rp(-7_000_000) {
		t.Fatalf("perubahan persediaan = %d, mau %d", got, rp(-7_000_000))
	}
	if got := lineByName(t, c.WorkingCapital, "Perubahan utang usaha").AmountCents; got != rp(7_000_000) {
		t.Fatalf("perubahan utang usaha = %d, mau %d (utang naik = arus masuk)", got, rp(7_000_000))
	}

	if got := c.TotalInvesting(); got != rp(-20_000_000) {
		t.Fatalf("arus investasi = %d, mau %d", got, rp(-20_000_000))
	}
	if got := lineByName(t, c.Financing, "Setoran modal pemilik").AmountCents; got != rp(10_000_000) {
		t.Fatalf("setoran modal = %d", got)
	}
	if got := lineByName(t, c.Financing, "Prive dan dividen dibayarkan").AmountCents; got != rp(-5_000_000) {
		t.Fatalf("prive = %d", got)
	}
	if got := lineByName(t, c.Financing, "Perubahan utang bank (neto)").AmountCents; got != rp(15_000_000) {
		t.Fatalf("utang bank = %d", got)
	}

	// Penutup ke Neraca: kenaikan kas laporan = perubahan kas menurut jurnal.
	kasAwal := cashDelta(scenario, "opening")
	kasMutasi := cashDelta(scenario, "movements")
	if c.CashBeginning != kasAwal {
		t.Fatalf("kas awal %d != %d menurut jurnal", c.CashBeginning, kasAwal)
	}
	if c.NetChange() != kasMutasi {
		t.Fatalf("kenaikan kas %d != mutasi kas %d menurut jurnal", c.NetChange(), kasMutasi)
	}
	if c.CashEnd != c.CashBeginning+c.NetChange() {
		t.Fatalf("kas akhir %d != awal + kenaikan %d", c.CashEnd, c.CashBeginning+c.NetChange())
	}
	if c.OperatingNet()+c.TotalInvesting()+c.TotalFinancing() != c.NetChange() {
		t.Fatalf("jumlah tiga aktivitas != kenaikan kas neto")
	}
}

func TestIsCashAccount(t *testing.T) {
	for _, code := range []string{"1-1000", "1-1100"} {
		for _, a := range testChart {
			if a.Code == code && !IsCashAccount(a) {
				t.Fatalf("akun %s harus termasuk kas dan setara kas", code)
			}
		}
	}
	for _, code := range []string{"1-2000", "1-4000", "2-1000", "3-1000", "4-1000"} {
		for _, a := range testChart {
			if a.Code == code && IsCashAccount(a) {
				t.Fatalf("akun %s tidak boleh termasuk kas", code)
			}
		}
	}
}

func TestMergeLinesByName(t *testing.T) {
	cur := []ReportLine{{Name: "A", AmountCents: 100}, {Name: "B", AmountCents: 200}}
	pri := []ReportLine{{Name: "B", AmountCents: 50}, {Name: "C", AmountCents: 70}}
	got := mergeLinesByName(cur, pri)
	if len(got) != 3 {
		t.Fatalf("jumlah baris = %d, mau 3", len(got))
	}
	if got[0].Name != "A" || got[0].PriorAmountCents != 0 {
		t.Fatalf("baris A salah: %+v", got[0])
	}
	if got[1].Name != "B" || got[1].AmountCents != 200 || got[1].PriorAmountCents != 50 {
		t.Fatalf("baris B salah: %+v", got[1])
	}
	if got[2].Name != "C" || got[2].AmountCents != 0 || got[2].PriorAmountCents != 70 {
		t.Fatalf("baris C salah: %+v", got[2])
	}
}

func TestCashFlowViewMergeDanTotal(t *testing.T) {
	v := CashFlowView{
		Cur: CashFlow{
			NetIncome:      rp(6_500_000),
			NonCash:        []ReportLine{{Name: "Penyusutan dan amortisasi", AmountCents: rp(6_000_000)}},
			WorkingCapital: []ReportLine{{Name: "Perubahan piutang", AmountCents: rp(-5_000_000)}},
			CashBeginning:  rp(65_000_000),
			CashEnd:        rp(72_500_000),
		},
		Pri: &CashFlow{
			NetIncome:      rp(15_000_000),
			NonCash:        []ReportLine{{Name: "Penyusutan dan amortisasi", AmountCents: rp(4_000_000)}},
			WorkingCapital: []ReportLine{{Name: "Perubahan piutang", AmountCents: rp(2_000_000)}},
			CashBeginning:  rp(50_000_000),
			CashEnd:        rp(65_000_000),
		},
	}
	v.MergeLines()
	if v.PriorNetIncome != rp(15_000_000) || v.PriorCashEnd != rp(65_000_000) {
		t.Fatalf("kolom pembanding salah: %+v", v)
	}
	if v.PriorTotalNonCash() != rp(4_000_000) || v.TotalNonCash() != rp(6_000_000) {
		t.Fatalf("jumlah non-kas salah: cur=%d pri=%d", v.TotalNonCash(), v.PriorTotalNonCash())
	}
	if v.PriorOperatingNet() != rp(15_000_000)+rp(4_000_000)+rp(2_000_000) {
		t.Fatalf("operasi pembanding = %d", v.PriorOperatingNet())
	}
	if v.OperatingNet() != rp(6_500_000)+rp(6_000_000)-rp(5_000_000) {
		t.Fatalf("operasi berjalan = %d", v.OperatingNet())
	}
}

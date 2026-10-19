package models

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DashboardView adalah seluruh data dashboard perusahaan: ringkasan posisi
// keuangan, laba periode berjalan, rasio ringkas, seri bulanan, dan jurnal
// terbaru. Semua angka dalam sen.
type DashboardView struct {
	PeriodName    string
	PeriodStatus  string
	AsOfLabel     string
	RoundingUnit  string
	RoundingLabel string
	HasPeriod     bool
	JournalCount  int64

	// Pilihan periode tahun pembukuan (tahun kalender bebas, tidak statis
	// year-to-date). Years berisi tahun yang punya jurnal ditambah tahun
	// berjalan; SelectedYear adalah tahun aktif.
	Years        []int
	SelectedYear int

	TotalAssets      int64
	TotalLiabilities int64
	TotalEquity      int64
	UnclosedProfit   int64
	Revenue          int64
	Expense          int64
	NetIncome        int64

	CurrentAssets      int64
	CurrentLiabilities int64
	NonCurrentAssets   int64
	CashTotal          int64
	ReceivableTotal    int64

	CashLines       []ReportLine
	ReceivableLines []ReportLine
	PayableLines    []ReportLine
	TaxPayable      int64
	BankLoan        int64
	Modals          []ReportLine

	LineChart ChartData
	BarChart  ChartData
	Recent    []RecentJournal

	Range        string        // rentang grafik aktif: month, quarter, year
	RangeOptions []RangeOption // pilihan rentang untuk tombol filter
	SeriesTitle  string        // judul kartu tren sesuai rentang
}

// RecentJournal adalah satu baris jurnal terbaru pada dashboard.
type RecentJournal struct {
	Reference string
	DateLabel string
	Memo      string
	Amount    int64
}

// Money menampilkan nilai sen memakai satuan pembulatan entitas.
func (v DashboardView) Money(cents int64) string {
	return FormatRounded(cents, v.RoundingUnit)
}

// SumLines menjumlahkan nilai beberapa baris laporan.
func SumLines(lines []ReportLine) int64 {
	var total int64
	for _, l := range lines {
		total += l.AmountCents
	}
	return total
}

// RatioLancar menghitung rasio lancar (aset lancar dibagi liabilitas jangka
// pendek). Nol berarti tidak dapat dihitung karena liabilitas jangka pendek nol.
func (v DashboardView) RatioLancar() float64 {
	if v.CurrentLiabilities == 0 {
		return 0
	}
	return float64(v.CurrentAssets) / float64(v.CurrentLiabilities)
}

// RasioLiabilitasEkuitas menghitung DER (dalam persen).
func (v DashboardView) RasioLiabilitasEkuitas() float64 {
	equity := v.TotalEquity + v.UnclosedProfit
	if equity == 0 {
		return 0
	}
	return float64(v.TotalLiabilities) / float64(equity) * 100
}

// MarginLaba menghitung margin laba periode (dalam persen).
func (v DashboardView) MarginLaba() float64 {
	if v.Revenue == 0 {
		return 0
	}
	return float64(v.NetIncome) / float64(v.Revenue) * 100
}

// FormatFloat memformat angka desimal gaya Indonesia (koma sebagai desimal).
func FormatFloat(v float64, desimal int) string {
	s := strconv.FormatFloat(v, 'f', desimal, 64)
	return strings.ReplaceAll(s, ".", ",")
}

// ChartLabel adalah label sumbu X pada grafik.
type ChartLabel struct {
	X    string
	Text string
}

// ChartBar adalah satu batang pada grafik bulanan.
type ChartBar struct {
	X        string
	Y        string
	W        string
	H        string
	Positive bool
	Title    string
	Label    string
	NetText  string
}

// GridLine adalah garis bantu horizontal beserta label nilainya. Class dipakai
// menempatkan label sumbu Y di luar gambar (lewat kelas Tailwind, karena templ
// tidak menerima atribut style bernilai ekspresi) supaya ukuran hurufnya tidak
// ikut membesar pada layar lebar.
type GridLine struct {
	Y     string // posisi garis bantu
	X     string // posisi label sumbu Y di dalam gambar
	Label string
	Top   bool
}

// Rentang (granularitas) grafik dashboard.
const (
	GranMonth   = "month"
	GranQuarter = "quarter"
	GranYear    = "year"
)

// ChartPoint adalah satu titik data grafik beserta nilai teksnya, dipakai
// lapisan interaktif (hover) di sisi klien.
type ChartPoint struct {
	Index   int
	X       string
	YRev    string
	YExp    string
	YTop    string
	BandX   string
	BandW   string
	Label   string
	RevText string
	ExpText string
	NetText string
}

// RangeOption adalah satu pilihan rentang grafik pada tombol filter.
type RangeOption struct {
	Value  string
	Label  string
	Active bool
}

// NormalizeRange memastikan rentang yang dikirim pengguna dikenal; nilai lain
// dikembalikan ke rentang bulanan.
func NormalizeRange(v string) string {
	switch v {
	case GranQuarter, GranYear:
		return v
	default:
		return GranMonth
	}
}

// RangeOptions menyusun tombol filter rentang beserta penanda aktif.
func RangeOptions(active string) []RangeOption {
	return []RangeOption{
		{Value: GranMonth, Label: "Bulanan", Active: active == GranMonth},
		{Value: GranQuarter, Label: "Kuartalan", Active: active == GranQuarter},
		{Value: GranYear, Label: "Tahunan", Active: active == GranYear},
	}
}

// RangeTitle adalah judul kartu grafik tren sesuai rentang aktif.
func RangeTitle(gran string) string {
	switch gran {
	case GranQuarter:
		return "Tren Pendapatan dan Beban · 8 Kuartal"
	case GranYear:
		return "Tren Pendapatan dan Beban · 5 Tahun"
	default:
		return "Tren Pendapatan dan Beban · 12 Bulan"
	}
}

// SeriesKeys mengembalikan tanggal awal jendela grafik dan daftar kunci
// keranjang seri: bulan (12 bulan terakhir), kuartal (8 kuartal terakhir),
// atau tahun (5 tahun terakhir).
func SeriesKeys(gran string, asOf time.Time) (time.Time, []string) {
	ym := time.Date(asOf.Year(), asOf.Month(), 1, 0, 0, 0, 0, time.UTC)
	switch gran {
	case GranQuarter:
		end := ym.AddDate(0, -((int(ym.Month()) - 1) % 3), 0) // bulan pertama kuartal berjalan
		start := end.AddDate(0, -21, 0)                       // 8 kuartal
		keys := make([]string, 0, 8)
		for i := 0; i < 8; i++ {
			b := start.AddDate(0, i*3, 0)
			keys = append(keys, fmt.Sprintf("%d-Q%d", b.Year(), (int(b.Month())-1)/3+1))
		}
		return start, keys
	case GranYear:
		start := time.Date(asOf.Year()-4, 1, 1, 0, 0, 0, 0, time.UTC)
		keys := make([]string, 0, 5)
		for i := 0; i < 5; i++ {
			keys = append(keys, strconv.Itoa(start.Year()+i))
		}
		return start, keys
	default:
		start := ym.AddDate(0, -11, 0)
		keys := make([]string, 0, 12)
		for i := 0; i < 12; i++ {
			keys = append(keys, start.AddDate(0, i, 0).Format("2006-01"))
		}
		return start, keys
	}
}

// BucketKey memetakan kunci bulanan "YYYY-MM" ke keranjang seri grafik.
func BucketKey(gran string, bulan string) string {
	if len(bulan) < 7 {
		return bulan
	}
	switch gran {
	case GranQuarter:
		y, err1 := strconv.Atoi(bulan[:4])
		mo, err2 := strconv.Atoi(bulan[5:7])
		if err1 != nil || err2 != nil {
			return bulan
		}
		return fmt.Sprintf("%d-Q%d", y, (mo-1)/3+1)
	case GranYear:
		return bulan[:4]
	default:
		return bulan
	}
}

// SeriesLabel memberi label sumbu X untuk satu keranjang seri.
func SeriesLabel(gran string, key string) string {
	switch gran {
	case GranQuarter:
		if len(key) == 7 {
			return "Q" + key[6:] + " " + key[2:4]
		}
		return key
	case GranYear:
		return key
	default:
		return MonthLabel(key)
	}
}

// ChartData adalah geometri grafik yang sudah siap dirender sebagai SVG.
// Semua koordinat berupa string supaya templ tidak perlu memformat angka.
type ChartData struct {
	Width          string
	Height         string
	RevPath        string
	ExpPath        string
	AreaG          string
	Bars           []ChartBar
	XLabels        []ChartLabel
	Grid           []GridLine
	AxisX          string       // posisi garis sumbu Y di dalam gambar
	AxisY          string       // posisi garis sumbu X di dalam gambar
	AxisEnd        string       // ujung garis sumbu X
	ZeroY          string       // posisi garis nol pada grafik batang
	CompressCenter float64      // titik mulai pemampatan skala (median nilai)
	Compressed     bool         // true bila skala benar-benar dimampatkan
	Points         []ChartPoint // titik data untuk hover interaktif
	HasData        bool
	ScaleNote      string // keterangan skala; diisi handler memakai satuan pembulatan entitas
	LabelStep      int    // tampilkan label sumbu X setiap N bulan
	MaxCents       int64  // nilai tertinggi pada grafik (sen)
	MinCents       int64  // nilai terendah pada grafik (sen)
}

// chartGeom menyimpan konstanta tata letak grafik dalam satuan viewBox.
const (
	chartWidth    = 1000.0
	chartHeight   = 300.0
	barHeight     = 200.0
	chartPadL     = 78.0 // ruang label sumbu Y di dalam gambar
	chartPadR     = 4.0
	chartPadT     = 0.0
	chartPadB     = 0.0
	maxXAxisLabel = 8
)

func f(v float64) string {
	return strconv.FormatFloat(v, 'f', 1, 64)
}

// nilaiSumbu menyusun nilai bulat untuk label sumbu Y pada rentang data.
// Angka dibulatkan ke kelipatan 1, 2, 2,5, atau 5 dikali pangkat sepuluh
// supaya labelnya enak dibaca, bukan angka ganjil hasil pemetaan balik.
func nilaiSumbu(minV, maxV int64, jumlah int) []int64 {
	if jumlah < 2 {
		jumlah = 2
	}
	if maxV <= minV {
		maxV = minV + 1
	}
	step := langkahBulat(float64(maxV-minV) / float64(jumlah-1))
	lo := math.Floor(float64(minV)/step) * step
	hi := math.Ceil(float64(maxV)/step) * step
	var out []int64
	for v := lo; v <= hi+step/2; v += step {
		out = append(out, int64(v))
		if len(out) >= 9 {
			break
		}
	}
	return out
}

// langkahBulat membulatkan jarak antar label ke angka bulat terdekat.
func langkahBulat(raw float64) float64 {
	if raw <= 0 {
		return 1
	}
	mag := math.Pow(10, math.Floor(math.Log10(raw)))
	for _, m := range []float64{1, 2, 2.5, 5, 10} {
		if raw <= m*mag {
			return m * mag
		}
	}
	return 10 * mag
}

// compressF memampatkan nilai absolut di atas pusat c. Di bawah c pemetaan
// linier; di atasnya makin besar nilai makin kecil tambahannya, sehingga satu
// nilai ekstrem (biasanya minus besar) tidak menjulur sampai dasar grafik.
func compressF(a, c float64) float64 {
	if a <= c || c <= 0 {
		return a
	}
	return c + c*(1-math.Exp(-(a-c)/c))
}

// compressInv adalah kebalikan compressF, dipakai melabeli garis bantu dengan
// nilai sebenarnya.
func compressInv(y, c float64) float64 {
	if y <= c || c <= 0 {
		return y
	}
	if d := 1 - (y-c)/c; d > 0 {
		return c - c*math.Log(d)
	}
	return 2 * c
}

// maxAbsInt mengembalikan nilai absolut terbesar.
func maxAbsInt(values []int64) float64 {
	var max float64
	for _, v := range values {
		if a := math.Abs(float64(v)); a > max {
			max = a
		}
	}
	return max
}

// medianAbs menghitung median nilai absolut (pusat yang tahan terhadap
// pencilan), dipakai sebagai titik mulai pemampatan skala.
func medianAbs(values []int64) float64 {
	abs := make([]float64, 0, len(values))
	for _, v := range values {
		if v != 0 {
			abs = append(abs, math.Abs(float64(v)))
		}
	}
	if len(abs) == 0 {
		return 0
	}
	sort.Float64s(abs)
	return abs[len(abs)/2]
}

// BuildLineChart menyusun grafik garis pendapatan dan beban per keranjang seri.
// fmtValue memformat nilai untuk keterangan hover (dengan mata uang), fmtAxis
// memformat label sumbu Y (tanpa mata uang supaya ringkas).
func BuildLineChart(labels []string, revenue []int64, expense []int64,
	fmtValue, fmtAxis func(int64) string) ChartData {
	c := ChartData{
		Width:   f(chartWidth),
		Height:  f(chartHeight),
		HasData: len(labels) > 1,
	}
	plotW := chartWidth - chartPadL - chartPadR
	plotH := chartHeight - chartPadT - chartPadB

	maxV := int64(0)
	for i := range labels {
		if revenue[i] > maxV {
			maxV = revenue[i]
		}
		if expense[i] > maxV {
			maxV = expense[i]
		}
	}
	if maxV == 0 || len(labels) == 0 {
		return c
	}
	c.LabelStep = labelStep(len(labels))
	// Rentang data sebelum pemetaan, dipakai menyusun label sumbu Y bulat.
	minRaw, maxRaw := int64(0), int64(0)
	for i := range labels {
		for _, v := range []int64{revenue[i], expense[i]} {
			if v > maxRaw {
				maxRaw = v
			}
			if v < minRaw {
				minRaw = v
			}
		}
	}
	c.MaxCents = maxRaw

	// Skala vertikal: nilai dalam rentang umum linier, nilai jauh di atas
	// median dimampatkan supaya garis/area tetap terbaca penuh.
	all := make([]int64, 0, len(revenue)+len(expense))
	all = append(all, revenue...)
	all = append(all, expense...)
	c.CompressCenter = medianAbs(all)
	c.Compressed = maxAbsInt(all) > 2*c.CompressCenter && c.CompressCenter > 0
	mapV := func(v int64) float64 {
		if v < 0 {
			return -compressF(float64(-v), c.CompressCenter)
		}
		return compressF(float64(v), c.CompressCenter)
	}

	// Label sumbu Y memakai nilai bulat; posisinya dihitung dari pemetaan
	// sehingga garis bantu dan label selalu sejajar.
	sumbu := nilaiSumbu(minRaw, maxRaw, 5)
	maxM, minM := 0.0, 0.0
	for i := range labels {
		for _, v := range []int64{revenue[i], expense[i]} {
			if mv := mapV(v); mv > maxM {
				maxM = mv
			} else if mv < minM {
				minM = mv
			}
		}
	}
	for _, tv := range sumbu {
		if mv := mapV(tv); mv > maxM {
			maxM = mv
		} else if mv < minM {
			minM = mv
		}
	}
	if maxM-minM == 0 {
		return c
	}

	x := func(i int) float64 {
		if len(labels) == 1 {
			return chartPadL + plotW/2
		}
		return chartPadL + float64(i)*plotW/float64(len(labels)-1)
	}
	y := func(v int64) float64 {
		return plotH - (mapV(v)-minM)/(maxM-minM)*plotH
	}
	posisi := func(mv float64) float64 {
		return plotH - (mv-minM)/(maxM-minM)*plotH
	}

	// Garis bantu + label sumbu Y pada nilai bulat.
	c.AxisX = f(chartPadL)
	c.AxisY = f(plotH)
	c.AxisEnd = f(chartWidth - chartPadR)
	for i, tv := range sumbu {
		c.Grid = append(c.Grid, GridLine{
			Y:     f(posisi(mapV(tv))),
			X:     f(chartPadL - 10),
			Label: fmtAxis(tv),
			Top:   i == len(sumbu)-1,
		})
	}

	// Garis pendapatan, garis beban, label sumbu X, dan titik untuk hover.
	step := plotW / float64(len(labels)-1)
	rev := make([]string, len(labels))
	exp := make([]string, len(labels))
	for i := range labels {
		rev[i] = "L" + f(x(i)) + " " + f(y(revenue[i]))
		exp[i] = "L" + f(x(i)) + " " + f(y(expense[i]))
		c.XLabels = append(c.XLabels, ChartLabel{X: f(x(i)), Text: labels[i]})
		bandX := math.Max(0, x(i)-step/2)
		bandW := math.Min(chartWidth, x(i)+step/2) - bandX
		c.Points = append(c.Points, ChartPoint{
			Index:   i,
			X:       f(x(i)),
			YRev:    f(y(revenue[i])),
			YExp:    f(y(expense[i])),
			YTop:    f(math.Min(y(revenue[i]), y(expense[i]))),
			BandX:   f(bandX),
			BandW:   f(bandW),
			Label:   labels[i],
			RevText: fmtValue(revenue[i]),
			ExpText: fmtValue(expense[i]),
			NetText: fmtValue(revenue[i] - expense[i]),
		})
	}
	c.RevPath = "M" + strings.Join(rev, " ")[1:]
	c.ExpPath = "M" + strings.Join(exp, " ")[1:]
	// Area di bawah garis pendapatan untuk memberi bobot visual.
	c.AreaG = c.RevPath + " L" + f(x(len(labels)-1)) + " " + f(plotH) +
		" L" + f(x(0)) + " " + f(plotH) + " Z"

	return c
}

// BuildBarChart menyusun grafik batang laba bersih per keranjang seri.
// fmtValue untuk keterangan hover, fmtAxis untuk label sumbu Y.
func BuildBarChart(labels []string, net []int64, fmtValue, fmtAxis func(int64) string) ChartData {
	c := ChartData{Width: f(chartWidth), Height: f(barHeight), HasData: len(labels) > 0}
	plotW := chartWidth - chartPadL - chartPadR
	plotH := barHeight - chartPadT - chartPadB

	var maxV, minV int64
	for _, v := range net {
		if v > maxV {
			maxV = v
		}
		if v < minV {
			minV = v
		}
	}
	if len(labels) == 0 || (maxV == 0 && minV == 0) {
		return c
	}
	c.LabelStep = labelStep(len(labels))
	c.MaxCents = maxV
	c.MinCents = minV

	// Skala dipadatkan: bulan yang ruginya jauh lebih besar dari laba bulan
	// lain tidak menjulur sampai dasar kartu, sementara angka sebenarnya tetap
	// muncul di hover dan di tabel.
	c.CompressCenter = medianAbs(net)
	c.Compressed = maxAbsInt(net) > 2*c.CompressCenter && c.CompressCenter > 0
	mapV := func(v int64) float64 {
		if v < 0 {
			return -compressF(float64(-v), c.CompressCenter)
		}
		return compressF(float64(v), c.CompressCenter)
	}

	sumbu := nilaiSumbu(minV, maxV, 5)
	maxM, minM := 0.0, 0.0
	for _, v := range net {
		if mv := mapV(v); mv > maxM {
			maxM = mv
		} else if mv < minM {
			minM = mv
		}
	}
	for _, tv := range sumbu {
		if mv := mapV(tv); mv > maxM {
			maxM = mv
		} else if mv < minM {
			minM = mv
		}
	}
	if maxM-minM == 0 {
		return c
	}
	posisi := func(mv float64) float64 {
		return plotH - (mv-minM)/(maxM-minM)*plotH
	}
	toY := func(v int64) float64 { return posisi(mapV(v)) }
	zeroY := toY(0)
	c.ZeroY = f(zeroY)

	// Sumbu dan garis bantu grafik batang beserta label nilai bulat.
	c.AxisX = f(chartPadL)
	c.AxisY = f(plotH)
	c.AxisEnd = f(chartWidth - chartPadR)
	for i, tv := range sumbu {
		c.Grid = append(c.Grid, GridLine{
			Y:     f(posisi(mapV(tv))),
			X:     f(chartPadL - 10),
			Label: fmtAxis(tv),
			Top:   i == len(sumbu)-1,
		})
	}

	slot := plotW / float64(len(labels))
	barW := math.Min(slot*0.6, 42)
	for i, v := range net {
		y1, y2 := toY(v), zeroY
		top := math.Min(y1, y2)
		h := math.Max(math.Abs(y1-y2), 0.8)
		cx := chartPadL + slot*float64(i) + slot/2
		c.Bars = append(c.Bars, ChartBar{
			X:        f(cx - barW/2),
			Y:        f(top),
			W:        f(barW),
			H:        f(h),
			Positive: v >= 0,
			Title:    labels[i] + ": " + FormatCents(v),
			Label:    labels[i],
			NetText:  fmtValue(v),
		})
		c.XLabels = append(c.XLabels, ChartLabel{X: f(cx), Text: labels[i]})
	}
	return c
}

// labelStep menentukan setiap berapa bulan label sumbu X ditampilkan supaya
// tidak bertumpuk pada layar sempit.
func labelStep(n int) int {
	if n <= maxXAxisLabel {
		return 1
	}
	return 2
}

// FormatThousands memformat angka rupiah (bukan sen) dengan pemisah ribuan.
func FormatThousands(v int64) string {
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

// MonthLabel mengubah "2026-03" menjadi "Mar 26".
func MonthLabel(bulan string) string {
	parts := strings.Split(bulan, "-")
	if len(parts) != 2 {
		return bulan
	}
	nama := []string{"Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 1 || m > 12 {
		return bulan
	}
	return nama[m-1] + " " + parts[0][2:]
}

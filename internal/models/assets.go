package models

import "time"

// Register aset tetap (PSAK 216): kalkulasi depresiasi, akumulasi, dan nilai
// buku sampai tanggal laporan. Jurnal depresiasi dibuat manual oleh user
// (penyesuaian) lewat akun aset tetap + akumulasi penyusutan di COA; modul ini
// hanya menyediakan kalkulasi rekomendasi dan tampilannya.
//
// Aturan hitung (sen, matematika bilangan bulat):
//   - Garis lurus (straight_line): (cost - residual) / masa manfaat per tahun,
//     dibagi proporsional per bulan penuh sejak tanggal perolehan.
//   - Saldo menurun (declining_balance): tarif tetap 2x tarif garis lurus per
//     tahun yang dihitung atas nilai buku awal tahun, dibatasi agar nilai buku
//     tidak jatuh di bawah residual, dan dipotong proporsional untuk tahun
//     berjalan yang belum genap.
//   - Akumulasi berhenti setelah masa manfaat habis dan tidak pernah melampaui
//     harga perolehan dikurangi residual.

// Nama metode depresiasi di database (enum fixed_asset_method).
const (
	MethodStraightLine = "straight_line"
	MethodDeclining    = "declining_balance"
)

// DepMethodLabel menerjemahkan kode metode ke label tampilan.
func DepMethodLabel(method string) string {
	if method == MethodDeclining {
		return "Saldo menurun"
	}
	return "Garis lurus"
}

// FixedAsset adalah satu aset tetap terdaftar. Nilai uang dalam sen.
type FixedAsset struct {
	ID              int64
	CompanyID       int64
	Name            string
	Code            string
	CostCents       int64
	AcquisitionDate time.Time
	UsefulLifeYears int
	ResidualCents   int64
	Method          string
	IsActive        bool
}

// AssetDepreciation adalah hasil kalkulasi satu aset per tanggal laporan.
type AssetDepreciation struct {
	AccumulatedCents int64 // akumulasi penyusutan sampai tanggal laporan
	BookCents        int64 // nilai buku = harga perolehan - akumulasi
}

// Depreciate menghitung akumulasi depresiasi dan nilai buku satu aset sampai
// tanggal laporan. Aset yang belum berumur satu bulan penuh, atau masa
// manfaatnya tidak valid, belum menyusut.
func Depreciate(a FixedAsset, asOf time.Time) AssetDepreciation {
	accum := AccumulatedDepreciation(a.CostCents, a.ResidualCents,
		a.AcquisitionDate, a.UsefulLifeYears, a.Method, asOf)
	return AssetDepreciation{AccumulatedCents: accum, BookCents: a.CostCents - accum}
}

// AccumulatedDepreciation menghitung akumulasi penyusutan (sen) sampai asOf.
// Proporsi tahun berjalan memakai bulan penuh sejak tanggal perolehan:
// bulan dihitung genap bila tanggal laporan sudah mencapai hari yang sama
// dengan tanggal perolehan.
func AccumulatedDepreciation(costCents, residualCents int64, acq time.Time, lifeYears int, method string, asOf time.Time) int64 {
	depreciable := costCents - residualCents
	if lifeYears <= 0 || depreciable <= 0 {
		return 0
	}
	months := completeMonths(acq, asOf)
	lifeMonths := int64(lifeYears) * 12
	if int64(months) > lifeMonths {
		months = int(lifeMonths)
	}
	if months <= 0 {
		return 0
	}
	if method == MethodDeclining {
		return decliningAccumulated(costCents, residualCents, lifeYears, months)
	}
	// Garis lurus: depreciable * months / lifeMonths, dibagi dua dulu supaya
	// perkalian tidak melimpah untuk nilai besar.
	m := int64(months)
	q := depreciable / lifeMonths
	r := depreciable % lifeMonths
	return q*m + r*m/lifeMonths
}

// completeMonths menghitung jumlah bulan penuh antara acq dan asOf.
func completeMonths(acq, asOf time.Time) int {
	m := (asOf.Year()-acq.Year())*12 + int(asOf.Month()) - int(acq.Month())
	if asOf.Day() < acq.Day() {
		m--
	}
	return m
}

// decliningAccumulated menghitung akumulasi metode saldo menurun: tarif tetap
// 2x tarif garis lurus per tahun atas nilai buku awal tahun, tidak pernah
// jatuh di bawah residual. Bulan yang belum genap di tahun berjalan
// dipotong proporsional.
func decliningAccumulated(cost, residual int64, lifeYears, months int) int64 {
	nbv := cost
	accum := int64(0)
	for year := 0; year < lifeYears; year++ {
		m := months - year*12
		if m <= 0 {
			break
		}
		if m > 12 {
			m = 12
		}
		full := nbv * 2 / int64(lifeYears) // 2x tarif garis lurus atas nilai buku
		if max := nbv - residual; full > max {
			full = max
		}
		if full < 0 {
			full = 0
		}
		dep := full/12*int64(m) + (full%12)*int64(m)/12
		accum += dep
		nbv -= dep
	}
	return accum
}

// AssetRow adalah satu baris register aset beserta kalkulasi per tanggal
// laporan.
type AssetRow struct {
	FixedAsset
	AcqLabel        string // tanggal perolehan dd/mm/yyyy
	MethodLabel     string
	AccumulatedCents int64
	BookCents        int64
}

// AssetCOABuku adalah saldo akun COA terkait aset tetap menurut buku besar
// (dari jurnal yang dibuat manual user), dipakai sebagai pembanding rekomendasi.
type AssetCOABuku struct {
	CostCents  int64 // saldo debit akun psak_line aset_tetap
	AccumCents int64 // saldo kredit akun psak_line aset_tetap_akumulasi
	BookCents  int64 // CostCents - AccumCents
}

// AssetsView adalah data halaman register aset tetap.
type AssetsView struct {
	AsOfISO       string
	AsOfLabel     string
	Rows          []AssetRow
	Total         AssetRow
	Buku          AssetCOABuku
	RoundingUnit  string
	RoundingLabel string
	CanManage     bool
}

// Money menampilkan nilai sen memakai satuan pembulatan entitas.
func (v AssetsView) Money(cents int64) string {
	return FormatRounded(cents, v.RoundingUnit)
}

// BuildAssetRows menghitung register: baris per aset plus baris jumlah.
// Urutan baris mengikuti urutan query (tanggal perolehan, kode, id).
func BuildAssetRows(assets []FixedAsset, asOf time.Time) ([]AssetRow, AssetRow) {
	rows := make([]AssetRow, 0, len(assets))
	var total AssetRow
	for _, a := range assets {
		d := Depreciate(a, asOf)
		r := AssetRow{
			FixedAsset:       a,
			AcqLabel:         FormatDateID(a.AcquisitionDate),
			MethodLabel:      DepMethodLabel(a.Method),
			AccumulatedCents: d.AccumulatedCents,
			BookCents:        d.BookCents,
		}
		rows = append(rows, r)
		total.CostCents += r.CostCents
		total.AccumulatedCents += r.AccumulatedCents
		total.BookCents += r.BookCents
	}
	return rows, total
}

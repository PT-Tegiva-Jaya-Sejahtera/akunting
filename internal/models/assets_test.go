package models

import (
	"testing"
	"time"
)

// Uji angka depresiasi untuk data uji: tiga aset dengan metode berbeda dan
// tanggal perolehan berbeda, ditambah sifat batas (residual, masa manfaat).

func tgl(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// TestDepresiasiGarisLurus: aset 240 juta, masa manfaat 4 tahun, tanpa
// residual. Akumulasi harus proporsional per bulan penuh, bukan penuh untuk
// tahun berjalan yang belum selesai.
func TestDepresiasiGarisLurus(t *testing.T) {
	a := FixedAsset{
		CostCents: rp(240_000_000), AcquisitionDate: tgl(2026, 1, 1),
		UsefulLifeYears: 4, Method: MethodStraightLine,
	}
	// 9 bulan penuh (Jan s.d. Okt) dari 48 bulan: 240jt * 9/48 = 45jt.
	d := Depreciate(a, tgl(2026, 10, 15))
	if d.AccumulatedCents != rp(45_000_000) || d.BookCents != rp(195_000_000) {
		t.Errorf("9 bulan: akumulasi %d (mau %d), nilai buku %d (mau %d)",
			d.AccumulatedCents, rp(45_000_000), d.BookCents, rp(195_000_000))
	}
	// 24 bulan = separuh masa manfaat: 120jt.
	d = Depreciate(a, tgl(2028, 1, 1))
	if d.AccumulatedCents != rp(120_000_000) {
		t.Errorf("24 bulan: akumulasi %d (mau %d)", d.AccumulatedCents, rp(120_000_000))
	}
	// Setelah masa manfaat habis: penuh 240jt, tidak lebih.
	d = Depreciate(a, tgl(2032, 6, 1))
	if d.AccumulatedCents != rp(240_000_000) || d.BookCents != 0 {
		t.Errorf("lewat masa manfaat: akumulasi %d (mau %d), nilai buku %d (mau 0)",
			d.AccumulatedCents, rp(240_000_000), d.BookCents)
	}
	// Belum satu bulan penuh: belum menyusut.
	d = Depreciate(a, tgl(2026, 1, 31))
	if d.AccumulatedCents != 0 {
		t.Errorf("belum genap 1 bulan: akumulasi %d (mau 0)", d.AccumulatedCents)
	}
}

// TestDepresiasiGarisLurusResidual: aset 100 juta, residual 10 juta, 5 tahun.
// Basis penyusutan = 90 juta, akumulasi tidak pernah melewati 90 juta.
func TestDepresiasiGarisLurusResidual(t *testing.T) {
	a := FixedAsset{
		CostCents: rp(100_000_000), ResidualCents: rp(10_000_000),
		AcquisitionDate: tgl(2026, 7, 1), UsefulLifeYears: 5, Method: MethodStraightLine,
	}
	// 3 bulan penuh (Jul s.d. Okt) dari 60 bulan: 90jt * 3/60 = 4,5jt.
	d := Depreciate(a, tgl(2026, 10, 15))
	if d.AccumulatedCents != rp(4_500_000) || d.BookCents != rp(95_500_000) {
		t.Errorf("3 bulan: akumulasi %d (mau %d), nilai buku %d (mau %d)",
			d.AccumulatedCents, rp(4_500_000), d.BookCents, rp(95_500_000))
	}
	// Masa manfaat habis: akumulasi tepat 90 juta, nilai buku = residual.
	d = Depreciate(a, tgl(2033, 3, 1))
	if d.AccumulatedCents != rp(90_000_000) || d.BookCents != rp(10_000_000) {
		t.Errorf("masa manfaat habis: akumulasi %d (mau %d), nilai buku %d (mau %d)",
			d.AccumulatedCents, rp(90_000_000), d.BookCents, rp(10_000_000))
	}
}

// TestDepresiasiSaldoMenurun: aset 80 juta, residual 8 juta, 4 tahun,
// tarif tetap 2x garis lurus (50% per tahun) atas nilai buku awal tahun.
// Tahun 1: 40jt (9 bulan = 30jt), tahun 2: 20jt, tahun 3: 10jt,
// tahun 4: 5jt dibatasi menjadi 2jt (agar nilai buku = residual 8jt).
func TestDepresiasiSaldoMenurun(t *testing.T) {
	a := FixedAsset{
		CostCents: rp(80_000_000), ResidualCents: rp(8_000_000),
		AcquisitionDate: tgl(2026, 1, 1), UsefulLifeYears: 4, Method: MethodDeclining,
	}
	// 9 bulan penuh tahun pertama: 40jt * 9/12 = 30jt.
	d := Depreciate(a, tgl(2026, 10, 15))
	if d.AccumulatedCents != rp(30_000_000) || d.BookCents != rp(50_000_000) {
		t.Errorf("9 bulan saldo menurun: akumulasi %d (mau %d), nilai buku %d (mau %d)",
			d.AccumulatedCents, rp(30_000_000), d.BookCents, rp(50_000_000))
	}
	// Genap 1 tahun: 40jt. Nilai buku 40jt.
	d = Depreciate(a, tgl(2027, 1, 1))
	if d.AccumulatedCents != rp(40_000_000) || d.BookCents != rp(40_000_000) {
		t.Errorf("1 tahun: akumulasi %d (mau %d), nilai buku %d (mau %d)",
			d.AccumulatedCents, rp(40_000_000), d.BookCents, rp(40_000_000))
	}
	// Masa manfaat habis: akumulasi 40+20+10+2 = 72jt, nilai buku = residual
	// 8jt (tidak pernah jatuh di bawah residual).
	d = Depreciate(a, tgl(2030, 1, 1))
	if d.AccumulatedCents != rp(72_000_000) || d.BookCents != rp(8_000_000) {
		t.Errorf("masa manfaat habis: akumulasi %d (mau %d), nilai buku %d (mau %d)",
			d.AccumulatedCents, rp(72_000_000), d.BookCents, rp(8_000_000))
	}
	// Melewati masa manfaat: tidak bertambah lagi dan tetap di atas residual.
	d = Depreciate(a, tgl(2035, 1, 1))
	if d.AccumulatedCents != rp(72_000_000) || d.BookCents != rp(8_000_000) {
		t.Errorf("lewat masa manfaat: akumulasi %d (mau %d), nilai buku %d (mau %d)",
			d.AccumulatedCents, rp(72_000_000), d.BookCents, rp(8_000_000))
	}
}

// TestDepresiasiTanggalPerolehan: proporsi memakai bulan penuh sejak tanggal
// perolehan; tanggal laporan sehari sebelum tanggal yang sama belum genap.
func TestDepresiasiTanggalPerolehan(t *testing.T) {
	a := FixedAsset{
		CostCents: rp(120_000_000), AcquisitionDate: tgl(2026, 1, 15),
		UsefulLifeYears: 4, Method: MethodStraightLine,
	}
	if got := Depreciate(a, tgl(2026, 7, 14)).AccumulatedCents; got != rp(12_500_000) {
		t.Errorf("5 bulan penuh (sehari sebelum genap 6): akumulasi %d (mau %d)", got, rp(12_500_000))
	}
	// Genap 6 bulan: 120jt * 6/48 = 15jt.
	if got := Depreciate(a, tgl(2026, 7, 15)).AccumulatedCents; got != rp(15_000_000) {
		t.Errorf("genap 6 bulan: akumulasi %d (mau %d)", got, rp(15_000_000))
	}
}

// TestDepresiasiKasusBatas: tanggal perolehan di masa depan, masa manfaat nol,
// residual sama dengan harga: tidak ada penyusutan.
func TestDepresiasiKasusBatas(t *testing.T) {
	asOf := tgl(2026, 10, 15)
	mkas := FixedAsset{CostCents: rp(10_000_000), AcquisitionDate: tgl(2027, 1, 1),
		UsefulLifeYears: 5, Method: MethodStraightLine}
	if got := Depreciate(mkas, asOf).AccumulatedCents; got != 0 {
		t.Errorf("aset belum diperoleh harus 0, dapat %d", got)
	}
	nol := FixedAsset{CostCents: rp(10_000_000), AcquisitionDate: tgl(2026, 1, 1),
		UsefulLifeYears: 0, Method: MethodStraightLine}
	if got := Depreciate(nol, asOf).AccumulatedCents; got != 0 {
		t.Errorf("masa manfaat nol harus 0, dapat %d", got)
	}
	penuh := FixedAsset{CostCents: rp(10_000_000), ResidualCents: rp(10_000_000),
		AcquisitionDate: tgl(2026, 1, 1), UsefulLifeYears: 5, Method: MethodDeclining}
	if got := Depreciate(penuh, asOf).AccumulatedCents; got != 0 {
		t.Errorf("residual = harga perolehan harus 0, dapat %d", got)
	}
}

// TestBuildAssetRowsJumlah: baris jumlah register = jumlah per baris.
func TestBuildAssetRowsJumlah(t *testing.T) {
	asets := []FixedAsset{
		{Name: "Kendaraan", CostCents: rp(240_000_000), AcquisitionDate: tgl(2026, 1, 1),
			UsefulLifeYears: 4, Method: MethodStraightLine},
		{Name: "Mesin", CostCents: rp(80_000_000), ResidualCents: rp(8_000_000),
			AcquisitionDate: tgl(2026, 1, 1), UsefulLifeYears: 4, Method: MethodDeclining},
	}
	rows, total := BuildAssetRows(asets, tgl(2026, 10, 15))
	if len(rows) != 2 {
		t.Fatalf("baris harus 2, dapat %d", len(rows))
	}
	if total.CostCents != rp(320_000_000) || total.AccumulatedCents != rp(75_000_000) ||
		total.BookCents != rp(245_000_000) {
		t.Errorf("jumlah: cost %d (mau %d), akumulasi %d (mau %d), buku %d (mau %d)",
			total.CostCents, rp(320_000_000), total.AccumulatedCents, rp(75_000_000),
			total.BookCents, rp(245_000_000))
	}
}

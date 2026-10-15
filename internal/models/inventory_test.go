package models

import (
	"errors"
	"testing"
)

// Uji angka biaya rata-rata tertimbang untuk data uji: tiga pembelian beda
// harga lalu satu penjualan, plus sifat batas (saldo kurang, urutan tanggal
// mundur, jumlah pecahan).

// TestRataRataTertimbang: 10 @ 10.000, lalu 5 @ 12.000, lalu 2 @ 8.000,
// lalu keluar 8. Harga rata-rata dihitung ulang setelah tiap pembelian;
// barang keluar dinilai dengan harga rata-rata saat itu.
func TestRataRataTertimbang(t *testing.T) {
	item := []InventoryItem{{ID: 1, CompanyID: 1, Name: "Kertas A4", Unit: "rim"}}
	moves := []InventoryMovement{
		{ID: 1, ItemID: 1, Date: tgl(2026, 10, 1), QtyMills: 10_000, UnitCostCents: rp(10_000), MovementType: MovementIn},
		{ID: 2, ItemID: 1, Date: tgl(2026, 10, 5), QtyMills: 5_000, UnitCostCents: rp(12_000), MovementType: MovementIn},
		{ID: 3, ItemID: 1, Date: tgl(2026, 10, 10), QtyMills: 2_000, UnitCostCents: rp(8_000), MovementType: MovementIn},
		{ID: 4, ItemID: 1, Date: tgl(2026, 10, 15), QtyMills: 8_000, MovementType: MovementOut},
	}
	calc, err := BuildInventory(item, moves)
	if err != nil {
		t.Fatalf("BuildInventory: %v", err)
	}
	if len(calc.Movements) != 4 {
		t.Fatalf("jumlah mutasi %d (mau 4)", len(calc.Movements))
	}

	// Pembelian 1: 10 @ 10.000 -> avg 10.000,00.
	m1 := calc.Movements[0]
	if m1.AppliedCostCents != rp(10_000) || m1.BalanceAvgCents != rp(10_000) || m1.BalanceQtyMills != 10_000 {
		t.Errorf("pembelian 1: harga %d, avg %d, saldo %d", m1.AppliedCostCents, m1.BalanceAvgCents, m1.BalanceQtyMills)
	}
	// Pembelian 2: (10x10.000 + 5x12.000)/15 = 10.666,67 (dibulatkan ke sen).
	m2 := calc.Movements[1]
	if m2.BalanceAvgCents != rp(10_666)+67 || m2.BalanceQtyMills != 15_000 {
		t.Errorf("pembelian 2: avg %d (mau %d), saldo %d", m2.BalanceAvgCents, rp(10_666)+67, m2.BalanceQtyMills)
	}
	// Pembelian 3: (15x10.666,67 + 2x8.000)/17 = 10.352,94.
	m3 := calc.Movements[2]
	if m3.BalanceAvgCents != rp(10_352)+94 || m3.BalanceQtyMills != 17_000 {
		t.Errorf("pembelian 3: avg %d (mau %d), saldo %d", m3.BalanceAvgCents, rp(10_352)+94, m3.BalanceQtyMills)
	}
	// Penjualan 8: COGS = 8 x 10.352,94 = 82.823,52; avg tidak berubah.
	m4 := calc.Movements[3]
	if m4.AppliedCostCents != rp(10_352)+94 {
		t.Errorf("penjualan: harga pokok per satuan %d (mau %d)", m4.AppliedCostCents, rp(10_352)+94)
	}
	if m4.ValueCents != rp(82_823)+52 {
		t.Errorf("penjualan: COGS %d (mau %d)", m4.ValueCents, rp(82_823)+52)
	}
	if m4.BalanceQtyMills != 9_000 || m4.BalanceAvgCents != rp(10_352)+94 {
		t.Errorf("penjualan: saldo %d, avg %d", m4.BalanceQtyMills, m4.BalanceAvgCents)
	}

	// Baris daftar: saldo 9 x 10.352,94 = 93.176,46.
	if len(calc.Rows) != 1 {
		t.Fatalf("jumlah baris item %d (mau 1)", len(calc.Rows))
	}
	row := calc.Rows[0]
	if row.QtyMills != 9_000 || row.AvgCents != rp(10_352)+94 || row.ValueCents != rp(93_176)+46 {
		t.Errorf("baris item: qty %d, avg %d, nilai %d (mau 9000, %d, %d)",
			row.QtyMills, row.AvgCents, row.ValueCents, rp(10_352)+94, rp(93_176)+46)
	}
	if calc.TotalCents != row.ValueCents {
		t.Errorf("jumlah nilai %d (mau %d)", calc.TotalCents, row.ValueCents)
	}
}

// TestSaldoKurang: mutasi keluar melebihi saldo pada titik itu ditolak.
func TestSaldoKurang(t *testing.T) {
	item := []InventoryItem{{ID: 1, CompanyID: 1, Name: "Kertas A4"}}
	moves := []InventoryMovement{
		{ID: 1, ItemID: 1, Date: tgl(2026, 10, 1), QtyMills: 5_000, UnitCostCents: rp(10_000), MovementType: MovementIn},
		{ID: 2, ItemID: 1, Date: tgl(2026, 10, 2), QtyMills: 6_000, MovementType: MovementOut},
	}
	_, err := BuildInventory(item, moves)
	if !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("harus ErrInsufficientStock, dapat %v", err)
	}
	// Tepat sama dengan saldo: boleh.
	moves[1].QtyMills = 5_000
	if _, err := BuildInventory(item, moves); err != nil {
		t.Fatalf("keluar pas dengan saldo harusnya boleh: %v", err)
	}
}

// TestMutasiTanggalMundur: mutasi yang dimasukkan dengan tanggal mundur tetap
// diproses pada posisi tanggalnya, jadi harga rata-rata mengikuti urutan waktu.
func TestMutasiTanggalMundur(t *testing.T) {
	item := []InventoryItem{{ID: 1, CompanyID: 1, Name: "Pulpen"}}
	moves := []InventoryMovement{
		{ID: 1, ItemID: 1, Date: tgl(2026, 10, 10), QtyMills: 10_000, UnitCostCents: rp(20_000), MovementType: MovementIn},
		{ID: 2, ItemID: 1, Date: tgl(2026, 10, 1), QtyMills: 10_000, UnitCostCents: rp(10_000), MovementType: MovementIn},
	}
	calc, err := BuildInventory(item, moves)
	if err != nil {
		t.Fatalf("BuildInventory: %v", err)
	}
	// Urut tanggal: 10 @ 10.000 lalu 10 @ 20.000 -> avg 15.000 di akhir;
	// baris pertama menurut urutan proses adalah tanggal 01/10.
	if calc.Movements[0].Date.Day() != 1 || calc.Movements[0].BalanceAvgCents != rp(10_000) {
		t.Errorf("proses pertama harus 01/10 dengan avg 10.000, dapat %+v", calc.Movements[0])
	}
	if calc.Rows[0].AvgCents != rp(15_000) {
		t.Errorf("avg akhir %d (mau %d)", calc.Rows[0].AvgCents, rp(15_000))
	}
}

// TestJumlahPecahan: jumlah dengan desimal (milli-unit) dihitung benar.
func TestJumlahPecahan(t *testing.T) {
	item := []InventoryItem{{ID: 1, CompanyID: 1, Name: "Tepung", Unit: "kg"}}
	moves := []InventoryMovement{
		{ID: 1, ItemID: 1, Date: tgl(2026, 10, 1), QtyMills: 1_500, UnitCostCents: rp(10_000), MovementType: MovementIn},
		{ID: 2, ItemID: 1, Date: tgl(2026, 10, 2), QtyMills: 500, MovementType: MovementOut},
	}
	calc, err := BuildInventory(item, moves)
	if err != nil {
		t.Fatalf("BuildInventory: %v", err)
	}
	m2 := calc.Movements[1]
	if m2.ValueCents != rp(5_000) {
		t.Errorf("COGS 0,5 kg x 10.000 = %d (mau %d)", m2.ValueCents, rp(5_000))
	}
	row := calc.Rows[0]
	if row.QtyMills != 1_000 || row.ValueCents != rp(10_000) || row.AvgCents != rp(10_000) {
		t.Errorf("saldo: qty %d (1000), nilai %d (%d), avg %d (%d)",
			row.QtyMills, row.ValueCents, rp(10_000), row.AvgCents, rp(10_000))
	}
}

// TestParseFormatQty: pembulatan teks jumlah dan pembacaan input.
func TestParseFormatQty(t *testing.T) {
	for _, tt := range []struct {
		text  string
		mills int64
	}{
		{"8", 8_000},
		{"2,5", 2_500},
		{"2.5", 2_500},
		{"0,125", 125},
		{"1.000,5", 1_000_500},
		{"", 0},
	} {
		got, err := ParseQty(tt.text)
		if err != nil || got != tt.mills {
			t.Errorf("ParseQty(%q) = %d, %v (mau %d)", tt.text, got, err, tt.mills)
		}
	}
	for _, tt := range []struct {
		text string
		err  bool
	}{
		{"-1", true},
		{"1,2345", true},
		{"1.2.3", true},
		{"abc", true},
	} {
		if _, err := ParseQty(tt.text); (err != nil) != tt.err {
			t.Errorf("ParseQty(%q) err = %v, mau error %v", tt.text, err, tt.err)
		}
	}
	for _, tt := range []struct {
		mills int64
		text  string
	}{
		{8_000, "8"},
		{2_500, "2,5"},
		{125, "0,125"},
		{1_500_000, "1.500"},
	} {
		if got := FormatQty(tt.mills); got != tt.text {
			t.Errorf("FormatQty(%d) = %q (mau %q)", tt.mills, got, tt.text)
		}
	}
}

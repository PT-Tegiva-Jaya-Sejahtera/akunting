package models

import (
	"testing"
	"time"
)

func tglUTC(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// itemP membuat item tagihan (positif) dengan jatuh tempo tertentu.
func itemP(journalID int64, partyID int64, name string, journalDate, dueDate time.Time, cets int64) ReceivableItem {
	return ReceivableItem{
		JournalID: journalID, Reference: "INV", PartyID: partyID, PartyName: name,
		JournalDate: journalDate, DueDate: dueDate, HasDueDate: true, AmountCents: cets,
	}
}

// itemBayar membuat item pembayaran (negatif) tanpa jatuh tempo.
func itemBayar(journalID int64, partyID int64, name string, journalDate time.Time, cets int64) ReceivableItem {
	return ReceivableItem{
		JournalID: journalID, Reference: "PAY", PartyID: partyID, PartyName: name,
		JournalDate: journalDate, AmountCents: -cets,
	}
}

func rowOf(t *testing.T, aging ReceivableAging, partyID int64) ReceivableRow {
	t.Helper()
	for _, r := range aging.Rows {
		if r.PartyID == partyID {
			return r
		}
	}
	t.Fatalf("baris pihak %d tidak ada: %+v", partyID, aging.Rows)
	return ReceivableRow{}
}

// TestAgingBucketBatas memeriksa batas kolom aging per tanggal laporan.
func TestAgingBucketBatas(t *testing.T) {
	asOf := tglUTC(2026, 10, 15)
	kasus := []struct {
		due  time.Time
		want int
	}{
		{tglUTC(2026, 11, 30), 0}, // belum jatuh tempo
		{tglUTC(2026, 10, 15), 1}, // jatuh tempo hari ini: 0 hari
		{tglUTC(2026, 9, 15), 1},  // tepat 30 hari
		{tglUTC(2026, 9, 14), 2},  // 31 hari
		{tglUTC(2026, 8, 16), 2},  // tepat 60 hari
		{tglUTC(2026, 8, 15), 3},  // 61 hari
		{tglUTC(2026, 7, 17), 3},  // tepat 90 hari -> kolom 61-90
		{tglUTC(2026, 7, 16), 4},  // 91 hari -> kolom 90+
	}
	for _, k := range kasus {
		if got := agingBucket(asOf, k.due); got != k.want {
			t.Errorf("agingBucket(%v) = %d, mau %d", k.due, got, k.want)
		}
	}
}

// TestAgingFIFOPembayaran memeriksa pembayaran menutup invoice paling tua
// lebih dulu sehingga sisa yang menua adalah invoice lama.
func TestAgingFIFOPembayaran(t *testing.T) {
	asOf := tglUTC(2026, 10, 15)
	items := []ReceivableItem{
		itemP(1, 7, "CV Andalas", tglUTC(2026, 8, 1), tglUTC(2026, 8, 31), rp(5_000_000)),   // 45 hari -> 31-60
		itemP(2, 7, "CV Andalas", tglUTC(2026, 10, 1), tglUTC(2026, 10, 10), rp(2_000_000)), // 5 hari -> 0-30
		itemBayar(3, 7, "CV Andalas", tglUTC(2026, 10, 12), rp(1_000_000)),
	}
	aging := BuildReceivableAging(items, asOf)
	r := rowOf(t, aging, 7)
	if r.Days3160 != rp(4_000_000) || r.Days030 != rp(2_000_000) {
		t.Fatalf("FIFO salah: 31-60 = %d (mau 400.000.000 sen), 0-30 = %d (mau 200.000.000 sen)",
			r.Days3160, r.Days030)
	}
	if r.Total != rp(6_000_000) {
		t.Fatalf("total = %d, mau %d", r.Total, rp(6_000_000))
	}
}

// TestAgingPembayaranLebihBesar memeriksa pembayaran yang melunasi satu
// invoice lalu mengurangi invoice berikutnya.
func TestAgingPembayaranLebihBesar(t *testing.T) {
	asOf := tglUTC(2026, 10, 15)
	items := []ReceivableItem{
		itemP(1, 7, "CV Andalas", tglUTC(2026, 5, 1), tglUTC(2026, 5, 31), rp(3_000_000)), // lunas
		itemP(2, 7, "CV Andalas", tglUTC(2026, 6, 1), tglUTC(2026, 6, 30), rp(2_000_000)), // tersisa 500 rb
		itemBayar(3, 7, "CV Andalas", tglUTC(2026, 7, 1), rp(4_500_000)),
	}
	aging := BuildReceivableAging(items, asOf)
	r := rowOf(t, aging, 7)
	// Sisa invoice 2 = 500 rb, jatuh tempo 30/6 -> 106 hari -> kolom 90+.
	if r.Days90Plus != rp(500_000) {
		t.Fatalf("sisa invoice kedua harus di kolom 90+, dapat %d", r.Days90Plus)
	}
	if r.Total != rp(500_000) {
		t.Fatalf("total = %d, mau %d", r.Total, rp(500_000))
	}
}

// TestAgingPelunasanLebih memeriksa pembayaran melebihi seluruh invoice:
// sisanya dicatat negatif di kolom belum jatuh tempo supaya jumlah baris
// tetap sama dengan saldo piutang di buku besar.
func TestAgingPelunasanLebih(t *testing.T) {
	asOf := tglUTC(2026, 10, 15)
	items := []ReceivableItem{
		itemP(1, 7, "CV Andalas", tglUTC(2026, 5, 1), tglUTC(2026, 5, 31), rp(3_000_000)),
		itemP(2, 7, "CV Andalas", tglUTC(2026, 6, 1), tglUTC(2026, 6, 30), rp(2_000_000)),
		itemBayar(3, 7, "CV Andalas", tglUTC(2026, 7, 1), rp(5_500_000)),
	}
	aging := BuildReceivableAging(items, asOf)
	r := rowOf(t, aging, 7)
	if r.Days90Plus != 0 {
		t.Fatalf("seluruh invoice harus lunas, 90+ = %d", r.Days90Plus)
	}
	if r.NotDue != -rp(500_000) {
		t.Fatalf("pelunasan lebih harus negatif di kolom belum jatuh tempo, dapat %d", r.NotDue)
	}
	if r.Total != -rp(500_000) {
		t.Fatalf("total = %d, mau %d", r.Total, -rp(500_000))
	}
}

// TestAgingTanpaDueDate memeriksa fallback tanggal jurnal sebagai tanggal
// jatuh tempo bila due_date tidak diisi.
func TestAgingTanpaDueDate(t *testing.T) {
	asOf := tglUTC(2026, 10, 15)
	items := []ReceivableItem{{
		JournalID: 1, Reference: "INV", PartyID: 7, PartyName: "CV Andalas",
		JournalDate: tglUTC(2026, 8, 1), AmountCents: rp(1_000_000), // 75 hari -> 61-90
	}}
	aging := BuildReceivableAging(items, asOf)
	r := rowOf(t, aging, 7)
	if r.Days6190 != rp(1_000_000) {
		t.Fatalf("jurnal tanpa due_date harus menua dari tanggal jurnal, 61-90 = %d", r.Days6190)
	}
}

// TestAgingMultiPihak memisahkan saldo antar pelanggan dan menghitung baris
// jumlah; jurnal tanpa pihak diurut paling bawah.
func TestAgingMultiPihak(t *testing.T) {
	asOf := tglUTC(2026, 10, 15)
	items := []ReceivableItem{
		itemP(1, 0, "", tglUTC(2026, 6, 1), tglUTC(2026, 7, 1), rp(700_000)),                // tanpa pihak, 106 hari
		itemP(2, 9, "PT Bahari", tglUTC(2026, 5, 1), tglUTC(2026, 5, 31), rp(3_000_000)),    // 137 hari
		itemP(3, 9, "PT Bahari", tglUTC(2026, 10, 10), tglUTC(2026, 11, 30), rp(1_500_000)), // belum jatuh tempo
		itemP(4, 7, "CV Andalas", tglUTC(2026, 8, 1), tglUTC(2026, 8, 31), rp(5_000_000)),   // 45 hari
	}
	aging := BuildReceivableAging(items, asOf)
	if len(aging.Rows) != 3 {
		t.Fatalf("jumlah baris = %d, mau 3", len(aging.Rows))
	}
	last := aging.Rows[len(aging.Rows)-1]
	if last.PartyID != PartyIDTanpaPihak || last.PartyName != PartyNameTanpaPihak {
		t.Fatalf("baris tanpa pihak harus di paling bawah, dapat %+v", last)
	}
	andalas := rowOf(t, aging, 7)
	bahari := rowOf(t, aging, 9)
	if andalas.Days3160 != rp(5_000_000) || andalas.Total != rp(5_000_000) {
		t.Fatalf("baris Andalas salah: %+v", andalas)
	}
	if bahari.Days90Plus != rp(3_000_000) || bahari.NotDue != rp(1_500_000) || bahari.Total != rp(4_500_000) {
		t.Fatalf("baris Bahari salah: %+v", bahari)
	}
	if aging.Total.Total != rp(10_200_000) {
		t.Fatalf("jumlah total = %d, mau %d", aging.Total.Total, rp(10_200_000))
	}
	if aging.Total.Days90Plus != rp(3_700_000) {
		t.Fatalf("jumlah kolom 90+ = %d, mau %d", aging.Total.Days90Plus, rp(3_700_000))
	}
}

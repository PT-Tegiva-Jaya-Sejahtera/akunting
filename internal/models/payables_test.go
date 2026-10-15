package models

import (
	"testing"
	"time"
)

// itemHutang membuat item tagihan supplier (positif) dengan jatuh tempo tertentu.
func itemHutang(journalID int64, partyID int64, name string, journalDate, dueDate time.Time, cets int64) PayableItem {
	return PayableItem{
		JournalID: journalID, Reference: "BILL", PartyID: partyID, PartyName: name,
		JournalDate: journalDate, DueDate: dueDate, HasDueDate: true, AmountCents: cets,
	}
}

// itemBayarHutang membuat item pembayaran hutang (negatif) tanpa jatuh tempo.
func itemBayarHutang(journalID int64, partyID int64, name string, journalDate time.Time, cets int64) PayableItem {
	return PayableItem{
		JournalID: journalID, Reference: "PAY", PartyID: partyID, PartyName: name,
		JournalDate: journalDate, AmountCents: -cets,
	}
}

func rowHutang(t *testing.T, aging PayableAging, partyID int64) PayableRow {
	t.Helper()
	for _, r := range aging.Rows {
		if r.PartyID == partyID {
			return r
		}
	}
	t.Fatalf("baris pihak %d tidak ada: %+v", partyID, aging.Rows)
	return PayableRow{}
}

// TestPayableAgingFIFO memeriksa pembayaran hutang menutup tagihan paling tua
// lebih dulu sehingga sisa yang menua adalah tagihan lama.
func TestPayableAgingFIFO(t *testing.T) {
	asOf := tglUTC(2026, 10, 15)
	items := []PayableItem{
		itemHutang(1, 8, "PT Sumber Jaya", tglUTC(2026, 8, 1), tglUTC(2026, 8, 31), rp(4_000_000)),   // 45 hari -> 31-60
		itemHutang(2, 8, "PT Sumber Jaya", tglUTC(2026, 10, 1), tglUTC(2026, 10, 10), rp(2_500_000)), // 5 hari -> 0-30
		itemBayarHutang(3, 8, "PT Sumber Jaya", tglUTC(2026, 10, 12), rp(1_000_000)),
	}
	aging := BuildPayableAging(items, asOf)
	r := rowHutang(t, aging, 8)
	if r.Days3160 != rp(3_000_000) || r.Days030 != rp(2_500_000) {
		t.Fatalf("FIFO salah: 31-60 = %d (mau 300.000.000 sen), 0-30 = %d (mau 250.000.000 sen)",
			r.Days3160, r.Days030)
	}
	if r.Total != rp(5_500_000) {
		t.Fatalf("total = %d, mau %d", r.Total, rp(5_500_000))
	}
}

// TestPayableAgingPelunasanLebih memeriksa pembayaran melebihi seluruh
// tagihan: sisanya dicatat negatif di kolom belum jatuh tempo supaya jumlah
// baris tetap sama dengan saldo hutang di buku besar.
func TestPayableAgingPelunasanLebih(t *testing.T) {
	asOf := tglUTC(2026, 10, 15)
	items := []PayableItem{
		itemHutang(1, 8, "PT Sumber Jaya", tglUTC(2026, 5, 1), tglUTC(2026, 5, 31), rp(3_000_000)),
		itemBayarHutang(2, 8, "PT Sumber Jaya", tglUTC(2026, 7, 1), rp(3_500_000)),
	}
	aging := BuildPayableAging(items, asOf)
	r := rowHutang(t, aging, 8)
	if r.NotDue != -rp(500_000) {
		t.Fatalf("bayar lebih harus negatif di kolom belum jatuh tempo, dapat %d", r.NotDue)
	}
	if r.Total != -rp(500_000) {
		t.Fatalf("total = %d, mau %d", r.Total, -rp(500_000))
	}
}

// TestPayableAgingTanpaSupplier memeriksa jurnal hutang tanpa pihak masuk
// baris "Tanpa Supplier" di paling bawah, dan baris jumlah menghitung semua.
func TestPayableAgingTanpaSupplier(t *testing.T) {
	asOf := tglUTC(2026, 10, 15)
	items := []PayableItem{
		itemHutang(1, 0, "", tglUTC(2026, 6, 1), tglUTC(2026, 7, 25), rp(800_000)),              // tanpa pihak, 82 hari -> 61-90
		itemHutang(2, 9, "CV Mitra", tglUTC(2026, 5, 1), tglUTC(2026, 5, 31), rp(3_000_000)),    // 137 hari -> 90+
		itemHutang(3, 9, "CV Mitra", tglUTC(2026, 10, 10), tglUTC(2026, 11, 30), rp(1_500_000)), // belum jatuh tempo
	}
	aging := BuildPayableAging(items, asOf)
	if len(aging.Rows) != 2 {
		t.Fatalf("jumlah baris = %d, mau 2", len(aging.Rows))
	}
	last := aging.Rows[len(aging.Rows)-1]
	if last.PartyID != PartyIDTanpaPihak || last.PartyName != PartyNameTanpaSupplier {
		t.Fatalf("baris tanpa pihak harus di paling bawah dengan nama %q, dapat %+v",
			PartyNameTanpaSupplier, last)
	}
	if last.Days6190 != rp(800_000) {
		t.Fatalf("baris Tanpa Supplier 61-90 = %d, mau %d", last.Days6190, rp(800_000))
	}
	mitra := rowHutang(t, aging, 9)
	if mitra.Days90Plus != rp(3_000_000) || mitra.NotDue != rp(1_500_000) || mitra.Total != rp(4_500_000) {
		t.Fatalf("baris CV Mitra salah: %+v", mitra)
	}
	if aging.Total.Total != rp(5_300_000) || aging.Total.Days6190 != rp(800_000) {
		t.Fatalf("jumlah salah: %+v", aging.Total)
	}
}

// TestPayableAgingArahSaldo memeriksa arah saldo hutang: tagihan supplier
// (kredit) positif, pembayaran (debit) negatif, dan buku besar hutang
// (kredit - debit) sama dengan jumlah subledger.
func TestPayableAgingArahSaldo(t *testing.T) {
	asOf := tglUTC(2026, 10, 15)
	items := []PayableItem{
		itemHutang(1, 8, "PT Sumber Jaya", tglUTC(2026, 8, 1), tglUTC(2026, 8, 31), rp(4_000_000)),
		itemBayarHutang(2, 8, "PT Sumber Jaya", tglUTC(2026, 10, 12), rp(1_000_000)),
	}
	aging := BuildPayableAging(items, asOf)
	if aging.Total.Total != rp(3_000_000) {
		t.Fatalf("saldo hutang = %d, mau %d (kredit - debit)", aging.Total.Total, rp(3_000_000))
	}
}

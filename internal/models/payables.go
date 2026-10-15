package models

import "time"

// Subledger hutang usaha: saldo terbuka per supplier dengan kolom aging
// (umur hutang dihitung dari tanggal jatuh tempo vs tanggal laporan).
// Struktur dan mesin aging dipakai bersama dengan subledger piutang (3a);
// yang berbeda hanya arah saldo (kredit - debit) dan nama baris tanpa pihak.

// Nama baris untuk jurnal hutang yang tidak punya pihak terkait.
const PartyNameTanpaSupplier = "Tanpa Supplier"

// PayableItem adalah satu jurnal yang menyentuh akun hutang usaha.
// AmountCents adalah mutasi neto jurnal pada akun hutang (kredit - debit,
// dalam sen): positif = tagihan supplier, negatif = pembayaran/pengurang.
type PayableItem struct {
	JournalID   int64
	Reference   string
	PartyID     int64
	PartyName   string
	JournalDate time.Time
	DueDate     time.Time
	HasDueDate  bool
	AmountCents int64
}

// PayableRow adalah satu baris saldo hutang per supplier. Kolom aging memuat
// sisa tagihan yang sudah jatuh tempo menurut umurnya; kolom NotDue memuat
// tagihan yang belum jatuh tempo plus sisa pelunasan lebih, sama seperti
// subledger piutang.
type PayableRow struct {
	PartyID    int64
	PartyName  string
	NotDue     int64
	Days030    int64
	Days3160   int64
	Days6190   int64
	Days90Plus int64
	Total      int64
}

// PayableAging adalah hasil pengelompokan aging: baris per supplier
// (diurut nama, baris tanpa pihak di paling bawah) dan baris jumlah.
type PayableAging struct {
	Rows  []PayableRow
	Total PayableRow
}

// PayablesView adalah data halaman subledger hutang usaha.
type PayablesView struct {
	AsOfISO       string
	AsOfLabel     string
	Rows          []PayableRow
	Total         PayableRow
	RoundingUnit  string
	RoundingLabel string
}

// Money menampilkan nilai sen memakai satuan pembulatan entitas.
func (v PayablesView) Money(cents int64) string {
	return FormatRounded(cents, v.RoundingUnit)
}

// BuildPayableAging mengelompokkan item per supplier lalu membaginya ke
// kolom aging lewat mesin yang sama dengan piutang: pembayaran dialokasikan
// FIFO (tagihan paling tua dilunasi lebih dulu) dan pelunasan lebih dicatat
// negatif di kolom NotDue agar jumlah baris tetap sama dengan saldo hutang
// usaha di buku besar.
func BuildPayableAging(items []PayableItem, asOf time.Time) PayableAging {
	ri := make([]ReceivableItem, len(items))
	for i, it := range items {
		ri[i] = ReceivableItem{
			JournalID:   it.JournalID,
			Reference:   it.Reference,
			PartyID:     it.PartyID,
			PartyName:   it.PartyName,
			JournalDate: it.JournalDate,
			DueDate:     it.DueDate,
			HasDueDate:  it.HasDueDate,
			AmountCents: it.AmountCents,
		}
	}
	ra := buildAging(ri, asOf, PartyNameTanpaSupplier)

	out := PayableAging{Rows: make([]PayableRow, 0, len(ra.Rows))}
	for _, r := range ra.Rows {
		out.Rows = append(out.Rows, payableRow(r))
	}
	out.Total = payableRow(ra.Total)
	return out
}

// payableRow menyalin satu baris hasil mesin aging bersama.
func payableRow(r ReceivableRow) PayableRow {
	return PayableRow{
		PartyID:    r.PartyID,
		PartyName:  r.PartyName,
		NotDue:     r.NotDue,
		Days030:    r.Days030,
		Days3160:   r.Days3160,
		Days6190:   r.Days6190,
		Days90Plus: r.Days90Plus,
		Total:      r.Total,
	}
}

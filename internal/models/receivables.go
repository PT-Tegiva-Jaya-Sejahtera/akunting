package models

import (
	"sort"
	"time"
)

// Subledger piutang usaha: saldo terbuka per pelanggan dengan kolom aging
// (umur piutang dihitung dari tanggal jatuh tempo vs tanggal laporan).
// Seluruh perhitungan murni di sini supaya bisa diuji tanpa HTTP/database.

// PartyIDTanpaPihak dipakai jurnal yang menyentuh akun piutang tetapi tidak
// punya pihak terkait (mis. jurnal saldo awal). Tetap ditampilkan supaya
// jumlah subledger selalu sama dengan saldo akun piutang di buku besar.
const PartyIDTanpaPihak = 0

// Nama baris untuk jurnal tanpa pihak.
const PartyNameTanpaPihak = "Tanpa Pelanggan"

// ReceivableItem adalah satu jurnal yang menyentuh akun piutang usaha.
// AmountCents adalah mutasi neto jurnal pada akun piutang (debit - kredit,
// dalam sen): positif = tagihan/invoice, negatif = pembayaran/pengurang.
type ReceivableItem struct {
	JournalID   int64
	Reference   string
	PartyID     int64
	PartyName   string
	JournalDate time.Time
	DueDate     time.Time
	HasDueDate  bool
	AmountCents int64
}

// ReceivableRow adalah satu baris saldo piutang per pelanggan. Kolom aging
// memuat sisa tagihan yang sudah jatuh tempo menurut umurnya; kolom NotDue
// memuat tagihan yang belum jatuh tempo (dipisah supaya tunggakan tidak
// tertukar dengan piutang yang masih wajar) plus sisa pelunasan lebih.
type ReceivableRow struct {
	PartyID    int64
	PartyName  string
	NotDue     int64
	Days030    int64
	Days3160   int64
	Days6190   int64
	Days90Plus int64
	Total      int64
}

// ReceivableAging adalah hasil pengelompokan aging: baris per pelanggan
// (diurut nama, baris tanpa pihak di paling bawah) dan baris jumlah.
type ReceivableAging struct {
	Rows  []ReceivableRow
	Total ReceivableRow
}

// ReceivablesView adalah data halaman subledger piutang usaha.
type ReceivablesView struct {
	AsOfISO       string
	AsOfLabel     string
	Rows          []ReceivableRow
	Total         ReceivableRow
	RoundingUnit  string
	RoundingLabel string
}

// Money menampilkan nilai sen memakai satuan pembulatan entitas.
func (v ReceivablesView) Money(cents int64) string {
	return FormatRounded(cents, v.RoundingUnit)
}

// openItem adalah sisa satu invoice yang belum lunas, dipakai internal
// alokasi FIFO pembayaran.
type openItem struct {
	due  time.Time
	cets int64
}

// BuildReceivableAging mengelompokkan item per pelanggan lalu membaginya ke
// kolom aging. Pembayaran (item negatif) dialokasikan FIFO: invoice paling
// tua dilunasi lebih dulu, sehingga sisa tagihan yang menua adalah invoice
// yang benar-benar belum tertutup. Sisa pelunasan lebih dari seluruh invoice
// dicatat negatif di kolom NotDue agar jumlah baris tetap sama dengan saldo
// piutang di buku besar.
func BuildReceivableAging(items []ReceivableItem, asOf time.Time) ReceivableAging {
	asOf = dateOnly(asOf)

	type group struct {
		partyID   int64
		partyName string
		items     []ReceivableItem
	}
	order := make([]int64, 0)
	groups := map[int64]*group{}
	for _, it := range items {
		g, ok := groups[it.PartyID]
		if !ok {
			name := it.PartyName
			if it.PartyID == PartyIDTanpaPihak || name == "" {
				name = PartyNameTanpaPihak
			}
			g = &group{partyID: it.PartyID, partyName: name}
			groups[it.PartyID] = g
			order = append(order, it.PartyID)
		}
		g.items = append(g.items, it)
	}

	rows := make([]ReceivableRow, 0, len(order))
	for _, pid := range order {
		g := groups[pid]
		sort.SliceStable(g.items, func(i, j int) bool {
			if !g.items[i].JournalDate.Equal(g.items[j].JournalDate) {
				return g.items[i].JournalDate.Before(g.items[j].JournalDate)
			}
			return g.items[i].JournalID < g.items[j].JournalID
		})

		row := ReceivableRow{PartyID: g.partyID, PartyName: g.partyName}
		var open []openItem
		var sisa int64 // pelunasan lebih yang belum teralokasi
		for _, it := range g.items {
			amt := it.AmountCents
			if amt == 0 {
				continue
			}
			if amt > 0 {
				due := dateOnly(it.JournalDate)
				if it.HasDueDate {
					due = dateOnly(it.DueDate)
				}
				open = append(open, openItem{due: due, cets: amt})
				continue
			}
			// Pembayaran: tutup invoice paling tua lebih dulu.
			bayar := -amt
			for bayar > 0 && len(open) > 0 {
				if open[0].cets <= bayar {
					bayar -= open[0].cets
					open = open[1:]
				} else {
					open[0].cets -= bayar
					bayar = 0
				}
			}
			sisa -= bayar
		}

		for _, oi := range open {
			row.addBucket(agingBucket(asOf, oi.due), oi.cets)
		}
		if sisa != 0 {
			row.NotDue += sisa
		}
		row.Total = row.NotDue + row.Days030 + row.Days3160 + row.Days6190 + row.Days90Plus
		rows = append(rows, row)
	}

	sort.SliceStable(rows, func(i, j int) bool {
		ii, jj := rows[i], rows[j]
		if (ii.PartyID == PartyIDTanpaPihak) != (jj.PartyID == PartyIDTanpaPihak) {
			return jj.PartyID == PartyIDTanpaPihak
		}
		if ii.PartyName != jj.PartyName {
			return ii.PartyName < jj.PartyName
		}
		return ii.PartyID < jj.PartyID
	})

	total := ReceivableRow{PartyName: "Jumlah"}
	for _, r := range rows {
		total.NotDue += r.NotDue
		total.Days030 += r.Days030
		total.Days3160 += r.Days3160
		total.Days6190 += r.Days6190
		total.Days90Plus += r.Days90Plus
		total.Total += r.Total
	}
	return ReceivableAging{Rows: rows, Total: total}
}

// addBucket menambahkan nilai ke kolom aging sesuai indeks bucket.
func (r *ReceivableRow) addBucket(bucket int, cets int64) {
	switch bucket {
	case 0:
		r.NotDue += cets
	case 1:
		r.Days030 += cets
	case 2:
		r.Days3160 += cets
	case 3:
		r.Days6190 += cets
	default:
		r.Days90Plus += cets
	}
}

// agingBucket menghitung umur piutang dalam hari lewat jatuh tempo per
// tanggal laporan, lalu memilih kolom: 0 = belum jatuh tempo, 1 = 0-30 hari,
// 2 = 31-60 hari, 3 = 61-90 hari, 4 = lewat 90 hari. Hari ke-90 masuk kolom
// 61-90; kolom 90+ memuat yang lewat dari 90 hari.
func agingBucket(asOf, due time.Time) int {
	days := int(asOf.Sub(due) / (24 * time.Hour))
	switch {
	case days < 0:
		return 0
	case days <= 30:
		return 1
	case days <= 60:
		return 2
	case days <= 90:
		return 3
	default:
		return 4
	}
}

// dateOnly membuang komponen jam supaya selisih hari selalu bulat.
func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

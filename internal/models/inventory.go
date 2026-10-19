package models

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// Persediaan (PSAK 202, Stage 4b): item, mutasi stok, dan biaya rata-rata
// tertimbang (weighted average) sampai tanggal laporan.
//
// Aturan hitung (matematika bilangan bulat; jumlah dalam milli-unit = 1/1000
// satuan, uang dalam sen):
//   - Pembelian (in): harga rata-rata dihitung ulang:
//     avg_baru = (qty_lama x avg_lama + qty_beli x harga_beli) / (qty_lama + qty_beli),
//     dibulatkan setengah ke atas ke sen.
//   - Barang keluar (out): dinilai dengan harga rata-rata saat itu
//     (harga pokok penjualan / COGS = qty x avg), harga rata-rata tidak
//     berubah oleh pengeluaran.
//   - Nilai persediaan = saldo qty x harga rata-rata, jadi selalu konsisten
//     dengan definisi "qty x avg cost" di halaman.
//
// Mutasi diproses berurut tanggal lalu id (urutan yang sama dengan query),
// sehingga mutasi yang dimasukkan dengan tanggal mundur tetap dihitung pada
// posisinya. Jurnal pembelian, penjualan, dan HPP tetap dibuat manual oleh
// user; modul ini hanya menghitung angka rekomendasinya.

// Nama tipe mutasi di database (enum inventory_movement_type).
const (
	MovementIn  = "in"
	MovementOut = "out"
)

// MovementLabel menerjemahkan kode mutasi ke label tampilan.
func MovementLabel(movementType string) string {
	if movementType == MovementOut {
		return "Keluar"
	}
	return "Masuk"
}

// ErrInsufficientStock dipakai saat mutasi keluar melebihi saldo yang ada
// pada titik tersebut dalam urutan mutasi.
var ErrInsufficientStock = errors.New("saldo persediaan tidak mencukupi")

// InventoryItem adalah satu item persediaan terdaftar.
type InventoryItem struct {
	ID        int64
	CompanyID int64
	Name      string
	SKU       string
	Unit      string
}

// InventoryMovement adalah satu mutasi stok. QtyMills adalah jumlah dalam
// milli-unit (qty x 1000, maksimal 3 desimal); UnitCostCents hanya dipakai
// untuk mutasi masuk (harga beli per satuan).
type InventoryMovement struct {
	ID            int64
	CompanyID     int64
	ItemID        int64
	Date          time.Time
	QtyMills      int64
	UnitCostCents int64
	MovementType  string
}

// InventoryRow adalah satu baris daftar persediaan: saldo qty, harga
// rata-rata tertimbang, dan nilai persediaan (qty x avg cost).
type InventoryRow struct {
	InventoryItem
	QtyMills   int64
	AvgCents   int64
	ValueCents int64
}

// MovementRow adalah satu baris riwayat mutasi beserta harga yang
// dipakai (harga beli untuk masuk, harga rata-rata saat itu untuk keluar)
// dan saldo setelah mutasi.
type MovementRow struct {
	InventoryMovement
	ItemName         string
	ItemUnit         string
	DateLabel        string
	TypeLabel        string
	QtyLabel         string
	AppliedCostCents int64 // harga satuan yang dipakai untuk menilai mutasi ini
	ValueCents       int64 // nilai mutasi = qty x AppliedCostCents (untuk keluar = COGS)
	BalanceQtyMills  int64 // saldo qty setelah mutasi ini
	BalanceAvgCents  int64 // harga rata-rata setelah mutasi ini
	BalanceCents     int64 // nilai persediaan setelah mutasi ini
}

// InventoryCalc adalah hasil perhitungan satu perusahaan per tanggal laporan.
type InventoryCalc struct {
	Rows       []InventoryRow
	Movements  []MovementRow
	TotalCents int64
}

// InventoryCOABuku adalah saldo akun persediaan COA menurut buku besar
// (dari jurnal yang dibuat manual user), dipakai sebagai pembanding.
type InventoryCOABuku struct {
	LedgerCents int64
}

// InventoryView adalah data halaman persediaan.
type InventoryView struct {
	AsOfISO    string
	AsOfLabel  string
	Rows       []InventoryRow
	TotalCents int64
	Movements  []MovementRow
	Items      []InventoryItem
	Buku       InventoryCOABuku
	CanManage  bool
	// Rekap stok seluruh barang: kumulatif masuk/keluar, tanggal terakhir
	// masuk/keluar, dan sisa per item, plus kode akun persediaan terkait.
	Rekap       []InventoryRekapRow
	AccountCode string
}

// InventoryRekapRow adalah satu baris rekap persediaan: no urut, kode akun
// persediaan, nama barang, kumulatif masuk dan keluar dengan tanggal
// terakhirnya, serta sisa stok.
type InventoryRekapRow struct {
	No           int
	AccountCode  string
	Name         string
	Unit         string
	QtyInLabel   string
	LastInLabel  string
	QtyOutLabel  string
	LastOutLabel string
	SisaLabel    string
}

// Money menampilkan nilai sen secara penuh (dengan sen) supaya angka
// rata-rata tertimbang dan COGS bisa dibandingkan satuan-sen.
func (v InventoryView) Money(cents int64) string {
	return FormatCents(cents)
}

// BuildInventory menghitung saldo, harga rata-rata tertimbang, nilai
// persediaan, dan riwayat mutasi ber-nilai untuk seluruh item. moves harus
// milik items yang sama (satu perusahaan); urutan dipaksa (tanggal, id).
func BuildInventory(items []InventoryItem, moves []InventoryMovement) (InventoryCalc, error) {
	urut := make([]InventoryMovement, len(moves))
	copy(urut, moves)
	sort.SliceStable(urut, func(i, j int) bool {
		if !urut[i].Date.Equal(urut[j].Date) {
			return urut[i].Date.Before(urut[j].Date)
		}
		return urut[i].ID < urut[j].ID
	})

	itemByID := make(map[int64]InventoryItem, len(items))
	for _, it := range items {
		itemByID[it.ID] = it
	}

	type state struct{ qtyMills, avgCents int64 }
	states := make(map[int64]*state, len(items))

	var calc InventoryCalc
	for _, m := range urut {
		it, ok := itemByID[m.ItemID]
		if !ok {
			return InventoryCalc{}, fmt.Errorf("mutasi %d: item %d tidak dikenal", m.ID, m.ItemID)
		}
		st := states[m.ItemID]
		if st == nil {
			st = &state{}
			states[m.ItemID] = st
		}

		row := MovementRow{
			InventoryMovement: m,
			ItemName:          it.Name,
			ItemUnit:          it.Unit,
			DateLabel:         FormatDateID(m.Date),
			TypeLabel:         MovementLabel(m.MovementType),
			QtyLabel:          FormatQty(m.QtyMills),
		}

		switch m.MovementType {
		case MovementIn:
			// Harga rata-rata dihitung ulang setelah tiap pembelian.
			// Aritmatika dalam milli-cents: qty x avg (sen per satuan).
			newQty := st.qtyMills + m.QtyMills
			newVal := st.qtyMills*st.avgCents + m.QtyMills*m.UnitCostCents
			st.avgCents = divRoundHalfUp(newVal, newQty)
			st.qtyMills = newQty
			row.AppliedCostCents = m.UnitCostCents
			row.ValueCents = divRoundHalfUp(m.QtyMills*m.UnitCostCents, 1000)
		case MovementOut:
			if m.QtyMills > st.qtyMills {
				return InventoryCalc{}, fmt.Errorf("%w: item %q mengeluarkan %s dari saldo %s",
					ErrInsufficientStock, it.Name, FormatQty(m.QtyMills), FormatQty(st.qtyMills))
			}
			// Barang keluar dinilai dengan harga rata-rata saat itu (COGS).
			row.AppliedCostCents = st.avgCents
			row.ValueCents = divRoundHalfUp(m.QtyMills*st.avgCents, 1000)
			st.qtyMills -= m.QtyMills
		default:
			return InventoryCalc{}, fmt.Errorf("mutasi %d: tipe %q tidak dikenal", m.ID, m.MovementType)
		}

		row.BalanceQtyMills = st.qtyMills
		row.BalanceAvgCents = st.avgCents
		row.BalanceCents = divRoundHalfUp(st.qtyMills*st.avgCents, 1000)
		calc.Movements = append(calc.Movements, row)
	}

	// Baris daftar: seluruh item terdaftar, termasuk yang belum punya mutasi.
	for _, it := range items {
		st := states[it.ID]
		r := InventoryRow{InventoryItem: it}
		if st != nil {
			r.QtyMills = st.qtyMills
			r.AvgCents = st.avgCents
			r.ValueCents = divRoundHalfUp(st.qtyMills*st.avgCents, 1000)
		}
		calc.Rows = append(calc.Rows, r)
		calc.TotalCents += r.ValueCents
	}
	return calc, nil
}

// ValidateMovement memeriksa satu calon mutasi terhadap mutasi yang sudah
// ada sebelum dipasang ke database: mengembalikan ErrInsufficientStock bila
// mutasi keluar melebihi saldo pada posisinya dalam urutan tanggal.
func ValidateMovement(items []InventoryItem, moves []InventoryMovement, calon InventoryMovement) error {
	_, err := BuildInventory(items, append(append([]InventoryMovement{}, moves...), calon))
	return err
}

// divRoundHalfUp membagi dua bilangan bulat non-negatif dan membulatkan
// setengah ke atas.
func divRoundHalfUp(v, d int64) int64 {
	if d <= 0 {
		return 0
	}
	return (v + d/2) / d
}

// ParseQty mengubah teks jumlah menjadi milli-unit (qty x 1000). Menerima
// "8", "2,5", "2.5", atau format Indonesia "1.000,5". Maksimal 3 desimal;
// jangan memakai pemisah ribuan bila tidak ada desimal ("1.000" = satu).
// String kosong dianggap 0. Negatif ditolak.
func ParseQty(s string) (int64, error) {
	s = strings.TrimSpace(strings.ReplaceAll(s, " ", ""))
	if s == "" {
		return 0, nil
	}
	if strings.ContainsAny(s, "-") {
		return 0, fmt.Errorf("jumlah negatif tidak diizinkan")
	}

	if strings.Contains(s, ",") {
		// Format Indonesia: titik = ribuan, koma = desimal.
		s = strings.ReplaceAll(s, ".", "")
		s = strings.ReplaceAll(s, ",", ".")
	}

	parts := strings.Split(s, ".")
	if len(parts) > 2 {
		return 0, fmt.Errorf("terlalu banyak tanda desimal")
	}
	intPart := parts[0]
	fracPart := ""
	if len(parts) == 2 {
		fracPart = parts[1]
	}
	if intPart == "" {
		intPart = "0"
	}
	if len(fracPart) > 3 {
		return 0, fmt.Errorf("maksimal 3 angka desimal")
	}
	if !allDigits(intPart) || (fracPart != "" && !allDigits(fracPart)) {
		return 0, fmt.Errorf("hanya angka yang diizinkan")
	}
	if len(intPart) > 15 {
		return 0, fmt.Errorf("jumlah terlalu besar")
	}
	for len(fracPart) < 3 {
		fracPart += "0"
	}
	bulanan, ok := new(big.Int).SetString(intPart, 10)
	if !ok {
		return 0, fmt.Errorf("jumlah tidak valid")
	}
	pecahan, ok := new(big.Int).SetString(fracPart, 10)
	if !ok {
		return 0, fmt.Errorf("jumlah tidak valid")
	}
	bulanan.Mul(bulanan, big.NewInt(1000))
	bulanan.Add(bulanan, pecahan)
	if !bulanan.IsInt64() {
		return 0, fmt.Errorf("jumlah terlalu besar")
	}
	return bulanan.Int64(), nil
}

// FormatQty menampilkan milli-unit menjadi teks jumlah gaya Indonesia,
// tanpa nol belakang yang tidak perlu: 8000 -> "8", 2500 -> "2,5",
// 1250 -> "1,25", 1500000 -> "1.500".
func FormatQty(mills int64) string {
	sign := ""
	if mills < 0 {
		sign = "-"
		mills = -mills
	}
	whole := mills / 1000
	frac := mills % 1000

	wholeStr := fmt.Sprintf("%d", whole)
	var b strings.Builder
	for i, r := range wholeStr {
		if i > 0 && (len(wholeStr)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	out := sign + b.String()
	if frac != 0 {
		s := fmt.Sprintf("%03d", frac)
		s = strings.TrimRight(s, "0")
		out += "," + s
	}
	return out
}

// NumericToMills mengubah pgtype.Numeric jumlah dari database menjadi
// milli-unit. Rumus: nilai = Int x 10^Exp, jadi milli = Int x 10^(Exp+3).
func NumericToMills(n pgtype.Numeric) int64 {
	if !n.Valid || n.Int == nil {
		return 0
	}
	v := new(big.Int).Set(n.Int)
	exp := int(n.Exp) + 3
	switch {
	case exp > 0:
		v.Mul(v, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(exp)), nil))
	case exp < 0:
		v.Div(v, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-exp)), nil))
	}
	return v.Int64()
}

// MillsToNumeric membungkus milli-unit menjadi pgtype.Numeric untuk sqlc
// (jumlah dengan sampai 3 desimal).
func MillsToNumeric(mills int64) (pgtype.Numeric, error) {
	var n pgtype.Numeric
	s := fmt.Sprintf("%d.%03d", mills/1000, mod1000(mills))
	if mills < 0 {
		s = fmt.Sprintf("-%d.%03d", (-mills)/1000, mod1000(-mills))
	}
	err := n.Scan(s)
	return n, err
}

func mod1000(v int64) int64 {
	m := v % 1000
	if m < 0 {
		m += 1000
	}
	return m
}

// Pajak penghasilan kini (PSAK 212, sebelumnya PSAK 46). Fungsi di file ini
// murni (menerima angka dalam satuan sen dan tarif pgtype.Numeric) supaya bisa
// diuji tanpa database. Pajak tangguhan (beda temporer) belum dihitung.
//
// Aturan hitung:
//   - Beban pajak kini = laba sebelum pajak x tarif pajak perusahaan.
//   - Rugi (laba sebelum pajak <= 0) berarti tidak ada pajak kini terutang,
//     beban pajak = 0 (rugi fiskal tidak dibawa ke periode ini).
//   - Pembulatan setengah ke atas sampai satuan sen terdekat.
package models

import (
	"math"
	"math/big"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

// TaxAccrual adalah hasil hitung pajak penghasilan kini atas satu rentang
// waktu, dalam satuan sen.
type TaxAccrual struct {
	ProfitBeforeTaxCents int64 // laba sebelum pajak (beban pajak dikecualikan)
	BookedTaxCents       int64 // beban pajak yang sudah dibukukan lewat jurnal
	CurrentTaxCents      int64 // beban pajak kini = max(0, laba) x tarif
}

// AccrueTax menghitung pajak penghasilan kini dari daftar total akun satu
// rentang waktu. Akun yang dipetakan ke beban pajak (PsakBebanPajak) dihitung
// terpisah sebagai beban yang sudah dibukukan, supaya laporan bisa
// merekonsiliasi angka jurnal dengan estimasi tarif tanpa menggandakan
// hitungan.
func AccrueTax(totals []AccountTotal, rate pgtype.Numeric) TaxAccrual {
	var a TaxAccrual
	for _, t := range totals {
		if t.PsakLine == PsakBebanPajak {
			a.BookedTaxCents += t.DebitCents - t.CreditCents
			continue
		}
		switch t.AccountType {
		case TypeRevenue, TypeExpense:
			a.ProfitBeforeTaxCents += t.CreditCents - t.DebitCents
		}
	}
	a.CurrentTaxCents = CurrentTax(a.ProfitBeforeTaxCents, rate)
	return a
}

// ApplyCurrentTax memasukkan beban pajak kini yang belum dibukukan lewat jurnal
// ke Laporan Perubahan Ekuitas: laba (rugi) periode berjalan dan laba belum
// ditutup berkurang sebesar beban pajak kini yang belum dibukukan
// (CurrentTax - BookedTax), sehingga laba periode sama dengan laba bersih
// Laporan Laba Rugi dan saldo akhir ekuitas menutup ke jumlah ekuitas Neraca.
func (s *EquityStatement) ApplyCurrentTax(a TaxAccrual) {
	delta := a.BookedTaxCents - a.CurrentTaxCents
	s.UnclosedProfit += delta
	s.UnclosedClosing += delta
	s.TotalClosing += delta
}

// ApplyCurrentTax memasukkan beban pajak kini yang belum dibukukan ke Laporan
// Arus Kas: laba periode disamakan dengan laba bersih Laporan Laba Rugi, lalu
// beban pajak yang belum dibayar ditambahkan kembali sebagai perubahan utang
// pajak (akruan non-kas), sehingga kenaikan kas tidak berubah dan baris tetap
// menutup ke subtotal.
func (c *CashFlow) ApplyCurrentTax(a TaxAccrual) {
	delta := a.BookedTaxCents - a.CurrentTaxCents
	if delta == 0 {
		return
	}
	c.NetIncome += delta
	c.WorkingCapital = append(c.WorkingCapital, ReportLine{
		Name:        "Perubahan utang pajak penghasilan (akruan)",
		AmountCents: -delta,
	})
}

// CurrentTax menghitung beban pajak kini: laba sebelum pajak dikalikan tarif
// pajak perusahaan, dibulatkan setengah ke atas ke satuan sen. Laba nol atau
// rugi menghasilkan 0.
func CurrentTax(profitBeforeTaxCents int64, rate pgtype.Numeric) int64 {
	if profitBeforeTaxCents <= 0 || !rate.Valid || rate.Int == nil {
		return 0
	}
	// Nilai tarif = Int x 10^Exp, jadi pajak = laba x Int x 10^Exp.
	num := new(big.Int).Mul(big.NewInt(profitBeforeTaxCents), rate.Int)
	if rate.Exp >= 0 {
		num.Mul(num, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(rate.Exp)), nil))
		if !num.IsInt64() {
			return math.MaxInt64
		}
		return num.Int64()
	}
	den := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-rate.Exp)), nil)
	q, r := new(big.Int).QuoRem(num, den, new(big.Int))
	// Pembulatan setengah ke atas: sisa >= separuh penyebut dibulatkan naik.
	if new(big.Int).Lsh(r, 1).Cmp(den) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if !q.IsInt64() {
		return math.MaxInt64
	}
	return q.Int64()
}

// RatePercent memformat tarif pajak sebagai persen gaya Indonesia: tarif 0,22
// menjadi "22%", tarif 0,225 menjadi "22,5%".
func RatePercent(rate pgtype.Numeric) string {
	if !rate.Valid || rate.Int == nil {
		return "0%"
	}
	sign := ""
	abs := new(big.Int).Set(rate.Int)
	if abs.Sign() < 0 {
		sign = "-"
		abs.Neg(abs)
	}
	// Persen = Int x 10^(Exp+2); susun digitnya sebagai angka desimal.
	k := int(rate.Exp) + 2
	digits := abs.String()
	frac := ""
	switch {
	case k >= 0:
		digits += strings.Repeat("0", k)
	default:
		if len(digits) <= -k {
			digits = strings.Repeat("0", -k-len(digits)+1) + digits
		}
		frac = digits[len(digits)+k:]
		digits = digits[:len(digits)+k]
	}
	digits = strings.TrimLeft(digits, "0")
	if digits == "" {
		digits = "0"
	}
	frac = strings.TrimRight(frac, "0")
	if frac != "" {
		return sign + digits + "," + frac + "%"
	}
	return sign + digits + "%"
}

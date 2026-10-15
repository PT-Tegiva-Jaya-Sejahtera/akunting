package models

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func rate(s string) pgtype.Numeric {
	var n pgtype.Numeric
	if err := n.Scan(s); err != nil {
		panic(err)
	}
	return n
}

func TestCurrentTax(t *testing.T) {
	cases := []struct {
		name   string
		profit int64
		tarif  string
		want   int64
	}{
		{"laba positif x 22%", 10_000_000_00, "0.22", 2_200_000_00},
		{"laba kecil tepat", 1_00_00, "0.22", 22_00},
		{"sisa tepat setengah sen dibulatkan naik", 25, "0.22", 6},
		{"sisa di bawah setengah sen dibulatkan turun", 24, "0.22", 5},
		{"rugi tidak kena pajak", -3_000_000_00, "0.22", 0},
		{"laba nol", 0, "0.22", 0},
		{"tarif nol", 10_000_000_00, "0", 0},
		{"tarif 11%", 10_000_000_00, "0.11", 1_100_000_00},
		{"tarif 22,5%", 10_000_000_00, "0.225", 2_250_000_00},
		{"tarif penuh", 1_000_000_00, "1", 1_000_000_00},
	}
	for _, c := range cases {
		got := CurrentTax(c.profit, rate(c.tarif))
		if got != c.want {
			t.Errorf("%s: CurrentTax(%d, %s) = %d, mau %d", c.name, c.profit, c.tarif, got, c.want)
		}
	}
	if got := CurrentTax(100, pgtype.Numeric{}); got != 0 {
		t.Errorf("tarif tidak valid harus 0, dapat %d", got)
	}
}

func TestRatePercent(t *testing.T) {
	cases := []struct {
		tarif string
		want  string
	}{
		{"0.22", "22%"},
		{"0.225", "22,5%"},
		{"0.11", "11%"},
		{"0", "0%"},
		{"1", "100%"},
		{"0.0225", "2,25%"},
	}
	for _, c := range cases {
		if got := RatePercent(rate(c.tarif)); got != c.want {
			t.Errorf("RatePercent(%s) = %q, mau %q", c.tarif, got, c.want)
		}
	}
	if got := RatePercent(pgtype.Numeric{}); got != "0%" {
		t.Errorf("tarif tidak valid = %q, mau %q", got, "0%")
	}
}

func TestAccrueTax(t *testing.T) {
	totals := []AccountTotal{
		{Code: "4-1000", Name: "Pendapatan Sewa", AccountType: TypeRevenue, CreditCents: 100_000_00},
		{Code: "5-1000", Name: "Beban Gaji", AccountType: TypeExpense, DebitCents: 30_000_00},
		{Code: "5-4000", Name: "Beban Bunga", AccountType: TypeExpense, DebitCents: 20_000_00},
		{Code: "5-3000", Name: "Beban Pajak Penghasilan", AccountType: TypeExpense,
			PsakLine: PsakBebanPajak, DebitCents: 10_000_00},
	}
	a := AccrueTax(totals, rate("0.22"))
	// Laba sebelum pajak tidak memasukkan beban pajak yang sudah dibukukan.
	if a.ProfitBeforeTaxCents != 50_000_00 {
		t.Fatalf("laba sebelum pajak = %d, mau 5000000", a.ProfitBeforeTaxCents)
	}
	if a.BookedTaxCents != 10_000_00 {
		t.Fatalf("beban pajak terbukukan = %d, mau 1000000", a.BookedTaxCents)
	}
	if a.CurrentTaxCents != 11_000_00 {
		t.Fatalf("beban pajak kini = %d, mau 1100000 (5.000.000 x 22%%)", a.CurrentTaxCents)
	}

	// Rugi: beban pajak kini 0, bukan negatif.
	rugi := AccrueTax([]AccountTotal{
		{AccountType: TypeRevenue, CreditCents: 5_000_00},
		{AccountType: TypeExpense, DebitCents: 8_000_00},
	}, rate("0.22"))
	if rugi.CurrentTaxCents != 0 {
		t.Fatalf("rugi harus beban pajak 0, dapat %d", rugi.CurrentTaxCents)
	}
}

func TestApplyCurrentTax(t *testing.T) {
	// Estimasi belum dibukukan sama sekali: laba periode turun sebesar beban
	// pajak kini, dan arus kas mendapat kembali pos non-kas sebesar itu.
	a := TaxAccrual{ProfitBeforeTaxCents: 50_000_00, CurrentTaxCents: 11_000_00}
	s := EquityStatement{UnclosedProfit: 50_000_00, UnclosedClosing: 50_000_00, TotalClosing: 150_000_00}
	s.ApplyCurrentTax(a)
	if s.UnclosedProfit != 39_000_00 || s.UnclosedClosing != 39_000_00 || s.TotalClosing != 139_000_00 {
		t.Fatalf("ekuitas salah: %+v", s)
	}

	c := CashFlow{NetIncome: 50_000_00}
	c.ApplyCurrentTax(a)
	if c.NetIncome != 39_000_00 {
		t.Fatalf("laba arus kas = %d, mau 3900000", c.NetIncome)
	}
	if c.OperatingNet() != 50_000_00 {
		t.Fatalf("kas neto operasi = %d, mau tetap 5000000", c.OperatingNet())
	}

	// Sebagian sudah dibukukan (1.000.000): yang disesuaikan hanya sisanya.
	s2 := EquityStatement{UnclosedProfit: 40_000_00, UnclosedClosing: 40_000_00, TotalClosing: 40_000_00}
	s2.ApplyCurrentTax(TaxAccrual{CurrentTaxCents: 11_000_00, BookedTaxCents: 10_000_00})
	if s2.UnclosedProfit != 39_000_00 {
		t.Fatalf("ekuitas dengan pajak terbukukan sebagian = %d, mau 3900000", s2.UnclosedProfit)
	}

	// Pajak sudah dibukukan penuh sesuai estimasi: tanpa penyesuaian.
	c2 := CashFlow{NetIncome: 39_000_00}
	c2.ApplyCurrentTax(TaxAccrual{CurrentTaxCents: 11_000_00, BookedTaxCents: 11_000_00})
	if len(c2.WorkingCapital) != 0 || c2.NetIncome != 39_000_00 {
		t.Fatalf("pajak terbukukan penuh tidak boleh disesuaikan: %+v", c2)
	}
}

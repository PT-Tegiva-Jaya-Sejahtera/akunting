package handlers

import (
	"testing"
	"time"

	"accounting/internal/database/db"
	"accounting/internal/models"
	"github.com/jackc/pgx/v5/pgtype"
)

func totalRow(code, name string, accountType db.AccountType, subType, psakLine string, debit, credit int64) db.ListAccountTotalsByPeriodRow {
	d, _ := models.CentsToNumeric(debit)
	c, _ := models.CentsToNumeric(credit)
	return db.ListAccountTotalsByPeriodRow{
		Code:        code,
		Name:        name,
		AccountType: accountType,
		SubType:     subType,
		PsakLine:    psakLine,
		TotalDebit:  d,
		TotalCredit: c,
	}
}

func TestMergeLines(t *testing.T) {
	cur := []models.ReportLine{
		{Code: "4-1000", Name: "Pendapatan Sewa", AmountCents: 1000},
		{Code: "5-1000", Name: "Beban Gaji", AmountCents: 2000},
	}
	pri := []models.ReportLine{
		{Code: "5-1000", Name: "Beban Gaji", AmountCents: 500},
		{Code: "5-2000", Name: "Beban Listrik", AmountCents: 750},
	}
	got := mergeLines(cur, pri)
	if len(got) != 3 {
		t.Fatalf("jumlah baris = %d, mau 3", len(got))
	}
	if got[0].Code != "4-1000" || got[0].PriorAmountCents != 0 {
		t.Fatalf("baris 1 salah: %+v", got[0])
	}
	if got[1].Code != "5-1000" || got[1].AmountCents != 2000 || got[1].PriorAmountCents != 500 {
		t.Fatalf("baris 2 salah: %+v", got[1])
	}
	if got[2].Code != "5-2000" || got[2].AmountCents != 0 || got[2].PriorAmountCents != 750 {
		t.Fatalf("baris 3 salah: %+v", got[2])
	}
}

func TestAssemblePLPisahBeban(t *testing.T) {
	rows := []db.ListAccountTotalsByPeriodRow{
		totalRow("4-1000", "Pendapatan Sewa", db.AccountTypeRevenue, models.SubRevenue, models.PsakPendapatan, 0, 1_000_000_00),
		totalRow("5-1000", "Beban Gaji", db.AccountTypeExpense, models.SubExpense, models.PsakBebanOperasional, 300_000_00, 0),
		totalRow("5-2000", "Beban Bunga", db.AccountTypeExpense, models.SubExpense, models.PsakBebanKeuangan, 50_000_00, 0),
		totalRow("5-3000", "Beban Pajak Penghasilan", db.AccountTypeExpense, models.SubExpense, models.PsakBebanPajak, 100_000_00, 0),
	}
	b := assemblePL(rows)
	if b.totalRevenue != 1_000_000_00 {
		t.Fatalf("total pendapatan = %d", b.totalRevenue)
	}
	if b.totalOperating != 300_000_00 || len(b.operating) != 1 {
		t.Fatalf("beban operasional salah: total=%d n=%d", b.totalOperating, len(b.operating))
	}
	if b.totalFinance != 50_000_00 || len(b.finance) != 1 {
		t.Fatalf("biaya keuangan salah: total=%d n=%d", b.totalFinance, len(b.finance))
	}
	if b.totalTax != 100_000_00 || len(b.tax) != 1 {
		t.Fatalf("beban pajak salah: total=%d n=%d", b.totalTax, len(b.tax))
	}

	var tarif pgtype.Numeric
	if err := tarif.Scan("0.22"); err != nil {
		t.Fatal(err)
	}
	var view models.ReportView
	applyProfitLoss(&view, b, plBlock{}, tarif)
	if view.OperatingProfit != 700_000_00 {
		t.Fatalf("laba operasi = %d, mau 70000000", view.OperatingProfit)
	}
	if view.ProfitBeforeTax != 650_000_00 {
		t.Fatalf("laba sebelum pajak = %d, mau 65000000", view.ProfitBeforeTax)
	}
	// Beban pajak kini = laba sebelum pajak x tarif = 650.000.000 x 22%.
	if view.TotalTaxExpense != 143_000_00 {
		t.Fatalf("beban pajak = %d, mau 14300000 (650.000.000 x 22%%)", view.TotalTaxExpense)
	}
	// Sudah ada beban pajak 100.000.000 lewat jurnal, jadi muncul baris koreksi.
	if len(view.TaxExpense) != 2 || view.TaxExpense[1].AmountCents != 43_000_00 {
		t.Fatalf("baris beban pajak salah: %+v", view.TaxExpense)
	}
	if view.NetIncome != 507_000_00 {
		t.Fatalf("laba bersih = %d, mau 50700000", view.NetIncome)
	}

	// Rugi: beban pajak 0, tidak negatif.
	var rugiView models.ReportView
	rugi := assemblePL([]db.ListAccountTotalsByPeriodRow{
		totalRow("4-1000", "Pendapatan Sewa", db.AccountTypeRevenue, models.SubRevenue, models.PsakPendapatan, 0, 100_000_00),
		totalRow("5-1000", "Beban Gaji", db.AccountTypeExpense, models.SubExpense, models.PsakBebanOperasional, 250_000_00, 0),
	})
	applyProfitLoss(&rugiView, rugi, plBlock{}, tarif)
	if rugiView.TotalTaxExpense != 0 {
		t.Fatalf("rugi harus beban pajak 0, dapat %d", rugiView.TotalTaxExpense)
	}
	if rugiView.NetIncome != -150_000_00 {
		t.Fatalf("laba bersih rugi = %d, mau -15000000", rugiView.NetIncome)
	}
}

func period(id int64, name string, start, end time.Time, status db.PeriodStatus) db.FiscalPeriod {
	return db.FiscalPeriod{
		ID:        id,
		Name:      name,
		StartDate: pgtype.Date{Time: start, Valid: true},
		EndDate:   pgtype.Date{Time: end, Valid: true},
		Status:    status,
	}
}

func TestResolveComparative(t *testing.T) {
	today := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	p2025 := period(1, "Tahun Buku 2025", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC), db.PeriodStatusClosed)
	p2026 := period(2, "Tahun Buku 2026", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC), db.PeriodStatusOpen)
	periods := []db.FiscalPeriod{p2026, p2025}

	// Periode terpilih: pembanding = periode fiskal sebelumnya.
	got := resolveComparative(periods, &p2026, false, p2026.StartDate.Time, today, "", today)
	if !got.ok || got.name != "Tahun Buku 2025" || !got.from.Equal(p2025.StartDate.Time) {
		t.Fatalf("pembanding periode salah: %+v", got)
	}

	// Parameter compare eksplisit menang.
	got = resolveComparative(periods, &p2026, false, p2026.StartDate.Time, today, "1", today)
	if !got.ok || got.name != "Tahun Buku 2025" {
		t.Fatalf("parameter compare salah: %+v", got)
	}

	// Periode pertama: tidak ada pembanding.
	got = resolveComparative(periods, &p2025, false, p2025.StartDate.Time, p2025.EndDate.Time, "", today)
	if got.ok {
		t.Fatalf("periode pertama seharusnya tanpa pembanding: %+v", got)
	}

	// Rentang kustom: pembanding = rentang sama panjang sebelumnya.
	fromT := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	toT := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	got = resolveComparative(nil, nil, true, fromT, toT, "", today)
	if !got.ok || !got.to.Equal(fromT.AddDate(0, 0, -1)) || !got.from.Equal(time.Date(2026, 1, 29, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("rentang kustom salah: %+v", got)
	}
}

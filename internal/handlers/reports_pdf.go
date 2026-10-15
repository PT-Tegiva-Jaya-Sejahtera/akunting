package handlers

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-pdf/fpdf"

	"accounting/internal/models"
)

// Tata letak PDF: A4 potret, satuan milimeter.
const (
	pdfPageW   = 210.0
	pdfMarginX = 20.0
	pdfRowH    = 6.0
	pdfCodeW   = 14.0
	pdfNameW   = 91.0
	pdfAmountW = 32.5  // dua kolom: periode berjalan + periode pembanding
	pdfBreakY  = 272.0 // batas bawah sebelum pindah halaman (ruang untuk footer)
)

// ReportsPLPdfGet mengunduh Laporan Laba Rugi sebagai PDF print-ready.
func (h *Handlers) ReportsPLPdfGet(w http.ResponseWriter, r *http.Request) {
	h.renderReportPDF(w, r, "pl", "Laporan Laba Rugi", "laba-rugi.pdf")
}

// ReportsBSPdfGet mengunduh Neraca (laporan posisi keuangan) sebagai PDF.
func (h *Handlers) ReportsBSPdfGet(w http.ResponseWriter, r *http.Request) {
	h.renderReportPDF(w, r, "bs", "Neraca", "neraca.pdf")
}

// renderReportPDF menyusun PDF laporan memakai angka yang sama dengan halaman
// HTML (models.ReportView), lalu menyajikannya sebagai lampiran unduhan.
func (h *Handlers) renderReportPDF(w http.ResponseWriter, r *http.Request, mode, title, filename string) {
	_, _, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	view, ok := h.reportView(w, r, company, mode)
	if !ok {
		return
	}

	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(pdfMarginX, 15, pdfMarginX)
	pdf.SetAutoPageBreak(true, 25)
	pdf.SetHeaderFunc(func() { drawPDFIdentity(pdf, view, title) })
	pdf.SetFooterFunc(func() { drawPDFFooter(pdf) })
	pdf.AddPage()

	switch mode {
	case "pl":
		drawPDFProfitLoss(pdf, view)
	default:
		drawPDFBalanceSheet(pdf, view)
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	if err := pdf.Output(w); err != nil {
		// Header sudah terkirim; catat ke log bila penulisan terputus.
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
	}
}

// drawPDFIdentity mencetak kepala laporan: identitas entitas, judul, periode,
// dan satuan penyajian. Dipanggil otomatis di setiap halaman.
func drawPDFIdentity(pdf *fpdf.Fpdf, view models.ReportView, title string) {
	pdf.SetFont("helvetica", "B", 12)
	pdf.SetTextColor(30, 30, 30)
	pdf.CellFormat(0, 6, view.CompanyName, "", 1, "L", false, 0, "")

	pdf.SetFont("helvetica", "", 9)
	pdf.SetTextColor(90, 90, 90)
	if view.CompanyAddr != "" {
		pdf.CellFormat(0, 4.5, view.CompanyAddr, "", 1, "L", false, 0, "")
	}
	if view.CompanyNpwp != "" {
		pdf.CellFormat(0, 4.5, "NPWP "+view.CompanyNpwp, "", 1, "L", false, 0, "")
	}

	pdf.Ln(2)
	pdf.SetFont("helvetica", "B", 13)
	pdf.SetTextColor(20, 20, 20)
	pdf.CellFormat(0, 7, title, "", 1, "C", false, 0, "")

	pdf.SetFont("helvetica", "", 9)
	pdf.SetTextColor(90, 90, 90)
	pdf.CellFormat(0, 4.5, "Untuk periode "+view.PeriodName, "", 1, "C", false, 0, "")
	pdf.CellFormat(0, 4.5, "Periode pelaporan "+view.DateFromLabel+" s.d. "+view.DateToLabel, "", 1, "C", false, 0, "")
	// Identitas PSAK 201 par. 49-53: terpisah vs konsolidasi, mata uang,
	// pembulatan.
	pdf.CellFormat(0, 4.5, view.ConsolidationLabel(), "", 1, "C", false, 0, "")
	pdf.CellFormat(0, 4.5, "Mata uang pelaporan "+view.BaseCurrency+"   ·   Pembulatan "+view.RoundingLabel, "", 1, "C", false, 0, "")
	pdf.Ln(3)
}

// drawPDFFooter mencetak garis, rujukan standar, tanggal cetak, dan nomor
// halaman.
func drawPDFFooter(pdf *fpdf.Fpdf) {
	pdf.SetY(-18)
	pdf.SetDrawColor(200, 200, 200)
	pdf.SetLineWidth(0.2)
	y := pdf.GetY()
	pdf.Line(pdfMarginX, y, pdfPageW-pdfMarginX, y)
	pdf.SetFont("helvetica", "", 8)
	pdf.SetTextColor(120, 120, 120)
	pdf.CellFormat(0, 5, fmt.Sprintf("Disusun menurut PSAK 201   ·   Dicetak %s   ·   Halaman %d",
		models.FormatDateID(time.Now()), pdf.PageNo()), "", 0, "C", false, 0, "")
}

// drawPDFColumnHeaders mencetak kepala kolom angka: periode berjalan dan
// periode pembanding (PSAK 201 par. 38). Label periode terbungkus bila
// panjang agar tidak menabrak kolom sebelahnya.
func drawPDFColumnHeaders(pdf *fpdf.Fpdf, view models.ReportView) {
	prior := "belum tersedia"
	if view.HasComparative {
		prior = view.PriorPeriodName
	}
	cur := view.DateFromLabel + " s.d. " + view.DateToLabel

	pdf.SetFont("helvetica", "B", 8)
	pdf.SetTextColor(90, 90, 90)
	pdf.CellFormat(pdfCodeW+pdfNameW, pdfRowH, "", "", 0, "L", false, 0, "")
	pdf.CellFormat(pdfAmountW, pdfRowH, "Periode Berjalan", "", 0, "R", false, 0, "")
	pdf.CellFormat(pdfAmountW, pdfRowH, "Periode Pembanding", "", 1, "R", false, 0, "")

	pdf.SetFont("helvetica", "", 7)
	curLines := pdf.SplitText(cur, pdfAmountW)
	priLines := pdf.SplitText(prior, pdfAmountW)
	n := len(curLines)
	if len(priLines) > n {
		n = len(priLines)
	}
	for i := 0; i < n; i++ {
		c, p := "", ""
		if i < len(curLines) {
			c = curLines[i]
		}
		if i < len(priLines) {
			p = priLines[i]
		}
		pdf.CellFormat(pdfCodeW+pdfNameW, 3.8, "", "", 0, "L", false, 0, "")
		pdf.CellFormat(pdfAmountW, 3.8, c, "", 0, "R", false, 0, "")
		pdf.CellFormat(pdfAmountW, 3.8, p, "", 1, "R", false, 0, "")
	}

	pdf.SetDrawColor(180, 180, 180)
	pdf.SetLineWidth(0.2)
	y := pdf.GetY()
	pdf.Line(pdfMarginX, y, pdfPageW-pdfMarginX, y)
	pdf.Ln(1)
}

// drawPDFProfitLoss menyusun baris Laporan Laba Rugi menurut PSAK 201 par. 82:
// pendapatan, beban operasional (menurut sifat, par. 99-105), biaya keuangan,
// dan beban pajak sebagai pos tersendiri, dengan kolom pembanding.
func drawPDFProfitLoss(pdf *fpdf.Fpdf, view models.ReportView) {
	drawPDFColumnHeaders(pdf, view)

	drawPDFSection(pdf, "Pendapatan")
	drawPDFLines(pdf, view, view.Revenue)
	drawPDFSubtotal(pdf, view, "Jumlah Pendapatan", view.TotalRevenue, view.PriorTotalRevenue)

	drawPDFSection(pdf, "Beban Operasional (menurut sifat)")
	drawPDFLines(pdf, view, view.OperatingExpense)
	drawPDFSubtotal(pdf, view, "Jumlah Beban Operasional", view.TotalOperatingExpense, view.PriorTotalOperatingExpense)
	drawPDFSubtotal(pdf, view, "Laba Operasi", view.OperatingProfit, view.PriorOperatingProfit)

	drawPDFSection(pdf, "Biaya Keuangan")
	drawPDFLines(pdf, view, view.FinanceCost)
	drawPDFSubtotal(pdf, view, "Jumlah Biaya Keuangan", view.TotalFinanceCost, view.PriorTotalFinanceCost)
	drawPDFSubtotal(pdf, view, "Laba Sebelum Pajak", view.ProfitBeforeTax, view.PriorProfitBeforeTax)

	drawPDFSection(pdf, "Beban Pajak Penghasilan")
	drawPDFLines(pdf, view, view.TaxExpense)
	drawPDFSubtotal(pdf, view, "Jumlah Beban Pajak", view.TotalTaxExpense, view.PriorTotalTaxExpense)

	drawPDFGrand(pdf, view, "Laba (Rugi) Periode Berjalan", view.NetIncome, view.PriorNetIncome)

	drawPDFNote(pdf, "Beban disajikan menurut sifat (PSAK 201 par. 99-105). Biaya keuangan (par. 82 huruf b) dan beban pajak (par. 82 huruf d) disajikan sebagai pos tersendiri. Bagian penghasilan komprehensif lain (par. 82A) belum disajikan.")
}

// drawPDFBalanceSheet menyusun baris Neraca mengikuti klasifikasi PSAK 201
// dengan kolom pembanding (par. 38).
func drawPDFBalanceSheet(pdf *fpdf.Fpdf, view models.ReportView) {
	drawPDFColumnHeaders(pdf, view)

	drawPDFSection(pdf, "Aset Lancar")
	drawPDFLines(pdf, view, view.CurrentAssets)
	drawPDFSubtotal(pdf, view, "Jumlah Aset Lancar", view.TotalCurrentAssets, view.PriorTotalCurrentAssets)

	drawPDFSection(pdf, "Aset Tidak Lancar")
	drawPDFLines(pdf, view, view.NonCurrentAssets)
	drawPDFSubtotal(pdf, view, "Jumlah Aset Tidak Lancar", view.TotalNonCurrentAssets, view.PriorTotalNonCurrentAssets)
	drawPDFSubtotal(pdf, view, "Jumlah Aset", view.TotalAssets, view.PriorTotalAssets)

	drawPDFSection(pdf, "Liabilitas Jangka Pendek")
	drawPDFLines(pdf, view, view.CurrentLiabilities)
	drawPDFSubtotal(pdf, view, "Jumlah Liabilitas Jangka Pendek", view.TotalCurrentLiabilities, view.PriorTotalCurrentLiabilities)

	drawPDFSection(pdf, "Liabilitas Jangka Panjang")
	drawPDFLines(pdf, view, view.NonCurrentLiabilities)
	drawPDFSubtotal(pdf, view, "Jumlah Liabilitas Jangka Panjang", view.TotalNonCurrentLiabilities, view.PriorTotalNonCurrentLiabilities)
	drawPDFSubtotal(pdf, view, "Jumlah Liabilitas", view.TotalLiabilities, view.PriorTotalLiabilities)

	drawPDFSection(pdf, "Ekuitas")
	drawPDFLines(pdf, view, view.Equity)
	if view.UnclosedProfit != 0 || view.PriorUnclosedProfit != 0 {
		drawPDFRow(pdf, view, "", "Laba (Rugi) Periode Belum Ditutup", view.UnclosedProfit, view.PriorUnclosedProfit, false)
	}
	drawPDFSubtotal(pdf, view, "Jumlah Ekuitas", view.TotalEquity+view.UnclosedProfit, view.PriorTotalEquity+view.PriorUnclosedProfit)

	drawPDFGrand(pdf, view, "Jumlah Liabilitas dan Ekuitas", view.TotalLiabEquity, view.PriorTotalLiabEquity)
}

// drawPDFSection mencetak judul bagian laporan.
func drawPDFSection(pdf *fpdf.Fpdf, name string) {
	pdf.SetFont("helvetica", "B", 10)
	pdf.SetTextColor(30, 30, 30)
	pdf.CellFormat(0, pdfRowH, name, "", 1, "L", false, 0, "")
}

// drawPDFLines mencetak seluruh baris akun beserta nominal periode berjalan
// dan periode pembanding.
func drawPDFLines(pdf *fpdf.Fpdf, view models.ReportView, lines []models.ReportLine) {
	for _, l := range lines {
		drawPDFRow(pdf, view, l.Code, l.Name, l.AmountCents, l.PriorAmountCents, false)
	}
}

// drawPDFSubtotal mencetak baris jumlah bagian dengan garis tunggal di atas
// kedua kolom nominal.
func drawPDFSubtotal(pdf *fpdf.Fpdf, view models.ReportView, label string, cents, prior int64) {
	drawPDFRow(pdf, view, "", label, cents, prior, true)
	pdf.SetDrawColor(80, 80, 80)
	pdf.SetLineWidth(0.25)
	y := pdf.GetY() - pdfRowH
	pdf.Line(pdfMarginX+pdfCodeW+pdfNameW, y, pdfPageW-pdfMarginX, y)
}

// drawPDFGrand mencetak baris total akhir dengan garis ganda di bawah nominal.
func drawPDFGrand(pdf *fpdf.Fpdf, view models.ReportView, label string, cents, prior int64) {
	drawPDFRow(pdf, view, "", label, cents, prior, true)
	pdf.SetDrawColor(30, 30, 30)
	pdf.SetLineWidth(0.25)
	y := pdf.GetY()
	pdf.Line(pdfMarginX+pdfCodeW+pdfNameW, y, pdfPageW-pdfMarginX, y)
	pdf.Line(pdfMarginX+pdfCodeW+pdfNameW, y+0.8, pdfPageW-pdfMarginX, y+0.8)
	pdf.Ln(1.6)
}

// drawPDFNote mencetak catatan kaki laporan dengan huruf kecil abu-abu.
func drawPDFNote(pdf *fpdf.Fpdf, text string) {
	pdf.SetFont("helvetica", "", 7.5)
	pdf.SetTextColor(120, 120, 120)
	pdf.MultiCell(0, 3.6, text, "", "L", false)
}

// drawPDFRow mencetak satu baris: kode akun, nama (terbungkus bila panjang),
// nominal periode berjalan, dan nominal periode pembanding, rata kanan
// memakai satuan penyajian perusahaan. Baris subtotal dan total dicetak
// tebal (bold = true).
func drawPDFRow(pdf *fpdf.Fpdf, view models.ReportView, code, name string, cents, prior int64, bold bool) {
	style := ""
	if bold {
		style = "B"
	}
	pdf.SetFont("helvetica", style, 10)
	nameLines := pdf.SplitText(name, pdfNameW)
	rowH := pdfRowH * float64(len(nameLines))
	if pdf.GetY()+rowH > pdfBreakY {
		pdf.AddPage()
	}

	pdf.SetTextColor(30, 30, 30)
	if code != "" {
		pdf.SetTextColor(110, 110, 110)
		pdf.SetFont("helvetica", "", 9)
		pdf.CellFormat(pdfCodeW, pdfRowH, code, "", 0, "L", false, 0, "")
	} else {
		pdf.CellFormat(pdfCodeW, pdfRowH, "", "", 0, "L", false, 0, "")
	}

	pdf.SetTextColor(30, 30, 30)
	pdf.SetFont("helvetica", style, 10)
	pdf.CellFormat(pdfNameW, pdfRowH, nameLines[0], "", 0, "L", false, 0, "")

	pdf.SetFont("helvetica", style, 10)
	pdf.CellFormat(pdfAmountW, pdfRowH, models.FormatRounded(cents, view.RoundingUnit), "", 0, "R", false, 0, "")

	pdf.SetTextColor(110, 110, 110)
	pdf.SetFont("helvetica", style, 10)
	pdf.CellFormat(pdfAmountW, pdfRowH, view.PriorMoney(prior), "", 1, "R", false, 0, "")

	for _, extra := range nameLines[1:] {
		pdf.CellFormat(pdfCodeW+pdfNameW, pdfRowH, extra, "", 1, "L", false, 0, "")
	}
}

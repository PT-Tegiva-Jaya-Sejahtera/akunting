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
	pdfNameW   = 111.0
	pdfAmountW = 45.0
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
	pdf.CellFormat(0, 4.5, view.DateFromLabel+" s.d. "+view.DateToLabel, "", 1, "C", false, 0, "")
	if view.EntityNote != "" {
		pdf.CellFormat(0, 4.5, view.EntityNote, "", 1, "C", false, 0, "")
	}
	pdf.CellFormat(0, 4.5, view.RoundingLabel, "", 1, "C", false, 0, "")
	pdf.Ln(3)
}

// drawPDFFooter mencetak garis, tanggal cetak, dan nomor halaman.
func drawPDFFooter(pdf *fpdf.Fpdf) {
	pdf.SetY(-18)
	pdf.SetDrawColor(200, 200, 200)
	pdf.SetLineWidth(0.2)
	y := pdf.GetY()
	pdf.Line(pdfMarginX, y, pdfPageW-pdfMarginX, y)
	pdf.SetFont("helvetica", "", 8)
	pdf.SetTextColor(120, 120, 120)
	pdf.CellFormat(0, 5, fmt.Sprintf("Dicetak %s   ·   Halaman %d",
		models.FormatDateID(time.Now()), pdf.PageNo()), "", 0, "C", false, 0, "")
}

// drawPDFProfitLoss menyusun baris Laporan Laba Rugi.
func drawPDFProfitLoss(pdf *fpdf.Fpdf, view models.ReportView) {
	drawPDFSection(pdf, "Pendapatan")
	drawPDFLines(pdf, view, view.Revenue)
	drawPDFSubtotal(pdf, view, "Jumlah Pendapatan", view.TotalRevenue)

	drawPDFSection(pdf, "Beban")
	drawPDFLines(pdf, view, view.Expense)
	drawPDFSubtotal(pdf, view, "Jumlah Beban", view.TotalExpense)

	drawPDFGrand(pdf, view, "Laba (Rugi) Periode Berjalan", view.NetIncome)
}

// drawPDFBalanceSheet menyusun baris Neraca mengikuti klasifikasi PSAK 201.
func drawPDFBalanceSheet(pdf *fpdf.Fpdf, view models.ReportView) {
	drawPDFSection(pdf, "Aset Lancar")
	drawPDFLines(pdf, view, view.CurrentAssets)
	drawPDFSubtotal(pdf, view, "Jumlah Aset Lancar", view.TotalCurrentAssets)

	drawPDFSection(pdf, "Aset Tidak Lancar")
	drawPDFLines(pdf, view, view.NonCurrentAssets)
	drawPDFSubtotal(pdf, view, "Jumlah Aset Tidak Lancar", view.TotalNonCurrentAssets)
	drawPDFSubtotal(pdf, view, "Jumlah Aset", view.TotalAssets)

	drawPDFSection(pdf, "Liabilitas Jangka Pendek")
	drawPDFLines(pdf, view, view.CurrentLiabilities)
	drawPDFSubtotal(pdf, view, "Jumlah Liabilitas Jangka Pendek", view.TotalCurrentLiabilities)

	drawPDFSection(pdf, "Liabilitas Jangka Panjang")
	drawPDFLines(pdf, view, view.NonCurrentLiabilities)
	drawPDFSubtotal(pdf, view, "Jumlah Liabilitas Jangka Panjang", view.TotalNonCurrentLiabilities)
	drawPDFSubtotal(pdf, view, "Jumlah Liabilitas", view.TotalLiabilities)

	drawPDFSection(pdf, "Ekuitas")
	drawPDFLines(pdf, view, view.Equity)
	if view.UnclosedProfit != 0 {
		drawPDFRow(pdf, view, "", "Laba (Rugi) Periode Belum Ditutup", view.UnclosedProfit, false)
	}
	drawPDFSubtotal(pdf, view, "Jumlah Ekuitas", view.TotalEquity+view.UnclosedProfit)

	drawPDFGrand(pdf, view, "Jumlah Liabilitas dan Ekuitas", view.TotalLiabEquity)
}

// drawPDFSection mencetak judul bagian laporan.
func drawPDFSection(pdf *fpdf.Fpdf, name string) {
	pdf.SetFont("helvetica", "B", 10)
	pdf.SetTextColor(30, 30, 30)
	pdf.CellFormat(0, pdfRowH, name, "", 1, "L", false, 0, "")
}

// drawPDFLines mencetak seluruh baris akun beserta nominalnya.
func drawPDFLines(pdf *fpdf.Fpdf, view models.ReportView, lines []models.ReportLine) {
	for _, l := range lines {
		drawPDFRow(pdf, view, l.Code, l.Name, l.AmountCents, false)
	}
}

// drawPDFSubtotal mencetak baris jumlah bagian dengan garis tunggal di atas
// kolom nominal.
func drawPDFSubtotal(pdf *fpdf.Fpdf, view models.ReportView, label string, cents int64) {
	drawPDFRow(pdf, view, "", label, cents, true)
	pdf.SetDrawColor(80, 80, 80)
	pdf.SetLineWidth(0.25)
	y := pdf.GetY() - pdfRowH
	pdf.Line(pdfMarginX+pdfCodeW+pdfNameW, y, pdfPageW-pdfMarginX, y)
}

// drawPDFGrand mencetak baris total akhir dengan garis ganda di bawah nominal.
func drawPDFGrand(pdf *fpdf.Fpdf, view models.ReportView, label string, cents int64) {
	drawPDFRow(pdf, view, "", label, cents, true)
	pdf.SetDrawColor(30, 30, 30)
	pdf.SetLineWidth(0.25)
	y := pdf.GetY()
	pdf.Line(pdfMarginX+pdfCodeW+pdfNameW, y, pdfPageW-pdfMarginX, y)
	pdf.Line(pdfMarginX+pdfCodeW+pdfNameW, y+0.8, pdfPageW-pdfMarginX, y+0.8)
	pdf.Ln(1.6)
}

// drawPDFRow mencetak satu baris: kode akun, nama (terbungkus bila panjang),
// dan nominal rata kanan memakai satuan penyajian perusahaan. Baris subtotal
// dan total dicetak tebal (bold = true).
func drawPDFRow(pdf *fpdf.Fpdf, view models.ReportView, code, name string, cents int64, bold bool) {
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
	pdf.CellFormat(pdfAmountW, pdfRowH, models.FormatRounded(cents, view.RoundingUnit), "", 1, "R", false, 0, "")

	for _, extra := range nameLines[1:] {
		pdf.CellFormat(pdfCodeW+pdfNameW, pdfRowH, extra, "", 1, "L", false, 0, "")
	}
}

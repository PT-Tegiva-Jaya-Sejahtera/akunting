package handlers

import (
	"encoding/json"
	"net/http"

	"accounting/internal/models"
)

// taxRingkasan adalah isi halaman ringkasan pajak penghasilan (PSAK 212).
// Angka dalam satuan sen agar presisi; tarif tampil sebagai persen gaya
// Indonesia.
type taxRingkasan struct {
	Periode                string `json:"periode"`
	MataUang               string `json:"mata_uang"`
	TarifPersen            string `json:"tarif_persen"`
	LabaSebelumPajakSen    int64  `json:"laba_sebelum_pajak_sen"`
	BebanPajakKiniSen      int64  `json:"beban_pajak_kini_sen"`
	BebanPajakTerbukukanSen int64 `json:"beban_pajak_terbukukan_sen"`
	LiabilitasPajakKiniSen int64  `json:"liabilitas_pajak_kini_sen"`
}

// TaxGet menampilkan ringkasan pajak penghasilan kini (PSAK 212) untuk periode
// laporan: tarif perusahaan, laba sebelum pajak, beban pajak kini, dan
// liabilitas pajak kini. Angka dihitung models.AccrueTax, sama dengan yang
// dipakai Laporan Laba Rugi dan Neraca. Format JSON sederhana.
func (h *Handlers) TaxGet(w http.ResponseWriter, r *http.Request) {
	_, _, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	win, ok := h.reportWindow(w, r, company, "tax")
	if !ok {
		return
	}
	movRows, err := h.periodMovementRows(r.Context(), company.ID, win.from, win.to)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	a := models.AccrueTax(keAccountTotals(movRows), company.TaxRate)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(taxRingkasan{
		Periode:                 win.view.PeriodName,
		MataUang:                company.BaseCurrency,
		TarifPersen:             models.RatePercent(company.TaxRate),
		LabaSebelumPajakSen:     a.ProfitBeforeTaxCents,
		BebanPajakKiniSen:       a.CurrentTaxCents,
		BebanPajakTerbukukanSen: a.BookedTaxCents,
		LiabilitasPajakKiniSen:  a.CurrentTaxCents - a.BookedTaxCents,
	})
}

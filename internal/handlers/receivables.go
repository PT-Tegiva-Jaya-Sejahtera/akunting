// Subledger piutang usaha (Stage 3a): saldo terbuka per pelanggan dengan
// kolom aging, dihitung dari tanggal jatuh tempo vs tanggal laporan.
// Angka disusun dari mutasi ledger lewat models.BuildReceivableAging
// supaya bisa diuji tanpa HTTP.
package handlers

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"accounting/internal/database/db"
	"accounting/internal/models"
	"accounting/templates"
)

// ReceivablesGet menampilkan subledger piutang usaha per tanggal laporan.
// Parameter "to" (dd/mm/yyyy) memilih tanggal laporan; bila kosong dipakai
// hari ini. Aging memakai tanggal jatuh tempo; invoice tanpa due_date menua
// dari tanggal jurnalnya.
func (h *Handlers) ReceivablesGet(w http.ResponseWriter, r *http.Request) {
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}

	now := time.Now()
	asOf := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if s := r.URL.Query().Get("to"); s != "" {
		t, err := models.ParseDateID(s)
		if err != nil {
			http.Error(w, "400 tanggal laporan harus berformat dd/mm/yyyy", http.StatusBadRequest)
			return
		}
		asOf = t
	}

	movs, err := h.DB.ListReceivableMovements(r.Context(), db.ListReceivableMovementsParams{
		CompanyID: company.ID,
		AsOf:      pgtype.Date{Time: asOf, Valid: true, InfinityModifier: pgtype.Finite},
	})
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}

	items := make([]models.ReceivableItem, 0, len(movs))
	for _, m := range movs {
		items = append(items, models.ReceivableItem{
			JournalID:   m.JournalID,
			Reference:   m.Reference,
			PartyID:     m.PartyID,
			PartyName:   m.PartyName,
			JournalDate: m.JournalDate.Time,
			DueDate:     m.DueDate.Time,
			HasDueDate:  m.DueDate.Valid,
			AmountCents: models.NumericToCents(m.TotalDebit) - models.NumericToCents(m.TotalCredit),
		})
	}

	aging := models.BuildReceivableAging(items, asOf)
	view := models.ReceivablesView{
		AsOfISO:       asOf.Format("2006-01-02"),
		AsOfLabel:     models.FormatDateID(asOf),
		Rows:          aging.Rows,
		Total:         aging.Total,
		RoundingUnit:  company.RoundingUnit,
		RoundingLabel: models.RoundingLabel(company.RoundingUnit),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.ReceivablesPage(h.newShell(r, user, company, role, "receivables",
		"Piutang Usaha "+company.Name), company, view).Render(r.Context(), w)
}

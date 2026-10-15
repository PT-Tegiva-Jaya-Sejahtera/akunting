// Subledger hutang usaha (Stage 3b): saldo terbuka per supplier dengan
// kolom aging, dihitung dari tanggal jatuh tempo vs tanggal laporan.
// Angka disusun dari mutasi ledger lewat models.BuildPayableAging; arah
// saldo dibalik dari piutang (kredit - debit) karena hutang bersaldo kredit.
package handlers

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"accounting/internal/database/db"
	"accounting/internal/models"
	"accounting/templates"
)

// PayablesGet menampilkan subledger hutang usaha per tanggal laporan.
// Parameter "to" (dd/mm/yyyy) memilih tanggal laporan; bila kosong dipakai
// hari ini. Aging memakai tanggal jatuh tempo; tagihan tanpa due_date menua
// dari tanggal jurnalnya.
func (h *Handlers) PayablesGet(w http.ResponseWriter, r *http.Request) {
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

	movs, err := h.DB.ListPayableMovements(r.Context(), db.ListPayableMovementsParams{
		CompanyID: company.ID,
		AsOf:      pgtype.Date{Time: asOf, Valid: true, InfinityModifier: pgtype.Finite},
	})
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}

	items := make([]models.PayableItem, 0, len(movs))
	for _, m := range movs {
		items = append(items, models.PayableItem{
			JournalID:   m.JournalID,
			Reference:   m.Reference,
			PartyID:     m.PartyID,
			PartyName:   m.PartyName,
			JournalDate: m.JournalDate.Time,
			DueDate:     m.DueDate.Time,
			HasDueDate:  m.DueDate.Valid,
			// Saldo hutang = kebalikan arah debit/kredit dari piutang.
			AmountCents: models.NumericToCents(m.TotalCredit) - models.NumericToCents(m.TotalDebit),
		})
	}

	aging := models.BuildPayableAging(items, asOf)
	view := models.PayablesView{
		AsOfISO:       asOf.Format("2006-01-02"),
		AsOfLabel:     models.FormatDateID(asOf),
		Rows:          aging.Rows,
		Total:         aging.Total,
		RoundingUnit:  company.RoundingUnit,
		RoundingLabel: models.RoundingLabel(company.RoundingUnit),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.PayablesPage(h.newShell(r, user, company, role, "payables",
		"Hutang Usaha "+company.Name), company, view).Render(r.Context(), w)
}

package handlers

import (
	"net/http"

	"accounting/internal/database/db"
	"accounting/internal/models"
	"accounting/templates"
)

// ClosingGet menampilkan halaman Jurnal Penutup: daftar jurnal penutup
// (kind=closing) beserta baris-barisnya, periode yang masih terbuka untuk
// ditutup, dan penjelasan singkat alur tutup buku.
func (h *Handlers) ClosingGet(w http.ResponseWriter, r *http.Request) {
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	periods, err := h.DB.ListFiscalPeriods(ctx, company.ID)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	periodByID := map[int64]db.FiscalPeriod{}
	openPeriods := []models.PeriodView{}
	for _, p := range periods {
		periodByID[p.ID] = p
		if p.Status != db.PeriodStatusClosed {
			openPeriods = append(openPeriods, periodView(p))
		}
	}

	closings, err := h.DB.GetSystemJournals(ctx, db.GetSystemJournalsParams{
		CompanyID: company.ID,
		Kind:      db.JournalKindClosing,
	})
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}

	view := models.ClosingView{
		OpenPeriods:   openPeriods,
		ClosedCount:   len(periods) - len(openPeriods),
		RoundingUnit:  company.RoundingUnit,
		RoundingLabel: models.RoundingLabel(company.RoundingUnit),
	}
	for _, j := range closings {
		entries, err := h.DB.GetJournalWithEntries(ctx, db.GetJournalWithEntriesParams{
			JournalID: j.ID,
			CompanyID: company.ID,
		})
		if err != nil {
			http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
			return
		}
		cj := models.ClosingJournal{
			DateLabel: j.JournalDate.Time.Format("02 Jan 2006"),
			DateShort: models.FormatDateShort(j.JournalDate.Time),
			Reference: j.Reference,
			Memo:      j.Memo,
			PeriodID:  j.PeriodID.Int64,
		}
		if p, ok := periodByID[j.PeriodID.Int64]; ok {
			cj.PeriodName = p.Name
			if p.Status == db.PeriodStatusClosed {
				cj.Status = "closed"
			} else {
				cj.Status = "open"
			}
		}
		for _, e := range entries {
			line := models.ClosingLine{
				AccountCode: e.AccountCode,
				AccountName: e.AccountName,
				Description: e.Description,
				Debit:       models.NumericToCents(e.Debit),
				Credit:      models.NumericToCents(e.Credit),
			}
			cj.Total += line.Debit
			cj.Lines = append(cj.Lines, line)
		}
		view.Journals = append(view.Journals, cj)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.ClosingPage(h.newShell(r, user, company, role, "closing", "Jurnal Penutup "+company.Name),
		company, view).Render(ctx, w)
}

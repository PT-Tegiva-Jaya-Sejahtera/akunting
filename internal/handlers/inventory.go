// Persediaan (PSAK 202, Stage 4b): daftar item, mutasi stok, harga rata-rata
// tertimbang, dan nilai persediaan per tanggal laporan. Jurnal pembelian,
// penjualan, dan HPP dibuat manual oleh user; halaman ini juga menampilkan
// pembanding saldo akun persediaan di buku besar sebagai rekomendasi angka
// untuk Neraca (aset lancar).
package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"accounting/internal/database/db"
	appmw "accounting/internal/middleware"
	"accounting/internal/models"
	"accounting/templates"
)

// InventoryGet menampilkan halaman persediaan. Parameter "to" (dd/mm/yyyy)
// memilih tanggal laporan; bila kosong dipakai hari ini.
func (h *Handlers) InventoryGet(w http.ResponseWriter, r *http.Request) {
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

	h.renderInventory(w, r, user, role, company, asOf, "")
}

// renderInventory menyusun halaman persediaan: daftar item dengan saldo qty,
// harga rata-rata tertimbang, nilai persediaan, riwayat mutasi ber-nilai,
// dan pembanding saldo akun persediaan di buku besar.
func (h *Handlers) renderInventory(w http.ResponseWriter, r *http.Request,
	user appmw.SessionUser, role string, company db.Company,
	asOf time.Time, errMsg string) {

	view, err := h.buildInventoryView(r, company, role, asOf)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.InventoryPage(h.newShell(r, user, company, role, "inventory",
		"Persediaan "+company.Name), company, view, errMsg).Render(r.Context(), w)
}

// buildInventoryView memuat item + mutasi perusahaan sampai tanggal laporan,
// menghitung harga rata-rata tertimbang lewat models.BuildInventory, lalu
// menambah pembanding saldo akun psak_line persediaan di buku besar.
func (h *Handlers) buildInventoryView(r *http.Request, company db.Company, role string,
	asOf time.Time) (models.InventoryView, error) {

	dbItems, err := h.DB.ListInventoryItems(r.Context(), company.ID)
	if err != nil {
		return models.InventoryView{}, err
	}
	items := make([]models.InventoryItem, 0, len(dbItems))
	for _, it := range dbItems {
		items = append(items, models.InventoryItem{
			ID: it.ID, CompanyID: it.CompanyID,
			Name: it.Name, SKU: it.Sku, Unit: it.Unit,
		})
	}

	moves, err := h.loadInventoryMovements(r, company.ID, asOf)
	if err != nil {
		return models.InventoryView{}, err
	}

	calc, err := models.BuildInventory(items, moves)
	if err != nil {
		// Mutasi yang sudah tersimpan seharusnya selalu valid (dicegah saat
		// insert); kalau tetap gagal, tampilkan tanpa riwayat daripada 500.
		calc, _ = models.BuildInventory(items, nil)
	}

	// Pembanding menurut buku besar: saldo akun psak_line persediaan sampai
	// tanggal laporan (aset lancar di Neraca). Jurnal pembelian dan penjualan
	// dibuat manual oleh user; angka di sebelahnya adalah rekomendasi modul.
	totals, err := h.DB.ListAccountTotalsByPeriod(r.Context(), db.ListAccountTotalsByPeriodParams{
		CompanyID: company.ID,
		DateFrom:  pgtype.Date{Time: time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true, InfinityModifier: pgtype.Finite},
		DateTo:    pgtype.Date{Time: asOf, Valid: true, InfinityModifier: pgtype.Finite},
	})
	if err != nil {
		return models.InventoryView{}, err
	}
	var buku models.InventoryCOABuku
	persediaanCode := ""
	for _, t := range totals {
		if t.PsakLine == models.PsakPersediaan {
			buku.LedgerCents += models.NumericToCents(t.TotalDebit) - models.NumericToCents(t.TotalCredit)
			if persediaanCode == "" {
				persediaanCode = t.Code
			}
		}
	}

	// Rekap stok seluruh barang: kumulatif masuk dan keluar beserta tanggal
	// terakhirnya, dan sisa stok per item (no, kode akun persediaan, nama
	// barang, quantity, tgl masuk, tgl keluar, sisa).
	type rekapAgg struct {
		in, out         int64
		lastIn, lastOut time.Time
		hasIn, hasOut   bool
	}
	agg := map[int64]*rekapAgg{}
	for _, m := range calc.Movements {
		a, ok := agg[m.ItemID]
		if !ok {
			a = &rekapAgg{}
			agg[m.ItemID] = a
		}
		if m.MovementType == models.MovementOut {
			a.out += m.QtyMills
			if !a.hasOut || m.Date.After(a.lastOut) {
				a.lastOut, a.hasOut = m.Date, true
			}
		} else {
			a.in += m.QtyMills
			if !a.hasIn || m.Date.After(a.lastIn) {
				a.lastIn, a.hasIn = m.Date, true
			}
		}
	}
	rekap := make([]models.InventoryRekapRow, 0, len(items))
	for i, it := range items {
		row := models.InventoryRekapRow{
			No:          i + 1,
			AccountCode: persediaanCode,
			Name:        it.Name,
			Unit:        it.Unit,
		}
		a := agg[it.ID]
		if a != nil {
			row.QtyInLabel = models.FormatQty(a.in)
			row.QtyOutLabel = models.FormatQty(a.out)
			row.SisaLabel = models.FormatQty(a.in - a.out)
			if a.hasIn {
				row.LastInLabel = models.FormatDateID(a.lastIn)
			}
			if a.hasOut {
				row.LastOutLabel = models.FormatDateID(a.lastOut)
			}
		} else {
			row.QtyInLabel = "-"
			row.QtyOutLabel = "-"
			row.SisaLabel = "-"
		}
		rekap = append(rekap, row)
	}

	return models.InventoryView{
		AsOfISO:     asOf.Format("2006-01-02"),
		AsOfLabel:   models.FormatDateID(asOf),
		Rows:        calc.Rows,
		TotalCents:  calc.TotalCents,
		Movements:   calc.Movements,
		Items:       items,
		Buku:        buku,
		Rekap:       rekap,
		AccountCode: persediaanCode,
		CanManage:   role == appmw.RoleAdmin || role == appmw.RoleOwner,
	}, nil
}

// loadInventoryMovements memuat mutasi sampai tanggal laporan dan
// mengubahnya ke bentuk model (jumlah milli-unit, biaya sen).
func (h *Handlers) loadInventoryMovements(r *http.Request, companyID int64,
	asOf time.Time) ([]models.InventoryMovement, error) {

	dbMovs, err := h.DB.ListInventoryMovements(r.Context(), db.ListInventoryMovementsParams{
		CompanyID: companyID,
		AsOf:      pgtype.Date{Time: asOf, Valid: true, InfinityModifier: pgtype.Finite},
	})
	if err != nil {
		return nil, err
	}
	moves := make([]models.InventoryMovement, 0, len(dbMovs))
	for _, m := range dbMovs {
		moves = append(moves, models.InventoryMovement{
			ID: m.ID, CompanyID: m.CompanyID, ItemID: m.ItemID,
			Date:          m.Date.Time,
			QtyMills:      models.NumericToMills(m.Quantity),
			UnitCostCents: models.NumericToCents(m.UnitCost),
			MovementType:  string(m.MovementType),
		})
	}
	return moves, nil
}

// InventoryItemCreatePost mendaftarkan item persediaan baru.
func (h *Handlers) InventoryItemCreatePost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "400 form tidak valid", http.StatusBadRequest)
		return
	}
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	sku := strings.TrimSpace(r.FormValue("sku"))
	unit := strings.TrimSpace(r.FormValue("unit"))
	errMsg := ""
	if name == "" {
		errMsg = "Nama item wajib diisi."
	}

	if errMsg != "" {
		h.renderInventoryErr(w, r, user, role, company, errMsg)
		return
	}

	_, err := h.DB.CreateInventoryItem(r.Context(), db.CreateInventoryItemParams{
		CompanyID: company.ID, Name: name, Sku: sku, Unit: unit,
	})
	if err != nil {
		h.renderInventoryErr(w, r, user, role, company,
			"Gagal menyimpan item (SKU mungkin sudah dipakai).")
		return
	}
	http.Redirect(w, r, inventoryPath(company.UrlKey), http.StatusSeeOther)
}

// InventoryMovementCreatePost mencatat satu mutasi stok (masuk/keluar).
// Mutasi keluar dinilai modul dengan harga rata-rata tertimbang saat itu,
// jadi kolom harga satuan hanya dipakai untuk mutasi masuk.
func (h *Handlers) InventoryMovementCreatePost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "400 form tidak valid", http.StatusBadRequest)
		return
	}
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}

	itemID, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("item_id")), 10, 64)
	if err != nil || itemID <= 0 {
		h.renderInventoryErr(w, r, user, role, company, "Item harus dipilih.")
		return
	}
	movementType := r.FormValue("movement_type")
	if movementType != models.MovementIn && movementType != models.MovementOut {
		h.renderInventoryErr(w, r, user, role, company, "Jenis mutasi tidak dikenal.")
		return
	}
	date, errD := models.ParseDateID(r.FormValue("date"))
	if errD != nil {
		h.renderInventoryErr(w, r, user, role, company, "Tanggal mutasi harus berformat dd/mm/yyyy.")
		return
	}
	qtyMills, errQ := models.ParseQty(r.FormValue("quantity"))
	if errQ != nil || qtyMills <= 0 {
		h.renderInventoryErr(w, r, user, role, company, "Jumlah harus lebih besar dari nol (maksimal 3 desimal).")
		return
	}
	unitCost := int64(0)
	if movementType == models.MovementIn {
		cost, errC := models.ParseMoney(r.FormValue("unit_cost"))
		if errC != nil || cost <= 0 {
			h.renderInventoryErr(w, r, user, role, company, "Harga satuan pembelian harus lebih besar dari nol.")
			return
		}
		unitCost = cost
	}

	// Validasi saldo: mutasi keluar tidak boleh melebihi saldo pada
	// posisinya dalam urutan tanggal, sama dengan perhitungan tampilan.
	dbItems, err := h.DB.ListInventoryItems(r.Context(), company.ID)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	items := make([]models.InventoryItem, 0, len(dbItems))
	known := false
	for _, it := range dbItems {
		items = append(items, models.InventoryItem{
			ID: it.ID, CompanyID: it.CompanyID,
			Name: it.Name, SKU: it.Sku, Unit: it.Unit,
		})
		if it.ID == itemID {
			known = true
		}
	}
	if !known {
		h.renderInventoryErr(w, r, user, role, company, "Item tidak ditemukan di perusahaan ini.")
		return
	}
	now := time.Now()
	moves, err := h.loadInventoryMovements(r, company.ID,
		time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC))
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	err = models.ValidateMovement(items, moves, models.InventoryMovement{
		CompanyID: company.ID, ItemID: itemID, Date: date,
		QtyMills: qtyMills, UnitCostCents: unitCost, MovementType: movementType,
	})
	if errors.Is(err, models.ErrInsufficientStock) {
		h.renderInventoryErr(w, r, user, role, company,
			"Saldo persediaan tidak mencukupi untuk pengeluaran ini.")
		return
	}
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}

	quantity, err := models.MillsToNumeric(qtyMills)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	_, err = h.DB.CreateInventoryMovement(r.Context(), db.CreateInventoryMovementParams{
		CompanyID: company.ID, ItemID: itemID,
		Date:         pgtype.Date{Time: date, Valid: true, InfinityModifier: pgtype.Finite},
		Quantity:     quantity,
		UnitCost:     mustNumeric(unitCost),
		MovementType: db.InventoryMovementType(movementType),
	})
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, inventoryPath(company.UrlKey), http.StatusSeeOther)
}

// renderInventoryErr menampilkan ulang halaman persediaan dengan pesan
// galat, memakai tanggal laporan hari ini.
func (h *Handlers) renderInventoryErr(w http.ResponseWriter, r *http.Request,
	user appmw.SessionUser, role string, company db.Company, errMsg string) {
	now := time.Now()
	h.renderInventory(w, r, user, role, company,
		time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC), errMsg)
}

func inventoryPath(companyKey string) string {
	return "/companies/" + companyKey + "/inventory"
}

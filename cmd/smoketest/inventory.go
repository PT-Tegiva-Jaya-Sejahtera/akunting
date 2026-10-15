// Bukti runtime persediaan (PSAK 202, Stage 4b). Satu perusahaan uji disi
// dengan item, tiga pembelian beda harga, dan satu penjualan, lalu:
//
//  1. harga rata-rata tertimbang dihitung ulang setelah tiap pembelian dan
//     dibandingkan ke angka harapan per titik (sen);
//  2. barang keluar dinilai dengan harga rata-rata saat itu (COGS) dan saldo
//     nilai persediaan dicek konsisten dengan qty x avg cost;
//  3. mutasi keluar melebihi saldo ditolak, FK komposit multi-tenant menolak
//     item perusahaan lain, dan CHECK quantity > 0 berlaku;
//  4. route /inventory dirender lewat router dengan middleware asli
//     (RequireAuth + RequireCompany) dan angkanya dibaca kembali dari HTML.
//
// Set SMOKE_SAVE_DIR untuk menyimpan HTML hasil render sebagai bukti berkas.
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"accounting/internal/auth"
	"accounting/internal/database/db"
	"accounting/internal/handlers"
	appmw "accounting/internal/middleware"
	"accounting/internal/models"
)

// evidenceInventory menjalankan seluruh bukti persediaan.
func evidenceInventory(ctx context.Context, pool *pgxpool.Pool, q *db.Queries, userID int64) {
	// ===== 1. Perusahaan, akun COA, item =====
	company, err := q.CreateCompany(ctx, db.CreateCompanyParams{
		Name: "PT Bukti Persediaan", Slug: "pt-bukti-persediaan",
		BaseCurrency: "IDR", UrlKey: "bukti4persediaan",
	})
	if err != nil {
		panic(err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO company_users (company_id, user_id, role) VALUES ($1, $2, 'owner')`,
		company.ID, userID); err != nil {
		panic(err)
	}

	akunID := map[string]int64{}
	for _, a := range []akunUji{
		{"1-1000", "Kas", "asset", models.SubCurrentAsset, "debit", models.PsakKasSetaraKas},
		{"1-3000", "Persediaan", "asset", models.SubCurrentAsset, "debit", models.PsakPersediaan},
	} {
		var id int64
		err := pool.QueryRow(ctx, `
			INSERT INTO accounts (company_id, code, name, account_type, sub_type, normal_balance, psak_line)
			VALUES ($1, $2, $3, $4::account_type, $5, $6::normal_balance, $7) RETURNING id`,
			company.ID, a.code, a.name, a.tipe, a.sub, a.normal, a.psak).Scan(&id)
		if err != nil {
			panic(fmt.Errorf("akun %s: %w", a.code, err))
		}
		akunID[a.code] = id
	}

	item, err := q.CreateInventoryItem(ctx, db.CreateInventoryItemParams{
		CompanyID: company.ID, Name: "Kertas A4", Sku: "SKU-A4", Unit: "rim",
	})
	if err != nil {
		panic(err)
	}
	itemLain, err := q.CreateInventoryItem(ctx, db.CreateInventoryItemParams{
		CompanyID: company.ID, Name: "Pulpen Hitam", Sku: "SKU-PN", Unit: "pcs",
	})
	if err != nil {
		panic(err)
	}
	fmt.Println("PASS seed bukti persediaan: 2 akun, 2 item")

	// ===== 2. Mutasi: 3 pembelian beda harga + 1 penjualan =====
	// Harapan per tanggal laporan 15/10/2026:
	//   in  10 rim @ 10.000 (01/10) -> avg 10.000,00
	//   in   5 rim @ 12.000 (05/10) -> avg 10.666,67  ((100.000+60.000)/15)
	//   in   2 rim @  8.000 (10/10) -> avg 10.352,94  ((15x10.666,67+16.000)/17)
	//   out  8 rim        (15/10) -> COGS 8 x 10.352,94 = 82.823,52
	//   saldo 9 rim x 10.352,94 = 93.176,46 (nilai persediaan)
	mutasi := func(itemID int64, tipe string, mills, costRupiah int64, tanggal time.Time) {
		quantity, err := models.MillsToNumeric(mills)
		if err != nil {
			panic(err)
		}
		_, err = q.CreateInventoryMovement(ctx, db.CreateInventoryMovementParams{
			CompanyID: company.ID, ItemID: itemID,
			Date:         tgl(tanggal),
			Quantity:     quantity,
			UnitCost:     num(fmt.Sprintf("%d.00", costRupiah)),
			MovementType: db.InventoryMovementType(tipe),
		})
		if err != nil {
			panic(fmt.Errorf("mutasi %s %d: %w", tipe, mills, err))
		}
	}
	mutasi(item.ID, models.MovementIn, 10_000, 10_000, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	mutasi(item.ID, models.MovementIn, 5_000, 12_000, time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC))
	mutasi(item.ID, models.MovementIn, 2_000, 8_000, time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC))
	mutasi(item.ID, models.MovementOut, 8_000, 0, time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC))
	// Item kedua tetap kosong: uji baris item tanpa mutasi tampil dengan nol.
	_ = itemLain

	// ===== 3. Angka rata-rata tertimbang via query + model yang sama =====
	asOf := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	dbItems, err := q.ListInventoryItems(ctx, company.ID)
	if err != nil {
		panic(err)
	}
	items := make([]models.InventoryItem, 0, len(dbItems))
	for _, it := range dbItems {
		items = append(items, models.InventoryItem{
			ID: it.ID, CompanyID: it.CompanyID,
			Name: it.Name, SKU: it.Sku, Unit: it.Unit,
		})
	}
	dbMovs, err := q.ListInventoryMovements(ctx, db.ListInventoryMovementsParams{
		CompanyID: company.ID, AsOf: tgl(asOf),
	})
	if err != nil {
		panic(err)
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
	calc, err := models.BuildInventory(items, moves)
	if err != nil {
		panic(err)
	}
	uang := func(cents int64) string { return models.FormatCents(cents) }

	barisItem := func(itemID int64) models.InventoryRow {
		for _, r := range calc.Rows {
			if r.ID == itemID {
				return r
			}
		}
		panic(fmt.Errorf("baris item %d tidak ada", itemID))
	}
	r := barisItem(item.ID)
	cek(r.AvgCents == rp(10_352)+94, "harga rata-rata tertimbang = %s (harus 10.352,94 setelah 3 pembelian beda harga)",
		uang(r.AvgCents))
	cek(r.QtyMills == 9_000, "saldo qty = %s rim (harus 9 setelah keluar 8)", models.FormatQty(r.QtyMills))
	cek(r.ValueCents == rp(93_176)+46, "nilai persediaan (qty x avg cost) = %s (harus 93.176,46)",
		uang(r.ValueCents))

	if len(calc.Movements) != 4 {
		panic(fmt.Errorf("jumlah mutasi %d (mau 4)", len(calc.Movements)))
	}
	m2 := calc.Movements[1]
	cek(m2.BalanceAvgCents == rp(10_666)+67, "avg setelah pembelian ke-2 (5 @ 12.000) = %s (harus 10.666,67)",
		uang(m2.BalanceAvgCents))
	m3 := calc.Movements[2]
	cek(m3.BalanceAvgCents == rp(10_352)+94, "avg setelah pembelian ke-3 (2 @ 8.000) = %s (harus 10.352,94)",
		uang(m3.BalanceAvgCents))
	m4 := calc.Movements[3]
	cek(m4.MovementType == models.MovementOut && m4.AppliedCostCents == rp(10_352)+94,
		"penjualan dinilai dengan harga rata-rata saat itu = %s (harus 10.352,94)",
		uang(m4.AppliedCostCents))
	cek(m4.ValueCents == rp(82_823)+52, "COGS penjualan 8 rim = %s (harus 82.823,52)",
		uang(m4.ValueCents))
	cek(m4.BalanceQtyMills == 9_000 && m4.BalanceAvgCents == rp(10_352)+94,
		"saldo setelah penjualan: qty %s, avg %s (avg tidak berubah oleh pengeluaran)",
		models.FormatQty(m4.BalanceQtyMills), uang(m4.BalanceAvgCents))

	// ===== 4. Batas: saldo kurang, FK komposit, CHECK jumlah =====
	err = models.ValidateMovement(items, moves, models.InventoryMovement{
		CompanyID: company.ID, ItemID: item.ID,
		Date:     time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC),
		QtyMills: 10_000, MovementType: models.MovementOut,
	})
	cek(err != nil && strings.Contains(err.Error(), models.ErrInsufficientStock.Error()),
		"mutasi keluar melebihi saldo ditolak: %v", err)

	tetangga, err := q.CreateCompany(ctx, db.CreateCompanyParams{
		Name: "PT Tetangga Bukti", Slug: "pt-tetangga-bukti",
		BaseCurrency: "IDR", UrlKey: "bukti4tetangga",
	})
	if err != nil {
		panic(err)
	}
	var itemTetangga int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO inventory_items (company_id, name) VALUES ($1, 'Item Tetangga') RETURNING id`,
		tetangga.ID).Scan(&itemTetangga); err != nil {
		panic(err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO inventory_movements (company_id, item_id, date, quantity, movement_type)
		VALUES ($1, $2, DATE '2026-10-01', 1, 'in')`, company.ID, itemTetangga)
	cek(err != nil, "FK komposit (item_id, company_id) menolak item perusahaan lain: %v", err)

	_, err = pool.Exec(ctx, `
		INSERT INTO inventory_movements (company_id, item_id, date, quantity, movement_type)
		VALUES ($1, $2, DATE '2026-10-01', 0, 'in')`, company.ID, item.ID)
	cek(err != nil, "CHECK quantity > 0 menolak jumlah nol: %v", err)

	_, err = pool.Exec(ctx, `
		INSERT INTO inventory_movements (company_id, item_id, date, quantity, movement_type)
		VALUES ($1, $2, DATE '2026-10-01', 1, 'x')`, company.ID, item.ID)
	cek(err != nil, "enum inventory_movement_type menolak tipe 'x': %v", err)

	// ===== 5. Jurnal manual akun persediaan untuk pembanding Neraca =====
	_, err = q.CreateJournalWithEntries(ctx, db.CreateJournalWithEntriesParams{
		CompanyID: company.ID, JournalDate: tgl(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)),
		Reference: "INV-001", Memo: "Pembelian persediaan (manual)", CreatedBy: userID,
		AccountIds:   []int64{akunID["1-3000"], akunID["1-1000"]},
		Descriptions: []string{"Persediaan", "Kas"},
		Debits:       []pgtype.Numeric{num("100000.00"), num("0")},
		Credits:      []pgtype.Numeric{num("0"), num("100000.00")},
	})
	if err != nil {
		panic(err)
	}

	// ===== 6. Render route dengan middleware asli + angka di HTML =====
	h := handlers.New(q, false, "")
	router := chi.NewRouter()
	router.Group(func(r chi.Router) {
		r.Use(appmw.RequireAuth(q, false))
		r.Route("/companies/{companyKey}", func(r chi.Router) {
			r.Use(appmw.RequireCompany(q))
			r.Get("/inventory", h.InventoryGet)
		})
	})

	token := "token-bukti-persediaan-4b"
	if _, err := q.CreateSession(ctx, db.CreateSessionParams{
		UserID:    userID,
		TokenHash: auth.HashToken(token),
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true, InfinityModifier: pgtype.Finite},
	}); err != nil {
		panic(err)
	}
	req := httptest.NewRequest("GET", "/companies/"+company.UrlKey+"/inventory?to=15/10/2026", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		panic(fmt.Sprintf("GET /inventory -> HTTP %d", w.Code))
	}
	html := w.Body.String()
	cek(strings.Contains(html, "Daftar Persediaan") && strings.Contains(html, "15 Oktober 2026"),
		"GET /companies/{companyKey}/inventory render (HTTP 200, judul tabel + tanggal laporan ada)")

	// Kolom tabel dimulai setelah kartu Daftar Persediaan supaya opsi form
	// (yang juga memuat nama item) tidak salah dibaca sebagai baris tabel.
	awal := strings.Index(html, "Daftar Persediaan")
	if awal < 0 {
		panic("kartu Daftar Persediaan tidak ada di HTML")
	}
	htmlTabel := html[awal:]

	cekBaris := func(needle string, want ...string) {
		got := rowNumCells(htmlTabel, needle)
		if len(got) != len(want) {
			panic(fmt.Sprintf("baris %q harus %d sel angka, dapat %d: %v", needle, len(want), len(got), got))
		}
		for i := range want {
			if got[i] != want[i] {
				panic(fmt.Sprintf("baris %q sel %d = %q, mau %q (semua: %v)", needle, i, got[i], want[i], got))
			}
		}
		fmt.Println("PASS baris HTML", needle, "=", strings.Join(got, " | "))
	}
	// Daftar item: SKU, saldo qty, harga rata-rata, nilai persediaan.
	cekBaris("SKU-A4", "SKU-A4", "9", "10.352,94", "93.176,46")
	cekBaris("Jumlah Nilai Persediaan", "93.176,46")
	// Mutasi penjualan: tanggal (FormatDateID, sama dengan label tanggal
	// laporan), jumlah, harga saat itu, COGS, saldo, avg.
	cekBaris("Keluar", "15 Oktober 2026", "8", "10.352,94", "82.823,52", "9", "10.352,94")
	// Rekomendasi Neraca: register vs buku besar (jurnal manual 100.000,00).
	cekBaris("Nilai persediaan (aset lancar di Neraca)",
		"93.176,46", "100.000,00", "-6.823,54")

	// Simpan HTML bila diminta, sebagai bukti berkas.
	if dir := os.Getenv("SMOKE_SAVE_DIR"); dir != "" {
		if err := os.WriteFile(filepath.Join(dir, "bukti-persediaan.html"), []byte(html), 0o644); err != nil {
			panic(err)
		}
		fmt.Println("PASS HTML bukti persediaan disimpan di", dir)
	}
}

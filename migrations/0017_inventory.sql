-- 0017_inventory.sql: persediaan (PSAK 202, Stage 4b).
-- Item persediaan + mutasi stok multi-tenant. Biaya = rata-rata tertimbang
-- (weighted average): unit_cost rata-rata dihitung ulang setelah tiap
-- pembelian (movement 'in'); barang keluar (movement 'out') dinilai dengan
-- unit_cost rata-rata saat itu sebagai harga pokok (COGS). Perhitungan
-- dilakukan modul (internal/models) sampai tanggal laporan, sama seperti
-- register aset tetap. Jurnal pembelian, penjualan, dan HPP tetap dibuat
-- manual oleh user lewat akun persediaan di COA; modul ini hanya
-- menyediakan angka rekomendasi untuk Neraca.

CREATE TYPE inventory_movement_type AS ENUM ('in', 'out');

CREATE TABLE inventory_items (
    id         BIGSERIAL,
    company_id BIGINT NOT NULL REFERENCES companies (id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    sku        TEXT NOT NULL DEFAULT '',
    unit       TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id, company_id),
    CONSTRAINT chk_inventory_items_name_not_empty CHECK (btrim(name) <> '')
);

-- SKU bersifat opsional; unik hanya bila diisi (pola parties.code dan
-- fixed_assets.code). PK komposit (id, company_id) + FK company_id menjaga
-- item tetap satu tenant.
CREATE UNIQUE INDEX idx_inventory_items_company_sku ON inventory_items (company_id, sku) WHERE sku <> '';
CREATE INDEX idx_inventory_items_company ON inventory_items (company_id);

CREATE TABLE inventory_movements (
    id            BIGSERIAL,
    company_id    BIGINT NOT NULL REFERENCES companies (id) ON DELETE CASCADE,
    item_id       BIGINT NOT NULL,
    date          DATE NOT NULL,
    quantity      NUMERIC(20, 3) NOT NULL,
    unit_cost     NUMERIC(20, 2) NOT NULL DEFAULT 0,
    movement_type inventory_movement_type NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id, company_id),
    -- FK komposit multi-tenant (child_id, company_id), pola ledger_entries
    -- dan journals.party_id: item perusahaan lain tidak bisa dipakai di
    -- mutasi perusahaan ini.
    FOREIGN KEY (item_id, company_id) REFERENCES inventory_items (id, company_id),
    CONSTRAINT chk_inventory_movements_qty_positive CHECK (quantity > 0),
    CONSTRAINT chk_inventory_movements_cost_nonneg CHECK (unit_cost >= 0)
);

-- Urutan pemrosesan rata-rata tertimbang: tanggal lalu id masuk, jadi index
-- ini juga menjadi index urutan baca mutasi per item.
CREATE INDEX idx_inventory_movements_item ON inventory_movements (company_id, item_id, date, id);

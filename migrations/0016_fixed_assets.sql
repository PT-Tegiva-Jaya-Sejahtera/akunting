-- 0016_fixed_assets.sql: register aset tetap (PSAK 216, Stage 4a).
-- Register aset tetap multi-tenant: satu baris per aset, depresiasi dan nilai
-- buku dihitung oleh modul (internal/models) sampai tanggal laporan. Jurnal
-- depresiasi tetap dibuat manual oleh user lewat akun aset tetap +
-- akumulasi penyusutan di COA, modul ini hanya menyediakan kalkulasi
-- rekomendasi.

CREATE TYPE fixed_asset_method AS ENUM ('straight_line', 'declining_balance');

CREATE TABLE fixed_assets (
    id                BIGSERIAL,
    company_id        BIGINT NOT NULL REFERENCES companies (id) ON DELETE CASCADE,
    name              TEXT NOT NULL,
    code              TEXT NOT NULL DEFAULT '',
    cost              NUMERIC(20, 2) NOT NULL,
    acquisition_date  DATE NOT NULL,
    useful_life_years INT NOT NULL,
    residual_value    NUMERIC(20, 2) NOT NULL DEFAULT 0,
    method            fixed_asset_method NOT NULL DEFAULT 'straight_line',
    is_active         BOOLEAN NOT NULL DEFAULT TRUE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id, company_id),
    CONSTRAINT chk_fixed_assets_cost_positive CHECK (cost > 0),
    CONSTRAINT chk_fixed_assets_life_positive CHECK (useful_life_years > 0),
    CONSTRAINT chk_fixed_assets_residual_range CHECK (residual_value >= 0 AND residual_value <= cost)
);

-- Kode aset bersifat opsional; unik hanya bila diisi (pola yang sama dengan
-- parties.code). FK company_id + PRIMARY KEY (id, company_id) menjaga aset
-- tetap satu tenant, mengikuti pola parties dan ledger_entries.
CREATE UNIQUE INDEX idx_fixed_assets_company_code ON fixed_assets (company_id, code) WHERE code <> '';
CREATE INDEX idx_fixed_assets_company ON fixed_assets (company_id);

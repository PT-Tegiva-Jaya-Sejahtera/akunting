-- 0006_psak201_periods.sql
-- Fondasi pelaporan PSAK 201 (Penyajian Laporan Keuangan):
--   1) identitas laporan keuangan pada entitas (par. 49-53)
--   2) periode akuntansi / tahun buku + kunci periode (par. 36-37, 45)
--   3) jenis jurnal: normal, saldo awal (opening), dan jurnal penutup (closing)
--   4) peran sistem pada akun, termasuk akun Saldo Laba
--   5) penjaga: jurnal tidak boleh masuk periode yang sudah ditutup

-- ============================================================
-- 1. Identitas pelaporan (par. 49-53 dan 138)
-- ============================================================
ALTER TABLE companies ADD COLUMN address TEXT NOT NULL DEFAULT '';
ALTER TABLE companies ADD COLUMN npwp TEXT NOT NULL DEFAULT '';
-- Satuan penyajian angka: penuh | ribuan | jutaan (par. 53 huruf d).
ALTER TABLE companies ADD COLUMN rounding_unit TEXT NOT NULL DEFAULT 'penuh';
-- Catatan jenis laporan: mis. "entitas tersendiri" (par. 51 huruf b).
ALTER TABLE companies ADD COLUMN entity_note TEXT NOT NULL DEFAULT '';

-- ============================================================
-- 2. Periode akuntansi / tahun buku
-- ============================================================
CREATE TYPE period_status AS ENUM ('open', 'closed');

CREATE TABLE fiscal_periods (
    id         BIGSERIAL,
    company_id BIGINT NOT NULL REFERENCES companies (id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    start_date DATE NOT NULL,
    end_date   DATE NOT NULL,
    status     period_status NOT NULL DEFAULT 'open',
    -- Periode pertama entitas: tempat saldo awal dimasukkan (par. 40A-40D).
    is_first   BOOLEAN NOT NULL DEFAULT FALSE,
    closed_at  TIMESTAMPTZ,
    closed_by  BIGINT REFERENCES users (id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id, company_id),
    UNIQUE (company_id, start_date, end_date),
    CONSTRAINT ck_fiscal_period_range CHECK (end_date > start_date)
);

-- Hanya satu periode pertama per perusahaan.
CREATE UNIQUE INDEX ux_fiscal_periods_first ON fiscal_periods (company_id) WHERE is_first;

-- ============================================================
-- 3. Jenis jurnal: normal | opening (saldo awal) | closing (penutup)
-- ============================================================
CREATE TYPE journal_kind AS ENUM ('normal', 'opening', 'closing');

ALTER TABLE journals ADD COLUMN kind journal_kind NOT NULL DEFAULT 'normal';
ALTER TABLE journals ADD COLUMN period_id BIGINT;
ALTER TABLE journals
    ADD CONSTRAINT fk_journals_period FOREIGN KEY (period_id, company_id)
    REFERENCES fiscal_periods (id, company_id) ON DELETE SET NULL;

-- Saldo awal: maksimum satu jurnal per perusahaan.
CREATE UNIQUE INDEX ux_journals_opening ON journals (company_id) WHERE kind = 'opening';
-- Jurnal penutup: maksimum satu per periode.
CREATE UNIQUE INDEX ux_journals_closing ON journals (company_id, period_id) WHERE kind = 'closing';

-- ============================================================
-- 4. Peran sistem pada akun (mis. akun Saldo Laba)
-- ============================================================
ALTER TABLE accounts ADD COLUMN system_role TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX ux_accounts_system_role
    ON accounts (company_id, system_role) WHERE system_role <> '';

-- ============================================================
-- 5. Penjaga periode pada jurnal
--    Bila perusahaan sudah punya periode akuntansi:
--      a) tanggal jurnal wajib berada di dalam salah satu periode;
--      b) periode tersebut wajib masih terbuka.
--    Perusahaan yang belum punya periode dibiarkan seperti sebelumnya.
-- ============================================================
CREATE FUNCTION check_journal_period() RETURNS trigger AS $$
DECLARE
    v_status period_status;
    v_count  INTEGER;
BEGIN
    SELECT COUNT(*) INTO v_count
      FROM fiscal_periods
     WHERE company_id = NEW.company_id;
    IF v_count = 0 THEN
        RETURN NEW;
    END IF;

    SELECT fp.status INTO v_status
      FROM fiscal_periods fp
     WHERE fp.company_id = NEW.company_id
       AND NEW.journal_date BETWEEN fp.start_date AND fp.end_date;

    IF v_status IS NULL THEN
        RAISE EXCEPTION 'tanggal % tidak berada di periode akuntansi mana pun', NEW.journal_date;
    END IF;
    IF v_status = 'closed' THEN
        RAISE EXCEPTION 'periode untuk tanggal % sudah ditutup', NEW.journal_date;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_journal_period
    BEFORE INSERT OR UPDATE OF journal_date, company_id ON journals
    FOR EACH ROW EXECUTE FUNCTION check_journal_period();

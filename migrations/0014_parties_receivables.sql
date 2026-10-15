-- 0014_parties_receivables.sql: subledger piutang usaha (Stage 3a).
-- 1. parties: pelanggan/supplier dalam satu tabel, dibedakan party_type.
-- 2. accounts.is_receivable: penanda akun piutang usaha, tanpa hardcode id.
-- 3. Journals diberi party_id + due_date (atribut dokumen: satu invoice =
--    satu pihak, satu tanggal jatuh tempo) untuk dasar aging.

CREATE TYPE party_type AS ENUM ('customer', 'supplier');

CREATE TABLE parties (
    id         BIGSERIAL,
    company_id BIGINT NOT NULL REFERENCES companies (id) ON DELETE CASCADE,
    party_type party_type NOT NULL,
    name       TEXT NOT NULL,
    code       TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id, company_id)
);

-- Kode pihak bersifat opsional; unik hanya bila diisi.
CREATE UNIQUE INDEX idx_parties_company_code ON parties (company_id, code) WHERE code <> '';
CREATE INDEX idx_parties_company_type ON parties (company_id, party_type);

-- Penanda akun piutang usaha. Subledger hanya menghitung akun bertanda ini,
-- jadi pemetaan piutang bisa diatur per akun dari data accounts.
ALTER TABLE accounts ADD COLUMN is_receivable BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE accounts SET is_receivable = TRUE
WHERE account_type = 'asset' AND psak_line = 'piutang';

-- party_id dan due_date ditaruh di journals (bukan ledger_entries): keduanya
-- atribut dokumen jurnal (invoice/pembayaran), satu pihak dan satu jatuh tempo
-- per dokumen. Nullable karena jurnal umum tanpa konteks piutang tidak wajib
-- mengisinya. FK komposit (party_id, company_id) menjaga pihak tetap satu
-- tenant dengan jurnalnya, mengikuti pola FK komposit di ledger_entries.
ALTER TABLE journals ADD COLUMN party_id BIGINT;
ALTER TABLE journals ADD COLUMN due_date DATE;
ALTER TABLE journals ADD CONSTRAINT fk_journals_party
    FOREIGN KEY (party_id, company_id) REFERENCES parties (id, company_id);
CREATE INDEX idx_journals_party ON journals (party_id, company_id);

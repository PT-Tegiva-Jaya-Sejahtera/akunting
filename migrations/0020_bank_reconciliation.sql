-- 0020_bank_reconciliation.sql: rekonsiliasi kas/bank (Stage B).
-- bank_accounts mendefinisikan rekening bank perusahaan dan menunjuk akun COA
-- (kas di bank) yang saldonya direkonsiliasi. bank_statement_lines menyimpan
-- mutasi bank hasil impor CSV (pembacaan berkas memakai modul internal/exchange).
-- Rekonsiliasi mencocokkan satu baris mutasi dengan satu baris ledger_entries
-- pada akun COA tersebut; bila jumlahnya berbeda (mis. selisih kurs atau biaya
-- bank), system membuat jurnal penyesuaian otomatis (referensi "REC-<id>")
-- yang menulis selisih itu ke ledger double-entry sehingga saldo buku
-- akhirnya sama dengan mutasi bank. Semua tabel multi-tenant (company_id +
-- FK komposit, pola ledger_entries).

CREATE TYPE statement_line_status AS ENUM ('unmatched', 'matched', 'adjusted');

CREATE TABLE bank_accounts (
    id             BIGSERIAL,
    company_id     BIGINT NOT NULL REFERENCES companies (id) ON DELETE CASCADE,
    account_id     BIGINT NOT NULL,
    bank_name      TEXT NOT NULL DEFAULT '',
    account_number TEXT NOT NULL DEFAULT '',
    -- Mata uang rekening; kosong = mata uang dasar perusahaan (companies.base_currency).
    currency       CHAR(3) NOT NULL DEFAULT '',
    is_active      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id, company_id),
    FOREIGN KEY (account_id, company_id) REFERENCES accounts (id, company_id),
    -- Satu akun COA hanya direkonsiliasi oleh satu definisi rekening bank.
    UNIQUE (company_id, account_id),
    CONSTRAINT chk_bank_accounts_currency CHECK (currency = '' OR currency ~ '^[A-Z]{3}$')
);
CREATE INDEX idx_bank_accounts_company ON bank_accounts (company_id);

CREATE TABLE bank_statement_lines (
    id                 BIGSERIAL,
    company_id         BIGINT NOT NULL REFERENCES companies (id) ON DELETE CASCADE,
    bank_account_id    BIGINT NOT NULL,
    line_date          DATE NOT NULL,
    description        TEXT NOT NULL DEFAULT '',
    reference          TEXT NOT NULL DEFAULT '',
    amount_in          NUMERIC(20, 2) NOT NULL DEFAULT 0,
    amount_out         NUMERIC(20, 2) NOT NULL DEFAULT 0,
    balance            NUMERIC(20, 2),          -- saldo mutasi, opsional (informasi)
    status             statement_line_status NOT NULL DEFAULT 'unmatched',
    matched_entry_id   BIGINT,                  -- baris ledger yang dicocokkan
    matched_journal_id BIGINT,                  -- jurnal selisih kurs bila status = adjusted
    matched_at         TIMESTAMPTZ,
    source_file        TEXT NOT NULL DEFAULT '',-- nama berkas CSV asal baris
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id, company_id),
    FOREIGN KEY (bank_account_id, company_id) REFERENCES bank_accounts (id, company_id),
    FOREIGN KEY (matched_entry_id, company_id) REFERENCES ledger_entries (id, company_id),
    FOREIGN KEY (matched_journal_id, company_id) REFERENCES journals (id, company_id),
    -- Satu baris mutasi = masuk ATAU keluar, tidak boleh keduanya/nol.
    CONSTRAINT ck_bank_stmt_one_side CHECK (
        (amount_in > 0 AND amount_out = 0) OR (amount_out > 0 AND amount_in = 0)
    ),
    -- Status adjusted selalu punya jurnal selisih; matched tidak boleh punya.
    CONSTRAINT chk_bank_stmt_adjusted CHECK (
        (status = 'adjusted') = (matched_journal_id IS NOT NULL)
    )
);

-- Baris ledger yang sudah dicocokkan tidak boleh dicocokkan ulang ke baris
-- mutasi lain (satu ledger entry dipakai paling satu baris mutasi).
CREATE UNIQUE INDEX idx_bank_stmt_matched_entry
    ON bank_statement_lines (company_id, matched_entry_id)
    WHERE matched_entry_id IS NOT NULL;
CREATE INDEX idx_bank_stmt_lines_account
    ON bank_statement_lines (company_id, bank_account_id, line_date, id);

-- 0001_init.sql: skema inti multi-tenant + double-entry ledger.
-- Semua tabel transaksi wajib punya company_id (aturan multi-tenancy).

CREATE TYPE company_role AS ENUM ('admin', 'accountant', 'read_only');
CREATE TYPE account_type AS ENUM ('asset', 'liability', 'equity', 'revenue', 'expense');
CREATE TYPE normal_balance AS ENUM ('debit', 'credit');
CREATE TYPE journal_status AS ENUM ('draft', 'posted', 'void');

CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    full_name     TEXT NOT NULL DEFAULT '',
    is_active     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE companies (
    id            BIGSERIAL PRIMARY KEY,
    name          TEXT NOT NULL,
    slug          TEXT NOT NULL UNIQUE,
    base_currency CHAR(3) NOT NULL DEFAULT 'IDR',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- RBAC: role per perusahaan, bukan global.
CREATE TABLE company_users (
    company_id BIGINT NOT NULL REFERENCES companies (id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role       company_role NOT NULL DEFAULT 'read_only',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (company_id, user_id)
);

-- Session cookie: simpan hash token, bukan token mentah.
CREATE TABLE sessions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash   TEXT NOT NULL UNIQUE,
    expires_at   TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_sessions_user_id ON sessions (user_id);
CREATE INDEX idx_sessions_expires_at ON sessions (expires_at);

-- Chart of Accounts.
CREATE TABLE accounts (
    id             BIGSERIAL,
    company_id     BIGINT NOT NULL REFERENCES companies (id) ON DELETE CASCADE,
    code           TEXT NOT NULL,
    name           TEXT NOT NULL,
    account_type   account_type NOT NULL,
    normal_balance normal_balance NOT NULL,
    is_active      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id, company_id),
    UNIQUE (company_id, code)
);

-- Header transaksi jurnal.
CREATE TABLE journals (
    id           BIGSERIAL,
    company_id   BIGINT NOT NULL REFERENCES companies (id) ON DELETE CASCADE,
    journal_date DATE NOT NULL,
    reference    TEXT NOT NULL,
    memo         TEXT NOT NULL DEFAULT '',
    status       journal_status NOT NULL DEFAULT 'posted',
    created_by   BIGINT NOT NULL REFERENCES users (id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id, company_id),
    UNIQUE (company_id, reference)
);

-- Line items double-entry. FK komposit (account_id, company_id) dan
-- (journal_id, company_id) memaksa seluruh baris se-tenant dengan induknya.
CREATE TABLE ledger_entries (
    id          BIGSERIAL,
    company_id  BIGINT NOT NULL REFERENCES companies (id) ON DELETE CASCADE,
    journal_id  BIGINT NOT NULL,
    account_id  BIGINT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    debit       NUMERIC(20, 2) NOT NULL DEFAULT 0,
    credit      NUMERIC(20, 2) NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id, company_id),
    FOREIGN KEY (journal_id, company_id) REFERENCES journals (id, company_id) ON DELETE CASCADE,
    FOREIGN KEY (account_id, company_id) REFERENCES accounts (id, company_id),
    -- Satu baris = debit ATAU credit, tidak boleh keduanya/nol.
    CONSTRAINT ck_ledger_one_side CHECK (
        (debit > 0 AND credit = 0) OR (credit > 0 AND debit = 0)
    )
);
CREATE INDEX idx_ledger_entries_journal ON ledger_entries (journal_id, company_id);
CREATE INDEX idx_ledger_entries_account ON ledger_entries (account_id, company_id);

-- Baris pengaman terakhir di level DB: Debits = Credits per jurnal.
CREATE FUNCTION check_journal_balanced() RETURNS trigger AS $$
DECLARE
    v_journal_id BIGINT;
    v_company_id BIGINT;
    v_debit      NUMERIC;
    v_credit     NUMERIC;
BEGIN
    IF TG_OP = 'DELETE' THEN
        v_journal_id := OLD.journal_id;
        v_company_id := OLD.company_id;
    ELSE
        v_journal_id := NEW.journal_id;
        v_company_id := NEW.company_id;
    END IF;

    SELECT COALESCE(SUM(debit), 0), COALESCE(SUM(credit), 0)
    INTO v_debit, v_credit
    FROM ledger_entries
    WHERE journal_id = v_journal_id AND company_id = v_company_id;

    IF v_debit <> v_credit THEN
        RAISE EXCEPTION 'journal % unbalanced: debit % <> credit %',
            v_journal_id, v_debit, v_credit;
    END IF;

    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER trg_journal_balanced
    AFTER INSERT OR UPDATE OR DELETE ON ledger_entries
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION check_journal_balanced();

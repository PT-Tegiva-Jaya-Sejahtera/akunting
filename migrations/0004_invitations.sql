-- 0004_invitations.sql: undangan pengguna baru ke perusahaan.
-- Token disimpan terbuka agar tombol "salin tautan" bisa dipakai kapan saja.
-- Mitigasi: masa berlaku 24 jam, sekali pakai, terikat email + peran,
-- dan status pembatalan dicatat di baris yang sama (audit trail, tanpa DELETE).
-- Catatan: bila kelak diinginkan mode aman penuh, ganti kolom token menjadi
-- hash-nya saja dan tampilkan tautan satu kali saat undangan dibuat.

CREATE TYPE invitation_status AS ENUM ('pending', 'used', 'cancelled');

CREATE TABLE invitations (
    id           BIGSERIAL PRIMARY KEY,
    company_id   BIGINT NOT NULL REFERENCES companies (id) ON DELETE CASCADE,
    email        TEXT NOT NULL,
    role         company_role NOT NULL,
    token        TEXT NOT NULL UNIQUE,
    status       invitation_status NOT NULL DEFAULT 'pending',
    created_by   BIGINT NOT NULL REFERENCES users (id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ NOT NULL,
    used_at      TIMESTAMPTZ,
    used_user_id BIGINT REFERENCES users (id)
);

CREATE INDEX idx_invitations_company ON invitations (company_id, status);
CREATE INDEX idx_invitations_expires ON invitations (expires_at);

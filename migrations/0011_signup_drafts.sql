-- 0011_signup_drafts.sql
-- Simpan sementara data pendaftaran penerima undangan di server. Password
-- langsung di-hash di langkah pertama, sehingga password mentah tidak pernah
-- dikirim balik ke perambah lewat kolom tersembunyi.
-- Baris di sini sekali pakai: dihapus setelah akun terbentuk, dan dibersihkan
-- otomatis bila kedaluwarsa.

CREATE TABLE IF NOT EXISTS signup_drafts (
    id            BIGSERIAL PRIMARY KEY,
    token_hash    TEXT NOT NULL UNIQUE,
    invitation_id BIGINT NOT NULL REFERENCES invitations (id) ON DELETE CASCADE,
    full_name     TEXT NOT NULL,
    username      TEXT NOT NULL,
    email         TEXT NOT NULL DEFAULT '',
    password_hash TEXT NOT NULL,
    expires_at    TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_signup_drafts_expires ON signup_drafts (expires_at);

COMMENT ON TABLE signup_drafts IS 'Data pendaftaran sementara (password sudah di-hash) antara langkah data dan langkah PIN.';

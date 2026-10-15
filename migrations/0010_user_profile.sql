-- 0010_user_profile.sql
-- Kolom profil pengguna untuk halaman Pengaturan profil: email kontak, nomor
-- hp, dan tanggal lahir. Foto profil sengaja tidak disimpan: tampilan memakai
-- inisial dua huruf dari nama lengkap.

ALTER TABLE users ADD COLUMN IF NOT EXISTS email TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS phone TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS birth_date DATE;

COMMENT ON COLUMN users.email IS 'Email kontak (berbeda dari username yang dipakai untuk masuk).';
COMMENT ON COLUMN users.phone IS 'Nomor hp/WhatsApp, boleh kosong.';
COMMENT ON COLUMN users.birth_date IS 'Tanggal lahir, boleh kosong.';

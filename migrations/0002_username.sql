-- 0002_username.sql: identitas login diganti dari email menjadi username.
-- Alasan: aplikasi hanya diakses lewat Tailscale, cukup username + password.

ALTER TABLE users RENAME COLUMN email TO username;
ALTER TABLE users RENAME CONSTRAINT users_email_key TO users_username_key;

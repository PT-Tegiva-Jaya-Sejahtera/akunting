-- 0015_payables.sql: subledger hutang usaha (Stage 3b).
-- Penanda akun hutang usaha, pola sama dengan is_receivable di 0014: tanpa
-- hardcode id, pemetaan bisa diatur per akun dari data accounts.
-- Akun hutang usaha = akun liabilitas sub_type liabilitas_jangka_pendek
-- dengan psak_line 'utang_usaha'.
ALTER TABLE accounts ADD COLUMN is_payable BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE accounts SET is_payable = TRUE
WHERE account_type = 'liability'
  AND sub_type = 'liabilitas_jangka_pendek'
  AND psak_line = 'utang_usaha';

-- Backfill hanya jalan sekali saat migrasi; akun baru dengan psak_line
-- 'utang_usaha' tidak otomatis ditandai (temuan yang sama dengan 3a).

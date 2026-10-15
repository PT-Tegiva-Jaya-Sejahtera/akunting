-- 0012: penghitung percobaan PIN undangan yang gagal. Mencegah brute-force
-- PIN 6 digit: undangan dikunci setelah 5 percobaan gagal dan hanya bisa
-- diganti dengan undangan baru dari owner.

ALTER TABLE invitations ADD COLUMN pin_attempts INTEGER NOT NULL DEFAULT 0;

COMMENT ON COLUMN invitations.pin_attempts IS 'Jumlah percobaan PIN yang gagal; undangan dikunci setelah 5.';

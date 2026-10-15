-- 0005: aktivitas terakhir user, sub-jenis akun PSAK 1, dan penyesuaian
-- undangan (PIN verifikasi 5 menit, email diisi saat signup).

ALTER TABLE users ADD COLUMN last_activity_at TIMESTAMPTZ;

-- Sub-jenis akun untuk klasifikasi Laporan Posisi Keuangan (PSAK 1).
-- Nilai: aset_lancar, aset_tidak_lancar, liabilitas_jangka_pendek,
-- liabilitas_jangka_panjang, ekuitas, pendapatan, beban.
ALTER TABLE accounts ADD COLUMN sub_type TEXT NOT NULL DEFAULT '';
UPDATE accounts SET sub_type = CASE account_type
    WHEN 'asset'     THEN 'aset_lancar'
    WHEN 'liability' THEN 'liabilitas_jangka_pendek'
    WHEN 'equity'    THEN 'ekuitas'
    WHEN 'revenue'   THEN 'pendapatan'
    WHEN 'expense'   THEN 'beban'
END;

-- Undangan tanpa email tujuan: link dibuat otomatis + PIN verifikasi
-- yang berlaku 5 menit sejak dibuat. Email diisi penerima saat signup.
ALTER TABLE invitations ALTER COLUMN email DROP NOT NULL;
ALTER TABLE invitations ADD COLUMN pin_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE invitations ADD COLUMN pin_expires_at TIMESTAMPTZ;

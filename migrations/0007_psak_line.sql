-- 0007_psak_line.sql
-- Pemetaan akun ke pos minimum laporan (PSAK 201 par. 54 dan par. 82).
-- Kolom ini dipakai dashboard untuk memisahkan kas dan setara kas, piutang,
-- utang, modal, saldo laba, beban keuangan, dan beban pajak tanpa menebak
-- dari nama akun setiap kali halaman dibuka.
--
-- Isi awal diisi lewat pendekatan pola nama akun; akun baru boleh diatur
-- manual dari halaman Akun (menyusul pada batch berikutnya).

ALTER TABLE accounts ADD COLUMN psak_line TEXT NOT NULL DEFAULT '';

UPDATE accounts
SET psak_line = CASE
    WHEN account_type = 'revenue' THEN 'pendapatan'
    WHEN account_type = 'expense' AND name ILIKE '%bunga%' THEN 'beban_keuangan'
    WHEN account_type = 'expense' AND (name ILIKE '%pajak%' OR name ILIKE '%pph%') THEN 'beban_pajak'
    WHEN account_type = 'expense' THEN 'beban_operasional'
    WHEN account_type = 'equity' AND system_role = 'laba_ditahan' THEN 'saldo_laba'
    WHEN account_type = 'equity' THEN 'modal'
    WHEN account_type = 'asset' AND (name ILIKE '%kas%' OR name ILIKE '%bank%') THEN 'kas_setara_kas'
    WHEN account_type = 'asset' AND name ILIKE '%piutang%' THEN 'piutang'
    WHEN account_type = 'asset' AND name ILIKE '%persediaan%' THEN 'persediaan'
    WHEN account_type = 'asset' AND sub_type = 'aset_tidak_lancar' AND name ILIKE '%akumulasi%' THEN 'aset_tetap_akumulasi'
    WHEN account_type = 'asset' AND sub_type = 'aset_tidak_lancar' THEN 'aset_tetap'
    WHEN account_type = 'asset' THEN 'aset_lainnya'
    WHEN account_type = 'liability' AND name ILIKE '%bank%' THEN 'utang_bank'
    WHEN account_type = 'liability' AND (name ILIKE '%pajak%' OR name ILIKE '%ppn%' OR name ILIKE '%pph%') THEN 'pajak'
    WHEN account_type = 'liability' THEN 'utang_usaha'
    ELSE ''
END;

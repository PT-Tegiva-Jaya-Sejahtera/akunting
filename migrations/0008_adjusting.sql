-- 0008_adjusting.sql
-- Menandai jurnal penyesuaian (adjusting entries) supaya bisa dipisahkan dari
-- jurnal umum, dipakai halaman Jurnal Penyesuaian dan Kertas Kerja.

ALTER TABLE journals ADD COLUMN is_adjusting BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX idx_journals_adjusting ON journals (company_id, is_adjusting, journal_date);

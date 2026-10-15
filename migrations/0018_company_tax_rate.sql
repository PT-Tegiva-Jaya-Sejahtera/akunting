-- 0018_company_tax_rate.sql: tarif PPh badan per perusahaan (PSAK 212).
-- Tarif dipakai model untuk menghitung beban pajak kini:
-- beban pajak kini = laba sebelum pajak x tax_rate (nol bila rugi).
-- Default 0.22 = tarif PPh badan umum Indonesia (UU HPP).
ALTER TABLE companies
    ADD COLUMN tax_rate NUMERIC NOT NULL DEFAULT 0.22
    CONSTRAINT companies_tax_rate_range CHECK (tax_rate >= 0 AND tax_rate <= 1);

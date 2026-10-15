-- 0009_company_url_key.sql
-- Kunci URL perusahaan: pengganti nomor urut pada alamat /companies/<kunci>.
-- Tujuannya menambah lapisan pengaman supaya pembukuan tidak bisa ditebak
-- lewat urutan angka. Nilai lama diisi acak dari gabungan id, nama, dan waktu,
-- lalu dijaga unik oleh indeks. Kolom juga diberi nilai bawaan acak supaya
-- penyisipan baru tidak pernah gagal karena kolom ini kosong.

ALTER TABLE companies ADD COLUMN IF NOT EXISTS url_key TEXT;

-- translate menukar angka heksadesimal ke alfabet yang mudah dibaca
-- (tanpa 0, 1, i, l, o) supaya kunci tetap enak diketik dan tidak rancu.
UPDATE companies
   SET url_key = translate(
         lower(substr(md5(id::text || '|' || name || '|' || clock_timestamp()::text || '|' || random()::text), 1, 12)),
         '0123456789abcdef', 'abcdefghjkmnpqrst')
 WHERE url_key IS NULL OR url_key = '';

CREATE UNIQUE INDEX IF NOT EXISTS companies_url_key_key ON companies (url_key);

ALTER TABLE companies ALTER COLUMN url_key SET NOT NULL;

ALTER TABLE companies ALTER COLUMN url_key
  SET DEFAULT translate(
        lower(substr(md5(random()::text || clock_timestamp()::text || 'akunting'), 1, 12)),
        '0123456789abcdef', 'abcdefghjkmnpqrst');

COMMENT ON COLUMN companies.url_key IS 'Kunci acak pada URL /companies/<kunci>, menggantikan id agar tidak mudah ditebak.';

-- 0019_oci_kind.sql: penanda akun penghasilan komprehensif lain (OCI),
-- PSAK 201 par. 82A. Nilai membagi komponen OCI menjadi dua kelompok:
--   recyclable      : akan direklasifikasi ke laba rugi (mis. selisih kurs
--                     penjabaran, lindung nilai arus kas);
--   non_recyclable  : tidak akan direklasifikasi ke laba rugi (mis.
--                     pengukuran kembali imbalan pasti, surplus revaluasi).
-- Kolom nullable: akun biasa (bukan OCI) tidak diberi nilai. Akun ber-oci_kind
-- lazimnya bertipe ekuitas; mutasinya disajikan pada bagian OCI Laporan Laba
-- Rugi dan Penghasilan Komprehensif Lain, saldonya tetap bagian ekuitas Neraca.
ALTER TABLE accounts
    ADD COLUMN oci_kind TEXT
    CONSTRAINT accounts_oci_kind_values
        CHECK (oci_kind IS NULL OR oci_kind IN ('recyclable', 'non_recyclable'));

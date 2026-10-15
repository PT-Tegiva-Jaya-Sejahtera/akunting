-- 0013_pillar.sql: pilar penyusunan laporan per perusahaan.
-- 'psak201' = PSAK 201 (UMKM tapi penuh), 'sak_emkm' = SAK EMKM.
CREATE TYPE pillar AS ENUM ('psak201', 'sak_emkm');
ALTER TABLE companies ADD COLUMN pillar pillar NOT NULL DEFAULT 'psak201';

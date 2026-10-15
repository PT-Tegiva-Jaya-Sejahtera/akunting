-- 0003_roles.sql: model peran menjadi 3 level: owner, admin, staff.
-- Pemetaan data lama: admin -> owner, accountant -> admin, read_only -> staff.
-- RENAME VALUE mempertahankan seluruh baris company_users tanpa perubahan.

ALTER TYPE company_role RENAME VALUE 'admin' TO 'owner';
ALTER TYPE company_role RENAME VALUE 'accountant' TO 'admin';
ALTER TYPE company_role RENAME VALUE 'read_only' TO 'staff';

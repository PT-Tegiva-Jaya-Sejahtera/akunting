-- queries.sql: query krusial awal untuk sqlc.
-- Konvensi: parameter posisi $1..$n dipetakan sqlc dari nama @param.

-- ============================================================
-- Autentikasi user & session
-- ============================================================

-- name: CreateUser :one
INSERT INTO users (username, password_hash, full_name)
VALUES (@username, @password_hash, @full_name)
RETURNING *;

-- name: GetUserByUsername :one
SELECT * FROM users
WHERE username = @username AND is_active = TRUE;

-- name: CreateSession :one
INSERT INTO sessions (user_id, token_hash, expires_at)
VALUES (@user_id, @token_hash, @expires_at)
RETURNING *;

-- name: GetSessionWithUser :one
SELECT
    s.id            AS session_id,
    s.user_id       AS user_id,
    s.expires_at    AS expires_at,
    s.last_seen_at  AS last_seen_at,
    u.username    AS username,
    u.full_name     AS full_name
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = @token_hash
  AND s.expires_at > now()
  AND u.is_active = TRUE;

-- name: TouchSession :exec
UPDATE sessions
SET last_seen_at = now()
WHERE id = @id;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = @id;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at <= now();

-- ============================================================
-- RBAC: daftar perusahaan yang berhak diakses user
-- ============================================================

-- name: ListCompaniesForUser :many
SELECT
    c.id,
    c.name,
    c.slug,
    c.url_key,
    c.base_currency,
    c.created_at,
    cu.role::TEXT AS role
FROM companies c
JOIN company_users cu ON cu.company_id = c.id
WHERE cu.user_id = @user_id
ORDER BY c.name;

-- name: GetCompanyByID :one
SELECT * FROM companies WHERE id = @id;

-- name: GetCompanyByKey :one
SELECT * FROM companies WHERE url_key = @url_key;

-- name: CreateCompany :one
INSERT INTO companies (name, slug, base_currency, url_key)
VALUES (@name, @slug, @base_currency, @url_key)
RETURNING *;

-- name: GetCompanyBySlug :one
SELECT * FROM companies WHERE slug = @slug;

-- name: GetUserRoleInCompany :one
SELECT role::TEXT AS role
FROM company_users
WHERE user_id = @user_id AND company_id = @company_id;

-- name: GrantCompanyAccess :exec
INSERT INTO company_users (company_id, user_id, role)
VALUES (@company_id, @user_id, @role)
ON CONFLICT (company_id, user_id) DO UPDATE SET role = EXCLUDED.role;

-- name: RevokeCompanyAccess :exec
DELETE FROM company_users
WHERE company_id = @company_id AND user_id = @user_id;

-- ============================================================
-- Manajemen user dan peran per perusahaan (owner/admin/staff)
-- ============================================================

-- name: TouchUserActivity :execrows
UPDATE users
SET last_activity_at = now()
WHERE id = @id
  AND (last_activity_at IS NULL OR last_activity_at < now() - interval '1 minute');

-- name: ListCompaniesForUserWithStats :many
SELECT
    c.id,
    c.name,
    c.slug,
    c.url_key,
    c.base_currency,
    c.rounding_unit,
    cu.role::TEXT AS role,
    (SELECT COUNT(*) FROM company_users x WHERE x.company_id = c.id)::BIGINT AS user_count,
    (SELECT MAX(u.last_activity_at)::TIMESTAMPTZ
       FROM company_users x JOIN users u ON u.id = x.user_id
      WHERE x.company_id = c.id) AS last_activity,
    (SELECT COALESCE(SUM(le.debit - le.credit), 0)
       FROM ledger_entries le
       JOIN accounts a ON a.id = le.account_id AND a.company_id = le.company_id
      WHERE le.company_id = c.id AND a.sub_type LIKE 'aset%')::NUMERIC(20, 2) AS total_assets,
    -- Laba bersih periode terakhir (periode aktif terbaru, atau tahun berjalan
    -- bila perusahaan belum punya periode).
    (SELECT COALESCE(SUM(CASE
                WHEN a.account_type = 'revenue' THEN le.credit - le.debit
                WHEN a.account_type = 'expense' THEN -(le.debit - le.credit)
                ELSE 0 END), 0)
       FROM ledger_entries le
       JOIN accounts a ON a.id = le.account_id AND a.company_id = le.company_id
       JOIN journals j ON j.id = le.journal_id AND j.company_id = le.company_id
      WHERE le.company_id = c.id
        AND j.journal_date >= COALESCE((SELECT MIN(p.start_date) FROM fiscal_periods p
                                         WHERE p.company_id = c.id
                                           AND p.end_date = (SELECT MAX(p2.end_date) FROM fiscal_periods p2 WHERE p2.company_id = c.id)),
                                       date_trunc('year', CURRENT_DATE)::DATE)
        AND j.journal_date <= COALESCE((SELECT MAX(p.end_date) FROM fiscal_periods p WHERE p.company_id = c.id),
                                       CURRENT_DATE)
    )::NUMERIC(20, 2) AS net_income
FROM companies c
JOIN company_users cu ON cu.company_id = c.id
WHERE cu.user_id = @user_id
ORDER BY c.name;

-- name: ListCompanyUsers :many
SELECT
    u.id           AS user_id,
    u.username     AS username,
    u.full_name    AS full_name,
    u.last_activity_at AS last_activity_at,
    cu.role::TEXT  AS role
FROM company_users cu
JOIN users u ON u.id = cu.user_id
WHERE cu.company_id = @company_id
ORDER BY u.username;

-- name: CountCompanyOwners :one
SELECT COUNT(*)::BIGINT AS owner_count
FROM company_users
WHERE company_id = @company_id AND role = 'owner';

-- name: UpdateCompanyUserRole :exec
UPDATE company_users
SET role = @role
WHERE company_id = @company_id AND user_id = @user_id;

-- name: DeleteCompanyUser :exec
DELETE FROM company_users
WHERE company_id = @company_id AND user_id = @user_id;

-- ============================================================
-- Undangan pengguna (invite) ke perusahaan
-- ============================================================

-- name: CreateInvitation :one
INSERT INTO invitations (company_id, email, role, token, pin_hash, pin_expires_at, created_by, expires_at)
VALUES (@company_id, @email, @role, @token, @pin_hash, @pin_expires_at, @created_by, @expires_at)
RETURNING *;

-- name: ListInvitations :many
SELECT *
FROM invitations
WHERE company_id = @company_id
ORDER BY created_at DESC, id DESC;

-- name: GetInvitationByToken :one
SELECT *
FROM invitations
WHERE token = @token;

-- name: CancelInvitation :execrows
UPDATE invitations
SET status = 'cancelled'
WHERE id = @id AND company_id = @company_id AND status = 'pending';

-- name: MarkInvitationUsed :execrows
UPDATE invitations
SET status = 'used',
    used_at = now(),
    used_user_id = @used_user_id,
    email = COALESCE(NULLIF(email, ''), @email)
WHERE id = @id AND company_id = @company_id AND status = 'pending';

-- name: IncrementInvitationPinAttempts :execrows
UPDATE invitations
SET pin_attempts = pin_attempts + 1
WHERE id = @id AND status = 'pending';

-- ============================================================
-- Chart of Accounts
-- ============================================================

-- name: CreateAccount :one
-- Penanda subledger (is_receivable/is_payable) dihitung pemanggil lewat
-- models.SubledgerFlags dari psak_line/sub_type, pola yang sama dengan
-- models.GuessPsakLine untuk psak_line, supaya akun baru langsung bertanda.
INSERT INTO accounts (company_id, code, name, account_type, sub_type, normal_balance, system_role, psak_line, is_receivable, is_payable)
VALUES (@company_id, @code, @name, @account_type, @sub_type, @normal_balance, @system_role, @psak_line, @is_receivable, @is_payable)
RETURNING *;

-- name: ListAccountsAll :many
SELECT *
FROM accounts
WHERE company_id = @company_id
ORDER BY code;

-- name: UpdateAccount :exec
UPDATE accounts
SET name = @name,
    account_type = @account_type,
    sub_type = @sub_type,
    normal_balance = @normal_balance,
    system_role = @system_role,
    is_active = @is_active
WHERE id = @id AND company_id = @company_id;

-- name: ListAccounts :many
SELECT *
FROM accounts
WHERE company_id = @company_id AND is_active = TRUE
ORDER BY code;

-- ============================================================
-- Transaksi jurnal beserta ledger entries (double-entry)
-- ============================================================

-- Satu statement atomik: validasi saldo -> insert header -> insert lines.
-- Arrays @account_ids/@descriptions/@debits/@credits harus sama panjang.
-- Jika total debit <> total credit, CTE "guard" kosong, jurnal tidak
-- tercipta dan query mengembalikan 0 baris (pgx.ErrNoRows di Go).
-- DEFERRABLE trigger di migrations tetap menjadi baris pengaman terakhir.

-- name: CreateJournalWithEntries :one
WITH input AS (
    SELECT
        unnest(@account_ids::BIGINT[])   AS account_id,
        unnest(@descriptions::TEXT[])    AS description,
        unnest(@debits::NUMERIC(20, 2)[])    AS debit,
        unnest(@credits::NUMERIC(20, 2)[])   AS credit
),
totals AS (
    SELECT
        COALESCE(SUM(debit), 0)  AS total_debit,
        COALESCE(SUM(credit), 0) AS total_credit,
        COUNT(*)                 AS line_count
    FROM input
),
guard AS (
    SELECT 1
    FROM totals
    WHERE total_debit = total_credit
      AND total_debit > 0
      AND line_count >= 2
),
journal AS (
    INSERT INTO journals (company_id, journal_date, reference, memo, created_by, is_adjusting, party_id, due_date)
    SELECT @company_id, @journal_date, @reference, @memo, @created_by, @is_adjusting, @party_id, @due_date
    FROM guard
    RETURNING id, company_id, journal_date, reference, memo, status, created_by, created_at, kind, period_id, is_adjusting, party_id, due_date
),
lines AS (
    INSERT INTO ledger_entries (company_id, journal_id, account_id, description, debit, credit)
    SELECT @company_id, j.id, i.account_id, i.description, i.debit, i.credit
    FROM journal j
    CROSS JOIN input i
)
SELECT * FROM journal;

-- name: ListRecentJournals :many
SELECT *
FROM journals
WHERE company_id = @company_id
ORDER BY journal_date DESC, id DESC
LIMIT @limit_count;

-- name: GetJournalByID :one
SELECT *
FROM journals
WHERE id = @id AND company_id = @company_id;

-- Ringkasan mutasi per akun pada periode tertentu (dasar: tanggal jurnal).
-- name: ListAccountTotalsByPeriod :many
SELECT
    a.id,
    a.code,
    a.name,
    a.account_type,
    a.sub_type,
    a.psak_line,
    a.normal_balance,
    COALESCE(t.total_debit, 0)::NUMERIC(20, 2)  AS total_debit,
    COALESCE(t.total_credit, 0)::NUMERIC(20, 2) AS total_credit
FROM accounts a
LEFT JOIN (
    SELECT
        le.account_id,
        le.company_id,
        SUM(le.debit)  AS total_debit,
        SUM(le.credit) AS total_credit
    FROM ledger_entries le
    JOIN journals j ON j.id = le.journal_id AND j.company_id = le.company_id
    WHERE j.journal_date >= @date_from AND j.journal_date <= @date_to
    GROUP BY le.account_id, le.company_id
) t ON t.account_id = a.id AND t.company_id = a.company_id
WHERE a.company_id = @company_id AND a.is_active = TRUE
ORDER BY a.code;

-- name: GetJournalWithEntries :many
SELECT
    le.id,
    le.journal_id,
    le.company_id,
    le.account_id,
    a.code        AS account_code,
    a.name        AS account_name,
    le.description,
    le.debit,
    le.credit
FROM ledger_entries le
JOIN accounts a ON a.id = le.account_id AND a.company_id = le.company_id
WHERE le.journal_id = @journal_id
  AND le.company_id = @company_id
ORDER BY le.id;

-- name: ListJournals :many
SELECT *
FROM journals
WHERE company_id = @company_id
  AND journal_date >= @date_from
  AND journal_date <= @date_to
ORDER BY journal_date DESC, id DESC
LIMIT @page_size OFFSET @page_offset;

-- Neraca lajur kasar: total debit & credit per akun pada periode.
-- name: ListAccountTotals :many
SELECT
    a.id,
    a.code,
    a.name,
    a.account_type,
    a.normal_balance,
    COALESCE(SUM(le.debit), 0)  AS total_debit,
    COALESCE(SUM(le.credit), 0) AS total_credit
FROM accounts a
LEFT JOIN ledger_entries le
    ON le.account_id = a.id
   AND le.company_id = a.company_id
   AND le.created_at >= @date_from
   AND le.created_at <  @date_to
WHERE a.company_id = @company_id AND a.is_active = TRUE
GROUP BY a.id
ORDER BY a.code;

-- ============================================================
-- PSAK 201: periode akuntansi, saldo awal, dan jurnal penutup
-- ============================================================

-- name: ListFiscalPeriods :many
SELECT *
FROM fiscal_periods
WHERE company_id = @company_id
ORDER BY start_date DESC, id DESC;

-- name: GetFiscalPeriod :one
SELECT *
FROM fiscal_periods
WHERE id = @id AND company_id = @company_id;

-- name: GetFiscalPeriodByDate :many
SELECT *
FROM fiscal_periods
WHERE company_id = @company_id
  AND @on_date::DATE BETWEEN start_date AND end_date
LIMIT 1;

-- name: EarliestPeriodStart :one
SELECT COALESCE(MIN(start_date), DATE '0001-01-01')::DATE AS start_date
FROM fiscal_periods
WHERE company_id = @company_id;

-- name: CreateFiscalPeriod :one
INSERT INTO fiscal_periods (company_id, name, start_date, end_date, is_first)
VALUES (@company_id, @name, @start_date, @end_date, @is_first)
RETURNING *;

-- name: CloseFiscalPeriod :execrows
UPDATE fiscal_periods
SET status = 'closed', closed_at = now(), closed_by = @closed_by
WHERE id = @id AND company_id = @company_id AND status = 'open';

-- name: ReopenFiscalPeriod :execrows
UPDATE fiscal_periods
SET status = 'open', closed_at = NULL, closed_by = NULL
WHERE id = @id AND company_id = @company_id AND status = 'closed';

-- name: GetAccountBySystemRole :one
SELECT *
FROM accounts
WHERE company_id = @company_id AND system_role = @system_role;

-- name: GetSystemJournals :many
SELECT *
FROM journals
WHERE company_id = @company_id AND kind = @kind
ORDER BY id;

-- name: DeleteSystemJournals :execrows
DELETE FROM journals
WHERE company_id = @company_id AND kind = @kind;

-- name: DeleteClosingJournal :execrows
DELETE FROM journals
WHERE company_id = @company_id AND kind = 'closing' AND period_id = @period_id;

-- name: CreateSystemJournalWithEntries :one
WITH input AS (
    SELECT
        unnest(@account_ids::BIGINT[])       AS account_id,
        unnest(@descriptions::TEXT[])        AS description,
        unnest(@debits::NUMERIC(20, 2)[])    AS debit,
        unnest(@credits::NUMERIC(20, 2)[])   AS credit
),
totals AS (
    SELECT
        COALESCE(SUM(debit), 0)  AS total_debit,
        COALESCE(SUM(credit), 0) AS total_credit,
        COUNT(*)                 AS line_count
    FROM input
),
guard AS (
    SELECT 1
    FROM totals
    WHERE total_debit = total_credit
      AND total_debit > 0
      AND line_count >= 2
),
journal AS (
    INSERT INTO journals (company_id, journal_date, reference, memo, created_by, kind, period_id)
    SELECT @company_id, @journal_date, @reference, @memo, @created_by, @kind, @period_id
    FROM guard
    RETURNING id, company_id, journal_date, reference, memo, status, created_by, created_at, kind, period_id
),
lines AS (
    INSERT INTO ledger_entries (company_id, journal_id, account_id, description, debit, credit)
    SELECT @company_id, j.id, i.account_id, i.description, i.debit, i.credit
    FROM journal j
    CROSS JOIN input i
)
SELECT * FROM journal;

-- ============================================================
-- Dashboard: seri bulanan pendapatan, beban, dan laba
-- ============================================================

-- name: MonthlyProfitLoss :many
SELECT
    to_char(date_trunc('month', j.journal_date), 'YYYY-MM')                    AS bulan,
    COALESCE(SUM(CASE WHEN a.account_type = 'revenue' THEN le.credit - le.debit ELSE 0 END), 0)::NUMERIC(20, 2) AS pendapatan,
    COALESCE(SUM(CASE WHEN a.account_type = 'expense' THEN le.debit - le.credit ELSE 0 END), 0)::NUMERIC(20, 2) AS beban,
    COUNT(DISTINCT j.id)::BIGINT                                             AS jumlah_jurnal
FROM journals j
JOIN ledger_entries le ON le.journal_id = j.id AND le.company_id = j.company_id
JOIN accounts a ON a.id = le.account_id AND a.company_id = le.company_id
WHERE j.company_id = @company_id
  AND j.journal_date >= @date_from
  AND j.journal_date <= @date_to
GROUP BY 1
ORDER BY 1;

-- name: CountJournals :one
SELECT COUNT(*)::BIGINT AS jumlah
FROM journals
WHERE company_id = @company_id;

-- ============================================================
-- Buku besar, jurnal penyesuaian, dan kertas kerja
-- ============================================================

-- Mutasi per akun hanya dari jurnal penyesuaian (kolom penyesuaian kertas kerja).
-- name: ListAccountTotalsByPeriodAdjusting :many
SELECT
    a.id,
    a.code,
    a.name,
    a.account_type,
    a.sub_type,
    a.psak_line,
    a.normal_balance,
    COALESCE(t.total_debit, 0)::NUMERIC(20, 2)  AS total_debit,
    COALESCE(t.total_credit, 0)::NUMERIC(20, 2) AS total_credit
FROM accounts a
LEFT JOIN (
    SELECT
        le.account_id,
        le.company_id,
        SUM(le.debit)  AS total_debit,
        SUM(le.credit) AS total_credit
    FROM ledger_entries le
    JOIN journals j ON j.id = le.journal_id AND j.company_id = le.company_id
    WHERE j.journal_date >= @date_from
      AND j.journal_date <= @date_to
      AND j.is_adjusting = TRUE
    GROUP BY le.account_id, le.company_id
) t ON t.account_id = a.id AND t.company_id = a.company_id
WHERE a.company_id = @company_id AND a.is_active = TRUE
ORDER BY a.code;

-- Neraca saldo sebelum penyesuaian: mutasi periode tanpa jurnal penyesuaian.
-- name: ListAccountTotalsByPeriodPlain :many
SELECT
    a.id,
    a.code,
    a.name,
    a.account_type,
    a.sub_type,
    a.psak_line,
    a.normal_balance,
    COALESCE(t.total_debit, 0)::NUMERIC(20, 2)  AS total_debit,
    COALESCE(t.total_credit, 0)::NUMERIC(20, 2) AS total_credit
FROM accounts a
LEFT JOIN (
    SELECT
        le.account_id,
        le.company_id,
        SUM(le.debit)  AS total_debit,
        SUM(le.credit) AS total_credit
    FROM ledger_entries le
    JOIN journals j ON j.id = le.journal_id AND j.company_id = le.company_id
    WHERE j.journal_date >= @date_from
      AND j.journal_date <= @date_to
      AND j.is_adjusting = FALSE
    GROUP BY le.account_id, le.company_id
) t ON t.account_id = a.id AND t.company_id = a.company_id
WHERE a.company_id = @company_id AND a.is_active = TRUE
ORDER BY a.code;

-- name: ListAdjustingJournals :many
SELECT *
FROM journals
WHERE company_id = @company_id
  AND is_adjusting = TRUE
ORDER BY journal_date DESC, id DESC
LIMIT @limit_count;

-- name: ListAccountMovements :many
SELECT
    le.id,
    j.journal_date,
    j.reference,
    j.memo,
    le.description,
    le.debit,
    le.credit
FROM ledger_entries le
JOIN journals j ON j.id = le.journal_id AND j.company_id = le.company_id
WHERE le.company_id = @company_id
  AND le.account_id = @account_id
  AND j.journal_date >= @date_from
  AND j.journal_date <= @date_to
ORDER BY j.journal_date, le.id;

-- name: UpdateCompanyProfile :exec
UPDATE companies
SET name = @name,
    address = @address,
    npwp = @npwp,
    rounding_unit = @rounding_unit,
    entity_note = @entity_note,
    updated_at = now()
WHERE id = @id;

-- ============================================================
-- Pertukaran data (export/import ke aplikasi lain)
-- ============================================================

-- name: ExportJournalLines :many
SELECT
    j.id                                                                     AS journal_id,
    j.journal_date,
    j.reference,
    j.memo,
    j.kind::TEXT                                                             AS kind,
    j.is_adjusting,
    a.code,
    a.name                                                                   AS account_name,
    le.description,
    le.debit,
    le.credit
FROM journals j
JOIN ledger_entries le ON le.journal_id = j.id AND le.company_id = j.company_id
JOIN accounts a ON a.id = le.account_id AND a.company_id = le.company_id
WHERE j.company_id = @company_id
  AND j.journal_date >= @date_from
  AND j.journal_date <= @date_to
ORDER BY j.journal_date, j.id, le.id;

-- Kunci jurnal (tanggal dan referensi) untuk mendeteksi import berganda.
-- name: ListJournalKeys :many
SELECT journal_date, reference
FROM journals
WHERE company_id = @company_id;

-- ============================================================
-- Profil pengguna
-- ============================================================

-- name: GetUserProfile :one
SELECT id, username, full_name, email, phone, birth_date, is_active, created_at, last_activity_at
FROM users
WHERE id = @id;

-- name: UpdateUserProfile :exec
UPDATE users
SET full_name  = @full_name,
    email      = @email,
    phone      = @phone,
    birth_date = @birth_date,
    updated_at = now()
WHERE id = @id;

-- ============================================================
-- Pendaftaran sementara (langkah data sebelum langkah PIN)
-- ============================================================

-- name: CreateSignupDraft :one
INSERT INTO signup_drafts (token_hash, invitation_id, full_name, username, email, password_hash, expires_at)
VALUES (@token_hash, @invitation_id, @full_name, @username, @email, @password_hash, @expires_at)
RETURNING *;

-- name: GetSignupDraft :one
SELECT * FROM signup_drafts
WHERE token_hash = @token_hash AND expires_at > now();

-- name: DeleteSignupDraft :exec
DELETE FROM signup_drafts WHERE token_hash = @token_hash;

-- name: DeleteSignupDraftsByInvitation :exec
DELETE FROM signup_drafts WHERE invitation_id = @invitation_id;

-- name: DeleteExpiredSignupDrafts :exec
DELETE FROM signup_drafts WHERE expires_at < now();

-- name: UsernameTaken :one
SELECT EXISTS (SELECT 1 FROM users WHERE lower(username) = lower(@username)) AS taken;

-- Mutasi akun pada rentang tanggal tertentu, TANPA jurnal saldo awal (kind=opening)
-- dan jurnal penutup (kind=closing). Dipakai Laporan Perubahan Ekuitas (untuk
-- transaksi dengan pemilik), Laporan Arus Kas (metode tidak langsung), dan
-- perhitungan laba periode berjalan yang belum dipengaruhi jurnal penutup.
-- name: ListAccountTotalsMovements :many
SELECT
    a.id,
    a.code,
    a.name,
    a.account_type,
    a.sub_type,
    a.psak_line,
    a.normal_balance,
    COALESCE(t.total_debit, 0)::NUMERIC(20, 2)  AS total_debit,
    COALESCE(t.total_credit, 0)::NUMERIC(20, 2) AS total_credit
FROM accounts a
LEFT JOIN (
    SELECT
        le.account_id,
        le.company_id,
        SUM(le.debit)  AS total_debit,
        SUM(le.credit) AS total_credit
    FROM ledger_entries le
    JOIN journals j ON j.id = le.journal_id AND j.company_id = le.company_id
    WHERE j.journal_date >= @date_from AND j.journal_date <= @date_to
      AND j.kind NOT IN ('opening', 'closing')
    GROUP BY le.account_id, le.company_id
) t ON t.account_id = a.id AND t.company_id = a.company_id
WHERE a.company_id = @company_id AND a.is_active = TRUE
ORDER BY a.code;

-- Mutasi akun dari jurnal penutup (kind=closing) pada rentang tanggal tertentu.
-- Dalam Laporan Perubahan Ekuitas mutasi ini adalah pemindahan laba periode ke
-- akun saldo laba, bukan transaksi dengan pemilik.
-- name: ListAccountTotalsClosing :many
SELECT
    a.id,
    a.code,
    a.name,
    a.account_type,
    a.sub_type,
    a.psak_line,
    a.normal_balance,
    COALESCE(t.total_debit, 0)::NUMERIC(20, 2)  AS total_debit,
    COALESCE(t.total_credit, 0)::NUMERIC(20, 2) AS total_credit
FROM accounts a
LEFT JOIN (
    SELECT
        le.account_id,
        le.company_id,
        SUM(le.debit)  AS total_debit,
        SUM(le.credit) AS total_credit
    FROM ledger_entries le
    JOIN journals j ON j.id = le.journal_id AND j.company_id = le.company_id
    WHERE j.journal_date >= @date_from AND j.journal_date <= @date_to
      AND j.kind = 'closing'
    GROUP BY le.account_id, le.company_id
) t ON t.account_id = a.id AND t.company_id = a.company_id
WHERE a.company_id = @company_id AND a.is_active = TRUE
ORDER BY a.code;

-- Saldo awal akun sebelum tanggal mulai laporan, termasuk jurnal saldo awal
-- (kind=opening) yang memang bertanggal awal periode pertama. Dipakai untuk
-- saldo awal Laporan Perubahan Ekuitas dan saldo awal kas pada Laporan Arus Kas.
-- name: ListAccountTotalsOpening :many
SELECT
    a.id,
    a.code,
    a.name,
    a.account_type,
    a.sub_type,
    a.psak_line,
    a.normal_balance,
    COALESCE(t.total_debit, 0)::NUMERIC(20, 2)  AS total_debit,
    COALESCE(t.total_credit, 0)::NUMERIC(20, 2) AS total_credit
FROM accounts a
LEFT JOIN (
    SELECT
        le.account_id,
        le.company_id,
        SUM(le.debit)  AS total_debit,
        SUM(le.credit) AS total_credit
    FROM ledger_entries le
    JOIN journals j ON j.id = le.journal_id AND j.company_id = le.company_id
    WHERE j.journal_date < @date_from
       OR (j.kind = 'opening' AND j.journal_date <= @date_to)
    GROUP BY le.account_id, le.company_id
) t ON t.account_id = a.id AND t.company_id = a.company_id
WHERE a.company_id = @company_id AND a.is_active = TRUE
ORDER BY a.code;

-- ============================================================
-- Pihak (pelanggan/supplier)
-- ============================================================

-- Daftar pihak perusahaan untuk dropdown form jurnal. Pelanggan dan supplier
-- berada di satu tabel, dibedakan kolom party_type.
-- name: ListParties :many
SELECT *
FROM parties
WHERE company_id = @company_id
ORDER BY party_type, name;

-- ============================================================
-- Subledger piutang usaha (Stage 3a)
-- ============================================================

-- Mutasi piutang per jurnal sampai tanggal laporan: dasar open item untuk
-- aging. Hanya akun bertanda is_receivable dan jurnal posted yang dihitung.
-- Pihak subledger ini hanya pelanggan (party_type = 'customer'): jurnal yang
-- pihaknya supplier diperlakukan sebagai jurnal tanpa pihak, supaya pelanggan
-- tidak pernah tampil di subledger hutang dan sebaliknya. Jurnal tanpa pihak
-- tetap disertakan (party_id 0) supaya jumlah subledger selalu sama dengan
-- saldo akun piutang pada buku besar.
-- name: ListReceivableMovements :many
SELECT
    j.id AS journal_id,
    COALESCE(p.id, 0) AS party_id,
    COALESCE(p.name, '') AS party_name,
    j.reference,
    j.journal_date,
    j.due_date,
    COALESCE(SUM(le.debit), 0)::NUMERIC(20, 2) AS total_debit,
    COALESCE(SUM(le.credit), 0)::NUMERIC(20, 2) AS total_credit
FROM journals j
JOIN ledger_entries le ON le.journal_id = j.id AND le.company_id = j.company_id
JOIN accounts a ON a.id = le.account_id AND a.company_id = le.company_id
LEFT JOIN parties p ON p.id = j.party_id AND p.company_id = j.company_id
    AND p.party_type = 'customer'
WHERE j.company_id = @company_id
  AND j.status = 'posted'
  AND j.journal_date <= @as_of
  AND a.is_receivable
GROUP BY j.id, p.id, p.name, j.reference, j.journal_date, j.due_date
ORDER BY j.journal_date, j.id;

-- ============================================================
-- Subledger hutang usaha (Stage 3b)
-- ============================================================

-- Mutasi hutang per jurnal sampai tanggal laporan: dasar open item untuk
-- aging. Hanya akun bertanda is_payable dan jurnal posted yang dihitung.
-- Arah mutasi dibalik dari piutang (kredit - debit): positif = tagihan
-- supplier, negatif = pembayaran. Pihak subledger ini hanya supplier
-- (party_type = 'supplier'): jurnal yang pihaknya pelanggan diperlakukan
-- sebagai jurnal tanpa pihak, dengan alasan yang sama dengan subledger
-- piutang di atas.
-- name: ListPayableMovements :many
SELECT
    j.id AS journal_id,
    COALESCE(p.id, 0) AS party_id,
    COALESCE(p.name, '') AS party_name,
    j.reference,
    j.journal_date,
    j.due_date,
    COALESCE(SUM(le.debit), 0)::NUMERIC(20, 2) AS total_debit,
    COALESCE(SUM(le.credit), 0)::NUMERIC(20, 2) AS total_credit
FROM journals j
JOIN ledger_entries le ON le.journal_id = j.id AND le.company_id = j.company_id
JOIN accounts a ON a.id = le.account_id AND a.company_id = le.company_id
LEFT JOIN parties p ON p.id = j.party_id AND p.company_id = j.company_id
    AND p.party_type = 'supplier'
WHERE j.company_id = @company_id
  AND j.status = 'posted'
  AND j.journal_date <= @as_of
  AND a.is_payable
GROUP BY j.id, p.id, p.name, j.reference, j.journal_date, j.due_date
ORDER BY j.journal_date, j.id;

-- ============================================================
-- Aset tetap (PSAK 216, Stage 4a)
-- ============================================================

-- Register aset tetap per perusahaan. Kode aset opsional (unik bila diisi),
-- sama dengan pola parties.code.
-- name: CreateFixedAsset :one
INSERT INTO fixed_assets (
    company_id, name, code, cost, acquisition_date,
    useful_life_years, residual_value, method, is_active
) VALUES (
    @company_id, @name, @code, @cost, @acquisition_date,
    @useful_life_years, @residual_value, @method, @is_active
)
RETURNING *;

-- name: ListFixedAssets :many
SELECT *
FROM fixed_assets
WHERE company_id = @company_id
ORDER BY acquisition_date, code, id;

-- name: SetFixedAssetActive :execrows
UPDATE fixed_assets
SET is_active = @is_active,
    updated_at = now()
WHERE id = @id AND company_id = @company_id;

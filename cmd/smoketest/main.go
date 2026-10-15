// Command smoketest: verifikasi runtime terhadap PostgreSQL nyata.
// Sementara; dihapus setelah lulus.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"accounting/internal/database/db"
)

func num(s string) pgtype.Numeric {
	var n pgtype.Numeric
	if err := n.Scan(s); err != nil {
		panic(err)
	}
	return n
}

func main() {
	ctx := context.Background()

	// Mandiri: buat database sementara, hapus sesudah selesai.
	adminDSN := envOr("SMOKE_ADMIN_DSN", "postgres://aiagent@/postgres?host=/var/run/postgresql")
	admin, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		panic(err)
	}
	defer admin.Close()
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS accounting_smoke"); err != nil {
		panic(err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE accounting_smoke"); err != nil {
		panic(err)
	}
	defer admin.Exec(ctx, "DROP DATABASE IF EXISTS accounting_smoke")

	dsn := envOr("SMOKE_DSN", "postgres://aiagent@/accounting_smoke?host=/var/run/postgresql")
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		panic(err)
	}
	defer pool.Close()

	// 1. Migrasi: semua file migrations/*.sql berurutan (nama file).
	entries, err := os.ReadDir("migrations")
	if err != nil {
		panic(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		sqlBytes, err := os.ReadFile("migrations/" + e.Name())
		if err != nil {
			panic(err)
		}
		if _, err := pool.Exec(ctx, string(sqlBytes)); err != nil {
			panic(fmt.Errorf("migrasi %s gagal: %w", e.Name(), err))
		}
		fmt.Println("PASS migrasi", e.Name())
	}

	q := db.New(pool)

	// 2. Seed
	var userID, companyA, companyB, acctKas, acctPendapatan int64
	err = pool.QueryRow(ctx,
		`INSERT INTO users (username, password_hash, full_name)
		 VALUES ('budi', 'x', 'Budi') RETURNING id`).Scan(&userID)
	if err != nil {
		panic(err)
	}
	err = pool.QueryRow(ctx,
		`INSERT INTO companies (name, slug) VALUES ('PT A', 'pt-a') RETURNING id`).Scan(&companyA)
	if err != nil {
		panic(err)
	}
	err = pool.QueryRow(ctx,
		`INSERT INTO companies (name, slug) VALUES ('PT B', 'pt-b') RETURNING id`).Scan(&companyB)
	if err != nil {
		panic(err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO company_users (company_id, user_id, role) VALUES ($1, $2, 'owner')`, companyA, userID); err != nil {
		panic(err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO company_users (company_id, user_id, role) VALUES ($1, $2, 'staff')`, companyB, userID); err != nil {
		panic(err)
	}
	err = pool.QueryRow(ctx,
		`INSERT INTO accounts (company_id, code, name, account_type, normal_balance)
		 VALUES ($1, '1001', 'Kas', 'asset', 'debit') RETURNING id`, companyA).Scan(&acctKas)
	if err != nil {
		panic(err)
	}
	err = pool.QueryRow(ctx,
		`INSERT INTO accounts (company_id, code, name, account_type, normal_balance)
		 VALUES ($1, '4001', 'Pendapatan', 'revenue', 'credit') RETURNING id`, companyA).Scan(&acctPendapatan)
	if err != nil {
		panic(err)
	}
	fmt.Println("PASS seed users/companies/accounts")

	// 3. RBAC query via sqlc
	companies, err := q.ListCompaniesForUser(ctx, userID)
	if err != nil {
		panic(err)
	}
	if len(companies) != 2 || companies[0].Role != "owner" && companies[1].Role != "owner" {
		panic(fmt.Errorf("ListCompaniesForUser salah: %+v", companies))
	}
	fmt.Println("PASS ListCompaniesForUser (2 perusahaan, role benar)")

	role, err := q.GetUserRoleInCompany(ctx, db.GetUserRoleInCompanyParams{UserID: userID, CompanyID: companyB})
	if err != nil || role != "staff" {
		panic(fmt.Errorf("GetUserRoleInCompany salah: role=%q err=%v", role, err))
	}
	fmt.Println("PASS GetUserRoleInCompany")

	// 4. Session via sqlc
	sess, err := q.CreateSession(ctx, db.CreateSessionParams{
		UserID:    userID,
		TokenHash: "sha256-hasil-hash-token",
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true, InfinityModifier: pgtype.Finite},
	})
	if err != nil {
		panic(err)
	}
	got, err := q.GetSessionWithUser(ctx, "sha256-hasil-hash-token")
	if err != nil || got.UserID != userID || got.Username != "budi" {
		panic(fmt.Errorf("GetSessionWithUser salah: %+v err=%v", got, err))
	}
	if err := q.DeleteSession(ctx, sess.ID); err != nil {
		panic(err)
	}
	if _, err := q.GetSessionWithUser(ctx, "sha256-hasil-hash-token"); !errors.Is(err, pgx.ErrNoRows) {
		panic(fmt.Errorf("session harusnya terhapus, err=%v", err))
	}
	fmt.Println("PASS session create/get/delete")

	// 5. Jurnal seimbang -> sukses, 2 baris ledger
	jdate := pgtype.Date{Time: time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC), Valid: true, InfinityModifier: pgtype.Finite}
	j, err := q.CreateJournalWithEntries(ctx, db.CreateJournalWithEntriesParams{
		CompanyID:    companyA,
		JournalDate:  jdate,
		Reference:    "JV-0001",
		Memo:         "Penjualan tunai",
		CreatedBy:    userID,
		AccountIds:   []int64{acctKas, acctPendapatan},
		Descriptions: []string{"Kas masuk", "Pendapatan jasa"},
		Debits:       []pgtype.Numeric{num("1000000.00"), num("0")},
		Credits:      []pgtype.Numeric{num("0"), num("1000000.00")},
	})
	if err != nil {
		panic(fmt.Errorf("jurnal seimbang gagal: %w", err))
	}
	lines, err := q.GetJournalWithEntries(ctx, db.GetJournalWithEntriesParams{JournalID: j.ID, CompanyID: companyA})
	if err != nil || len(lines) != 2 {
		panic(fmt.Errorf("GetJournalWithEntries salah: n=%d err=%v", len(lines), err))
	}
	fmt.Println("PASS CreateJournalWithEntries seimbang + 2 ledger_entries")

	// 6. Jurnal tidak seimbang -> ErrNoRows, tidak ada baris tersisa
	_, err = q.CreateJournalWithEntries(ctx, db.CreateJournalWithEntriesParams{
		CompanyID:    companyA,
		JournalDate:  jdate,
		Reference:    "JV-0002",
		Memo:         "Saldo tidak berimbang",
		CreatedBy:    userID,
		AccountIds:   []int64{acctKas, acctPendapatan},
		Descriptions: []string{"Kas", "Pendapatan"},
		Debits:       []pgtype.Numeric{num("500000.00"), num("0")},
		Credits:      []pgtype.Numeric{num("0"), num("400000.00")},
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		panic(fmt.Errorf("jurnal tidak seimbang harusnya ditolak, err=%v", err))
	}
	var nJournals int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM journals`).Scan(&nJournals); err != nil {
		panic(err)
	}
	if nJournals != 1 {
		panic(fmt.Errorf("jurnal tidak seimbang ternyata tercatat, total=%d", nJournals))
	}
	fmt.Println("PASS jurnal tidak seimbang ditolak (tidak ada baris tersisa)")

	// 7. Isolasi tenant: akun milik companyA dipakai di jurnal companyB -> FK tolak
	_, err = q.CreateJournalWithEntries(ctx, db.CreateJournalWithEntriesParams{
		CompanyID:    companyB,
		JournalDate:  jdate,
		Reference:    "JV-X",
		Memo:         "Cross-tenant",
		CreatedBy:    userID,
		AccountIds:   []int64{acctKas, acctPendapatan},
		Descriptions: []string{"a", "b"},
		Debits:       []pgtype.Numeric{num("10"), num("0")},
		Credits:      []pgtype.Numeric{num("0"), num("10")},
	})
	if err == nil {
		panic("cross-tenant account_id harusnya ditolak FK komposit")
	}
	fmt.Println("PASS isolasi tenant (FK komposit menolak cross-company)")

	// 8. Trigger DEFERRABLE: insert langsung yang tidak seimbang -> exception
	_, err = pool.Exec(ctx, `
		WITH j AS (
			INSERT INTO journals (company_id, journal_date, reference, created_by)
			VALUES ($1, DATE '2026-10-10', 'JV-T', $2) RETURNING id
		)
		INSERT INTO ledger_entries (company_id, journal_id, account_id, debit, credit)
		SELECT $1, j.id, $3, 100, 0 FROM j`, companyA, userID, acctKas)
	if err == nil {
		panic("trigger seharusnya menolak ledger_entries tanpa pasangan credit")
	}
	fmt.Println("PASS trigger check_journal_balanced menolak baris tunggal")

	fmt.Println("SEMUA SMOKE TEST LULUS")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

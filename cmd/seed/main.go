// Command seed mengisi database pengembangan dengan user, perusahaan,
// relasi role, dan chart of accounts awal.
//
// Contoh:
//
//	go run ./cmd/seed -email admin@contoh.id -password 'ganti-password' \
//	    -company 'PT Contoh Sejahtera' -slug pt-contoh
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"accounting/internal/auth"
	"accounting/internal/database/db"
	"accounting/internal/models"
)

func main() {
	username := flag.String("username", "", "username login")
	password := flag.String("password", "", "password user login")
	fullName := flag.String("name", "Admin", "nama lengkap user")
	company := flag.String("company", "PT Contoh Sejahtera", "nama perusahaan")
	slug := flag.String("slug", "", "slug perusahaan (default dari nama)")
	role := flag.String("role", "owner", "role user di perusahaan: owner|admin|staff")
	flag.Parse()

	if *username == "" || *password == "" {
		fmt.Fprintln(os.Stderr, "wajib: -username dan -password")
		flag.Usage()
		os.Exit(2)
	}
	if *slug == "" {
		*slug = slugify(*company)
	}
	if len(*password) < 8 {
		fmt.Fprintln(os.Stderr, "password minimal 8 karakter")
		os.Exit(2)
	}
	dbRole, err := parseRole(*role)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
	ctx := context.Background()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://aiagent@/accounting_dev?host=/var/run/postgresql"
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		slog.Error("gagal koneksi", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	q := db.New(pool)

	// 1. User
	user, err := q.GetUserByUsername(ctx, *username)
	if errors.Is(err, pgx.ErrNoRows) {
		hash, err := auth.HashPassword(*password)
		if err != nil {
			slog.Error("gagal hash password", "error", err)
			os.Exit(1)
		}
		user, err = q.CreateUser(ctx, db.CreateUserParams{
			Username:     *username,
			PasswordHash: hash,
			FullName:     *fullName,
		})
		if err != nil {
			slog.Error("gagal buat user", "error", err)
			os.Exit(1)
		}
		slog.Info("user dibuat", "username", user.Username)
	} else if err != nil {
		slog.Error("gagal cek user", "error", err)
		os.Exit(1)
	} else {
		slog.Info("user sudah ada, dipakai ulang", "username", user.Username)
	}

	// 2. Perusahaan + relasi role
	companyID, err := ensureCompany(ctx, q, *company, *slug, user.ID, dbRole)
	if err != nil {
		slog.Error("gagal buat perusahaan", "error", err)
		os.Exit(1)
	}

	// 3. Chart of accounts awal
	if err := seedAccounts(ctx, q, companyID); err != nil {
		slog.Error("gagal buat akun", "error", err)
		os.Exit(1)
	}

	fmt.Printf("SEED BERHASIL\n  user      : %s (id %d)\n  perusahaan: %s (id %d)\n  login     : %s / (password yang Anda isi)\n",
		user.Username, user.ID, *company, companyID, user.Username)
}

func ensureCompany(ctx context.Context, q *db.Queries, name, slug string, userID int64, role db.CompanyRole) (int64, error) {
	kunci, errK := models.NewCompanyKey()
	if errK != nil {
		return 0, fmt.Errorf("kunci perusahaan: %w", errK)
	}
	c, err := q.CreateCompany(ctx, db.CreateCompanyParams{
		Name:         name,
		Slug:         slug,
		BaseCurrency: "IDR",
		UrlKey:       kunci,
	})
	if err != nil {
		// Slug sudah dipakai: pakai perusahaan yang sudah ada.
		existing, err2 := q.GetCompanyBySlug(ctx, slug)
		if err2 != nil {
			return 0, fmt.Errorf("create company: %w", err)
		}
		c = existing
		slog.Info("perusahaan sudah ada, dipakai ulang", "slug", slug)
	}

	if err := q.GrantCompanyAccess(ctx, db.GrantCompanyAccessParams{
		CompanyID: c.ID,
		UserID:    userID,
		Role:      role,
	}); err != nil {
		return 0, err
	}
	slog.Info("perusahaan siap + role diberikan", "slug", slug, "role", string(role))
	return c.ID, nil
}

func parseRole(s string) (db.CompanyRole, error) {
	switch s {
	case "owner":
		return db.CompanyRoleOwner, nil
	case "admin":
		return db.CompanyRoleAdmin, nil
	case "staff":
		return db.CompanyRoleStaff, nil
	default:
		return "", fmt.Errorf("role tidak dikenal: %s (pilihan: owner|admin|staff)", s)
	}
}

func seedAccounts(ctx context.Context, q *db.Queries, companyID int64) error {
	existing, err := q.ListAccounts(ctx, companyID)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		slog.Info("akun sudah ada, dilewati", "jumlah", len(existing))
		return nil
	}

	akun := []struct {
		code, name, subType string
		typ                 db.AccountType
		normal              db.NormalBalance
	}{
		{"1001", "Kas", models.SubCurrentAsset, db.AccountTypeAsset, db.NormalBalanceDebit},
		{"1002", "Bank", models.SubCurrentAsset, db.AccountTypeAsset, db.NormalBalanceDebit},
		{"2001", "Utang Usaha", models.SubCurrentLiability, db.AccountTypeLiability, db.NormalBalanceCredit},
		{"3001", "Modal Pemilik", models.SubEquity, db.AccountTypeEquity, db.NormalBalanceCredit},
		{"4001", "Pendapatan Jasa", models.SubRevenue, db.AccountTypeRevenue, db.NormalBalanceCredit},
		{"5001", "Beban Operasional", models.SubExpense, db.AccountTypeExpense, db.NormalBalanceDebit},
	}
	for _, a := range akun {
		// psak_line ditebak seperti pembuatan akun dari form; penanda
		// subledger (is_receivable/is_payable) diturunkan dari psak_line +
		// sub_type supaya akun piutang/hutang bawaan seeder juga bertanda.
		psak := models.GuessPsakLine(a.name, string(a.typ), a.subType, "")
		isReceivable, isPayable := models.SubledgerFlags(string(a.typ), a.subType, psak)
		if _, err := q.CreateAccount(ctx, db.CreateAccountParams{
			CompanyID:     companyID,
			Code:          a.code,
			Name:          a.name,
			AccountType:   a.typ,
			SubType:       a.subType,
			NormalBalance: a.normal,
			PsakLine:      psak,
			IsReceivable:  isReceivable,
			IsPayable:     isPayable,
		}); err != nil {
			return err
		}
	}
	slog.Info("chart of accounts dibuat", "jumlah", len(akun))
	return nil
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "-")
	return s
}

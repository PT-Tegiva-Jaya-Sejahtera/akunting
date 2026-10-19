// Package accounting memuat aturan akuntansi murni yang dipakai bersama oleh
// handler HTTP dan perkakas baris perintah (seed/demo).
package accounting

import (
	"context"
	"errors"
	"fmt"

	"accounting/internal/database/db"
	"accounting/internal/models"
)

// Line adalah satu baris jurnal sistem (saldo awal atau jurnal penutup).
type Line struct {
	AccountID   int64
	Description string
	DebitCents  int64
	CreditCents int64
}

// ErrNoActivity dikembalikan bila periode tidak punya mutasi pendapatan/beban.
var ErrNoActivity = errors.New("tidak ada mutasi pendapatan atau beban pada periode ini")

// ErrNoClosingTarget dikembalikan bila perusahaan belum punya akun modal
// maupun Saldo Laba, jadi hasil jurnal penutup tidak punya tempat.
var ErrNoClosingTarget = errors.New("tutup buku butuh akun Modal (ekuitas) atau akun bertanda Saldo Laba")

// IkhtisarClosing menyusun baris-baris jurnal penutup dengan urutan buku
// ajar Indonesia, memakai akun perantara Ikhtisar Laba Rugi:
//
//  1. Pendapatan (D) ... / Ikhtisar Laba Rugi (K) total pendapatan
//  2. Ikhtisar Laba Rugi (D) total beban ... / Beban (K) ...
//  3. Ikhtisar Laba Rugi (D) laba ... / Modal (K); saat rugi dibalik.
//  4. Modal (D) prive ... / Prive (K), hanya bila ada prive bertanda.
//
// Semua akun nominal dinolkan pada akhir proses. mengembalikan baris jurnal
// dan laba bersih periode (sen; positif berarti laba).
func IkhtisarClosingLines(rows []db.ListAccountTotalsByPeriodRow,
	ikhtisarID, modalID, priveID int64) ([]Line, int64) {

	var lines []Line
	var totalRevenue, totalExpense int64

	// 1. Tutup seluruh akun pendapatan (debit) ke Ikhtisar Laba Rugi.
	for _, row := range rows {
		if row.ID == ikhtisarID || row.AccountType != db.AccountTypeRevenue {
			continue
		}
		amount := models.NumericToCents(row.TotalCredit) - models.NumericToCents(row.TotalDebit)
		if amount == 0 {
			continue
		}
		totalRevenue += amount
		lines = append(lines, Line{
			AccountID:   row.ID,
			Description: "Penutupan pendapatan " + row.Code,
			DebitCents:  amount,
		})
	}
	if totalRevenue > 0 {
		lines = append(lines, Line{
			AccountID:   ikhtisarID,
			Description: "Seluruh pendapatan dipindahkan ke Ikhtisar Laba Rugi",
			CreditCents: totalRevenue,
		})
	}

	// 2. Tutup seluruh akun beban: Ikhtisar Laba Rugi debet, beban kredit.
	for _, row := range rows {
		if row.AccountType != db.AccountTypeExpense {
			continue
		}
		amount := models.NumericToCents(row.TotalDebit) - models.NumericToCents(row.TotalCredit)
		if amount == 0 {
			continue
		}
		totalExpense += amount
		lines = append(lines, Line{
			AccountID:   row.ID,
			Description: "Penutupan beban " + row.Code,
			CreditCents: amount,
		})
	}
	if totalExpense > 0 {
		lines = append(lines, Line{
			AccountID:   ikhtisarID,
			Description: "Ikhtisar Laba Rugi menanggung seluruh beban",
			DebitCents:  totalExpense,
		})
	}

	net := totalRevenue - totalExpense
	if len(lines) == 0 && net == 0 {
		return nil, 0
	}

	// 3. Pindahkan hasil Ikhtisar Laba Rugi ke modal: laba dari debit
	// Ikhtisar ke kredit Modal, rugi sebaliknya.
	if net > 0 {
		lines = append(lines, Line{
			AccountID:   ikhtisarID,
			Description: "Laba periode berjalan dipindahkan ke modal",
			DebitCents:  net,
		})
		lines = append(lines, Line{
			AccountID:   modalID,
			Description: "Laba periode berjalan menambah modal",
			CreditCents: net,
		})
	} else if net < 0 {
		lines = append(lines, Line{
			AccountID:   modalID,
			Description: "Rugi periode berjalan mengurangi modal",
			DebitCents:  -net,
		})
		lines = append(lines, Line{
			AccountID:   ikhtisarID,
			Description: "Rugi periode berjalan dipindahkan dari modal",
			CreditCents: -net,
		})
	}

	// 4. Tutup prive: Modal debet, Prive kredit, hanya bila prive punya saldo
	// debit pada periode itu.
	if priveID != 0 && modalID != priveID {
		prive := priveBalanceDebit(rows, priveID)
		if prive > 0 {
			lines = append(lines, Line{
				AccountID:   modalID,
				Description: "Pengambilan pribadi pemilik mengurangi modal",
				DebitCents:  prive,
			})
			lines = append(lines, Line{
				AccountID:   priveID,
				Description: "Prive dipindahkan ke modal",
				CreditCents: prive,
			})
		}
	}
	return lines, net
}

// priveBalanceDebit membaca saldo debit akun prive dari baris mutasi periode.
func priveBalanceDebit(rows []db.ListAccountTotalsByPeriodRow, priveID int64) int64 {
	for _, row := range rows {
		if row.ID == priveID {
			return models.NumericToCents(row.TotalDebit) - models.NumericToCents(row.TotalCredit)
		}
	}
	return 0
}

// ProvisionClosingAccounts menjaga akun-akun yang dibutuhkan jurnal penutup:
// akun prive bertanda (boleh absen), akun modal = ekuitas pertama yang bukan
// prive/Ikhtisar/Saldo Laba (urut kode), dengan fallback akun Saldo Laba, dan
// akun Ikhtisar Laba Rugi yang dibuat otomatis bila belum ada. Dipakai
// bersama oleh handler tutup buku dan demoseed agar perilakunya satu.
func ProvisionClosingAccounts(ctx context.Context, q *db.Queries, companyID int64,
) (ikhtisarID, priveID, modalID int64, err error) {

	accs, err := q.ListAccounts(ctx, companyID)
	if err != nil {
		return 0, 0, 0, err
	}
	var retainedID int64
	occupied := map[string]bool{}
	for _, acc := range accs {
		occupied[acc.Code] = true
		switch acc.SystemRole {
		case models.SystemRoleIkhtisar:
			ikhtisarID = acc.ID
		case models.SystemRolePrive:
			priveID = acc.ID
		case models.SystemRoleRetainedEarnings:
			retainedID = acc.ID
		default:
			if acc.AccountType == db.AccountTypeEquity && modalID == 0 {
				modalID = acc.ID
			}
		}
	}
	if modalID == 0 && retainedID != 0 {
		modalID = retainedID
	}
	if modalID == 0 {
		return 0, 0, 0, ErrNoClosingTarget
	}
	if ikhtisarID != 0 {
		return ikhtisarID, priveID, modalID, nil
	}

	// Ikhtisar Laba Rugi belum ada: buat otomatis sebagai akun pendapatan
	// khusus (saldo akhirnya selalu nol setelah jurnal penutup).
	for _, code := range []string{"3-3099", "3-3098", "3-3097", "3-3096", "3-3095"} {
		if occupied[code] {
			continue
		}
		role := models.SystemRoleIkhtisar
		acc, errC := q.CreateAccount(ctx, db.CreateAccountParams{
			CompanyID:     companyID,
			Code:          code,
			Name:          "Ikhtisar Laba Rugi",
			AccountType:   db.AccountTypeRevenue,
			SubType:       models.SubRevenue,
			NormalBalance: db.NormalBalanceCredit,
			SystemRole:    role,
			PsakLine:      models.GuessPsakLine("Ikhtisar Laba Rugi", string(db.AccountTypeRevenue), models.SubRevenue, role),
		})
		if errC != nil {
			return 0, 0, 0, errC
		}
		return acc.ID, priveID, modalID, nil
	}
	return 0, 0, 0, fmt.Errorf("tidak ada kode akun bebas untuk Ikhtisar Laba Rugi (3-3099..3-3095 terpakai)")
}

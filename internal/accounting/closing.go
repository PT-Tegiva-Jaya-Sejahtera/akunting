// Package accounting memuat aturan akuntansi murni yang dipakai bersama oleh
// handler HTTP dan perkakas baris perintah (seed/demo).
package accounting

import (
	"errors"

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

// ClosingLines menyusun jurnal penutup: seluruh akun pendapatan ditutup dengan
// debit, seluruh akun beban ditutup dengan kredit, dan selisihnya dipindahkan
// ke akun saldo laba. Pola ini yang membuat laporan perubahan ekuitas bisa
// direkonsiliasi antar periode (PSAK 201 par. 106-110).
//
// Mengembalikan baris jurnal dan laba bersih periode (sen). Laba positif
// berarti saldo laba bertambah (kredit).
func ClosingLines(rows []db.ListAccountTotalsByPeriodRow, retainedAccountID int64) ([]Line, int64) {
	var lines []Line
	var totalRevenue, totalExpense int64

	for _, row := range rows {
		debit := models.NumericToCents(row.TotalDebit)
		credit := models.NumericToCents(row.TotalCredit)
		switch row.AccountType {
		case db.AccountTypeRevenue:
			amount := credit - debit
			if amount == 0 {
				continue
			}
			totalRevenue += amount
			// Tutup akun pendapatan: debit sebesar saldonya.
			lines = append(lines, Line{
				AccountID:   row.ID,
				Description: "Penutupan pendapatan " + row.Code,
				DebitCents:  amount,
			})
		case db.AccountTypeExpense:
			amount := debit - credit
			if amount == 0 {
				continue
			}
			totalExpense += amount
			// Tutup akun beban: kredit sebesar saldonya.
			lines = append(lines, Line{
				AccountID:   row.ID,
				Description: "Penutupan beban " + row.Code,
				CreditCents: amount,
			})
		}
	}

	net := totalRevenue - totalExpense
	if len(lines) == 0 && net == 0 {
		return nil, 0
	}
	if net > 0 {
		lines = append(lines, Line{
			AccountID:   retainedAccountID,
			Description: "Laba periode berjalan dipindahkan ke saldo laba",
			CreditCents: net,
		})
	} else if net < 0 {
		lines = append(lines, Line{
			AccountID:   retainedAccountID,
			Description: "Rugi periode berjalan dipindahkan ke saldo laba",
			DebitCents:  -net,
		})
	}
	return lines, net
}

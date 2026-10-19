package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"accounting/internal/database/db"
	appmw "accounting/internal/middleware"
	"accounting/internal/models"
	"accounting/templates"
)

// AccountsGet menampilkan daftar akun (Chart of Accounts) beserta
// sub-jenis PSAK 201 untuk klasifikasi laporan.
func (h *Handlers) AccountsGet(w http.ResponseWriter, r *http.Request) {
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	h.renderAccounts(w, r, user, role, company, "")
}

// AccountCreatePost menambahkan akun baru ke perusahaan.
func (h *Handlers) AccountCreatePost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "400 form tidak valid", http.StatusBadRequest)
		return
	}
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}

	code := strings.TrimSpace(r.FormValue("code"))
	name := strings.TrimSpace(r.FormValue("name"))
	accType := db.AccountType(r.FormValue("account_type"))
	subType := r.FormValue("sub_type")
	normal := db.NormalBalance(r.FormValue("normal_balance"))
	systemRole := strings.TrimSpace(r.FormValue("system_role"))

	if code == "" || name == "" {
		h.renderAccounts(w, r, user, role, company, "Kode dan nama akun wajib diisi.")
		return
	}
	if err := validateAccountType(accType, subType, normal); err != nil {
		h.renderAccounts(w, r, user, role, company, err.Error())
		return
	}
	if err := validateSystemRole(systemRole); err != nil {
		h.renderAccounts(w, r, user, role, company, err.Error())
		return
	}

	// psak_line ditebak dari nama/jenis akun; penanda subledger
	// (is_receivable/is_payable) diturunkan dari psak_line + sub_type supaya
	// akun piutang/hutang baru langsung bertanda tanpa menunggu migrasi.
	psakLine := models.GuessPsakLine(name, string(accType), subType, systemRole)
	isReceivable, isPayable := models.SubledgerFlags(string(accType), subType, psakLine)

	_, err := h.DB.CreateAccount(r.Context(), db.CreateAccountParams{
		CompanyID:     company.ID,
		Code:          code,
		Name:          name,
		AccountType:   accType,
		SubType:       subType,
		NormalBalance: normal,
		SystemRole:    systemRole,
		PsakLine:      psakLine,
		IsReceivable:  isReceivable,
		IsPayable:     isPayable,
	})
	if err != nil {
		h.renderAccounts(w, r, user, role, company, "Gagal menyimpan akun (kode mungkin sudah dipakai, atau peran sistem sudah dipegang akun lain).")
		return
	}
	http.Redirect(w, r, accountsPath(company.UrlKey), http.StatusSeeOther)
}

// AccountUpdatePost memperbarui satu akun (nama, jenis, sub-jenis,
// saldo normal, aktif/tidak).
func (h *Handlers) AccountUpdatePost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "400 form tidak valid", http.StatusBadRequest)
		return
	}
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	accountID, err := strconv.ParseInt(chi.URLParam(r, "accountID"), 10, 64)
	if err != nil {
		http.Error(w, "400 akun tidak valid", http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	accType := db.AccountType(r.FormValue("account_type"))
	subType := r.FormValue("sub_type")
	normal := db.NormalBalance(r.FormValue("normal_balance"))
	systemRole := strings.TrimSpace(r.FormValue("system_role"))
	isActive := r.FormValue("is_active") == "on"

	if name == "" {
		h.renderAccounts(w, r, user, role, company, "Nama akun wajib diisi.")
		return
	}
	if err := validateAccountType(accType, subType, normal); err != nil {
		h.renderAccounts(w, r, user, role, company, err.Error())
		return
	}
	if err := validateSystemRole(systemRole); err != nil {
		h.renderAccounts(w, r, user, role, company, err.Error())
		return
	}

	err = h.DB.UpdateAccount(r.Context(), db.UpdateAccountParams{
		ID:            accountID,
		CompanyID:     company.ID,
		Name:          name,
		AccountType:   accType,
		SubType:       subType,
		NormalBalance: normal,
		SystemRole:    systemRole,
		IsActive:      isActive,
	})
	if err != nil {
		h.renderAccounts(w, r, user, role, company, "Gagal memperbarui akun (peran sistem mungkin sudah dipakai akun lain).")
		return
	}
	http.Redirect(w, r, accountsPath(company.UrlKey), http.StatusSeeOther)
}

func validateAccountType(accType db.AccountType, subType string, normal db.NormalBalance) error {
	switch accType {
	case db.AccountTypeAsset, db.AccountTypeLiability, db.AccountTypeEquity,
		db.AccountTypeRevenue, db.AccountTypeExpense:
	default:
		return errString("Jenis akun tidak dikenal.")
	}
	switch subType {
	case models.SubCurrentAsset, models.SubNonCurrentAsset, models.SubCurrentLiability,
		models.SubLongLiability, models.SubEquity, models.SubRevenue, models.SubExpense:
	default:
		return errString("Sub-jenis akun tidak dikenal.")
	}
	switch normal {
	case db.NormalBalanceDebit, db.NormalBalanceCredit:
	default:
		return errString("Saldo normal tidak dikenal.")
	}
	return nil
}

// validateSystemRole memastikan hanya peran sistem yang dikenal yang dipakai.
func validateSystemRole(role string) error {
	switch role {
	case "", models.SystemRoleRetainedEarnings, models.SystemRoleKasKecil,
		models.SystemRoleIkhtisar, models.SystemRolePrive:
		return nil
	}
	return errString("Peran sistem akun tidak dikenal.")
}

func (h *Handlers) renderAccounts(w http.ResponseWriter, r *http.Request,
	user appmw.SessionUser, role string, company db.Company, errMsg string) {
	accounts, err := h.DB.ListAccountsAll(r.Context(), company.ID)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if errMsg != "" {
		w.WriteHeader(http.StatusBadRequest)
	}
	_ = templates.AccountsPage(h.newShell(r, user, company, role, "accounts", "Daftar Akun "+company.Name), company, accounts, errMsg).Render(r.Context(), w)
}

func accountsPath(companyKey string) string {
	return "/companies/" + companyKey + "/accounts"
}

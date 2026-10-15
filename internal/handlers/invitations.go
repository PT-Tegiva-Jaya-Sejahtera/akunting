package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"accounting/internal/auth"
	"accounting/internal/database/db"
	appmw "accounting/internal/middleware"
	"accounting/internal/models"
	"accounting/templates"
)

const (
	// inviteTTL: masa berlaku link undangan (batas luar).
	inviteTTL = 24 * time.Hour
	// pinTTL: masa berlaku PIN verifikasi, 5 menit sejak dibuat.
	pinTTL = 5 * time.Minute
	// draftTTL: masa berlaku data pendaftaran sementara di server.
	draftTTL = 30 * time.Minute
	// pinMaxAttempts: batas percobaan PIN yang gagal per undangan sebelum
	// undangan dikunci (anti brute-force PIN 6 digit).
	pinMaxAttempts = 5
)

// InvitationsGet menampilkan riwayat undangan perusahaan (khusus owner).
func (h *Handlers) InvitationsGet(w http.ResponseWriter, r *http.Request) {
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	h.renderInvitations(w, r, user, role, company, nil, "")
}

// InvitationCreatePost membuat satu link undangan otomatis beserta PIN
// verifikasi. Link dan PIN ditampilkan sekali di halaman ini untuk dibagikan
// (mis. lewat WhatsApp); database hanya menyimpan hash-nya.
func (h *Handlers) InvitationCreatePost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "400 form tidak valid", http.StatusBadRequest)
		return
	}
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}

	inviteRole, err := parseRoleForm(r.FormValue("role"))
	if err != nil {
		h.renderInvitations(w, r, user, role, company, nil, "Peran tidak dikenal.")
		return
	}

	token, _, err := auth.NewSessionToken()
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	pin, pinHash, err := auth.NewVerificationPIN()
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}

	now := time.Now()
	_, err = h.DB.CreateInvitation(r.Context(), db.CreateInvitationParams{
		CompanyID:    company.ID,
		Email:        pgtype.Text{}, // diisi penerima saat signup
		Role:         inviteRole,
		Token:        auth.TokenHashHex(token), // yang disimpan hash, bukan token mentah
		PinHash:      pinHash,
		PinExpiresAt: timestamptz(now.Add(pinTTL)),
		CreatedBy:    user.UserID,
		ExpiresAt:    timestamptz(now.Add(inviteTTL)),
	})
	if err != nil {
		h.renderInvitations(w, r, user, role, company, nil, "Gagal membuat undangan.")
		return
	}

	result := &models.NewInviteResult{
		Link: h.inviteBase(r) + "/invitations/" + token,
		PIN:  pin,
	}
	h.renderInvitations(w, r, user, role, company, result, "")
}

// InvitationCancelPost membatalkan undangan yang masih menunggu.
// Baris tidak dihapus: status berubah menjadi cancelled demi jejak audit.
func (h *Handlers) InvitationCancelPost(w http.ResponseWriter, r *http.Request) {
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	inviteID, err := strconv.ParseInt(chi.URLParam(r, "inviteID"), 10, 64)
	if err != nil {
		http.Error(w, "400 undangan tidak valid", http.StatusBadRequest)
		return
	}

	n, err := h.DB.CancelInvitation(r.Context(), db.CancelInvitationParams{
		ID:        inviteID,
		CompanyID: company.ID,
	})
	if err != nil {
		h.renderInvitations(w, r, user, role, company, nil, "Gagal membatalkan undangan.")
		return
	}
	if n == 0 {
		h.renderInvitations(w, r, user, role, company, nil,
			"Undangan sudah terpakai atau sudah dibatalkan. Akses yang sudah jadi dicabut lewat daftar anggota.")
		return
	}
	http.Redirect(w, r, invitationsPath(company.UrlKey), http.StatusSeeOther)
}

// InvitationAcceptGet menampilkan dialog signup untuk penerima undangan
// (publik, tanpa login): nama, username, email, password, kode verif.
func (h *Handlers) InvitationAcceptGet(w http.ResponseWriter, r *http.Request) {
	inv, company, ok := h.loadInvitation(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.InviteAcceptPage(company.Name, string(inv.Role), templates.SignupValues{}, "").Render(r.Context(), w)
}

// InvitationAcceptPost menangani dua langkah penerimaan undangan:
// langkah "data" (isi data diri + password + konfirmasi) lalu langkah
// "pin" (masukkan kode verifikasi) yang baru membuat akun.
func (h *Handlers) InvitationAcceptPost(w http.ResponseWriter, r *http.Request) {
	inv, company, ok := h.loadInvitation(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "400 form tidak valid", http.StatusBadRequest)
		return
	}

	// Seluruh isian dibersihkan lebih dahulu. Nilai bersih ini yang dikirim ke
	// database lewat parameter terikat sqlc, bukan dirangkai ke SQL.
	// Langkah PIN: draft sudah ada di server, jadi yang dikirim perambah hanya
	// tokennya, bukan data diri atau password.
	if r.FormValue("step") == "pin" {
		h.finishInvitationSignup(w, r, inv, company, sanitizeText(r.FormValue("draft"), 64))
		return
	}

	// Seluruh isian dibersihkan lebih dahulu. Nilai bersih ini yang dikirim ke
	// database lewat parameter terikat sqlc, bukan dirangkai ke SQL.
	values := templates.SignupValues{
		FullName: sanitizeText(r.FormValue("full_name"), 80),
		Username: sanitizeUsername(r.FormValue("username")),
		Email:    sanitizeEmail(r.FormValue("email")),
	}
	password := r.FormValue("password")
	password2 := r.FormValue("password2")

	if errMsg := h.validateSignup(r, values, password, password2); errMsg != "" {
		h.renderInviteAccept(w, r, inv, company, values, errMsg)
		return
	}

	// Langkah data: password langsung di-hash di sini. Password mentah tidak
	// pernah dikirim balik ke perambah lewat kolom tersembunyi.
	hash, err := auth.HashPassword(password)
	if err != nil {
		h.renderInviteAccept(w, r, inv, company, values, "Gagal memproses password.")
		return
	}
	// Draft lama yang kedaluwarsa dibersihkan sekalian, supaya tabel tetap ramping.
	_ = h.DB.DeleteExpiredSignupDrafts(r.Context())
	if err := h.DB.DeleteSignupDraftsByInvitation(r.Context(), inv.ID); err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	draftToken, _, err := auth.NewSessionToken()
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	draft, err := h.DB.CreateSignupDraft(r.Context(), db.CreateSignupDraftParams{
		TokenHash:    auth.TokenHashHex(draftToken),
		InvitationID: inv.ID,
		FullName:     values.FullName,
		Username:     values.Username,
		Email:        values.Email,
		PasswordHash: hash,
		ExpiresAt:    timestamptz(time.Now().Add(draftTTL)),
	})
	if err != nil {
		h.renderInviteAccept(w, r, inv, company, values, "Gagal menyimpan data pendaftaran.")
		return
	}
	_ = draft

	h.renderInvitePin(w, r, inv, company, draftToken, "")
}

// finishInvitationSignup menyelesaikan langkah PIN: draft diambil dari server,
// undangan ditandai terpakai, akun dibuat, dan pengguna langsung masuk.
func (h *Handlers) finishInvitationSignup(w http.ResponseWriter, r *http.Request,
	inv db.Invitation, company db.Company, draftToken string) {
	pin := sanitizePin(r.FormValue("pin"))

	draft, err := h.DB.GetSignupDraft(r.Context(), auth.TokenHashHex(draftToken))
	if err != nil || draft.InvitationID != inv.ID {
		h.renderInviteAccept(w, r, inv, company, templates.SignupValues{},
			"Data pendaftaran sudah kedaluwarsa. Isi ulang data diri Anda.")
		return
	}
	// Anti brute-force: undangan dikunci setelah pinMaxAttempts kali PIN
	// salah; pemilik harus membuat undangan baru.
	if inv.PinAttempts >= pinMaxAttempts {
		h.renderInvitePin(w, r, inv, company, draftToken,
			"Undangan terkunci karena kode verifikasi salah 5 kali. Minta owner membuat undangan baru.")
		return
	}
	if !auth.VerifyPIN(inv.PinHash, pin) || time.Now().After(inv.PinExpiresAt.Time) {
		if _, err := h.DB.IncrementInvitationPinAttempts(r.Context(), inv.ID); err != nil {
			http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
			return
		}
		h.renderInvitePin(w, r, inv, company, draftToken,
			"Kode verifikasi salah atau sudah kedaluwarsa (berlaku 5 menit sejak link dibuat).")
		return
	}

	// Akun memakai password yang sudah di-hash pada langkah pertama.
	newUser, err := h.DB.CreateUser(r.Context(), db.CreateUserParams{
		Username:     draft.Username,
		PasswordHash: draft.PasswordHash,
		FullName:     draft.FullName,
	})
	if err != nil {
		h.renderInvitePin(w, r, inv, company, draftToken, "Username sudah dipakai atau akun gagal dibuat.")
		return
	}

	// Gerbang sekali pakai: hanya berhasil bila undangan masih 'pending'.
	email := pgtype.Text{}
	if draft.Email != "" {
		email = pgtype.Text{String: draft.Email, Valid: true}
	}
	n, err := h.DB.MarkInvitationUsed(r.Context(), db.MarkInvitationUsedParams{
		ID:         inv.ID,
		CompanyID:  company.ID, // kunci ganda: tanda terpakai hanya untuk perusahaan pemilik undangan
		UsedUserID: pgtype.Int8{Int64: newUser.ID, Valid: true},
		Email:      email,
	})
	if err != nil || n == 0 {
		h.renderInvitePin(w, r, inv, company, draftToken, "Undangan baru saja dipakai atau dibatalkan.")
		return
	}

	if err := h.DB.GrantCompanyAccess(r.Context(), db.GrantCompanyAccessParams{
		CompanyID: company.ID,
		UserID:    newUser.ID,
		Role:      inv.Role,
	}); err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	// Draft sekali pakai: hapus setelah akun terbentuk.
	_ = h.DB.DeleteSignupDraft(r.Context(), draft.TokenHash)

	token, tokenHash, err := auth.NewSessionToken()
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	if _, err := h.DB.CreateSession(r.Context(), db.CreateSessionParams{
		UserID:    newUser.ID,
		TokenHash: tokenHash,
		ExpiresAt: timestamptz(time.Now().Add(auth.SessionTTL)),
	}); err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}
	auth.SetSessionCookie(w, token, h.CookieSecure)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// UsernameCheckGet menjawab apakah username masih tersedia. Endpoint hanya
// hidup di dalam tautan undangan yang sah, bukan endpoint publik bebas, supaya
// daftar nama pengguna tidak bisa diambil orang luar.
func (h *Handlers) UsernameCheckGet(w http.ResponseWriter, r *http.Request) {
	_, _, ok := h.loadInvitation(w, r)
	if !ok {
		return
	}
	username := sanitizeUsername(r.URL.Query().Get("username"))
	type jawaban struct {
		Username string `json:"username"`
		Tersedia bool   `json:"tersedia"`
		Pesan    string `json:"pesan"`
	}
	out := jawaban{Username: username}
	switch {
	case len(username) < 3:
		out.Pesan = "Username minimal 3 karakter."
	case len(username) > 40:
		out.Pesan = "Username maksimal 40 karakter."
	default:
		taken, err := h.DB.UsernameTaken(r.Context(), username)
		if err != nil {
			http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
			return
		}
		out.Tersedia = !taken
		if taken {
			out.Pesan = "Username sudah dipakai."
		} else {
			out.Pesan = "Username tersedia."
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
}

// validateSignup memeriksa data diri untuk kedua langkah pendaftaran.
func (h *Handlers) validateSignup(r *http.Request, values templates.SignupValues, password, password2 string) string {
	if len(values.FullName) < 2 {
		return "Nama lengkap minimal 2 karakter."
	}
	if len(values.Username) < 3 {
		return "Username minimal 3 karakter (huruf, angka, titik, garis bawah, atau tanda hubung)."
	}
	// Email bersifat opsional saat mendaftar; bila diisi harus berbentuk benar.
	if values.Email != "" && !emailValid(values.Email) {
		return "Email tidak valid."
	}
	if aturan := auth.PeriksaPassword(password, password2); len(aturan.Masalah) > 0 {
		return aturan.PesanMasalah()
	}
	if taken, err := h.DB.UsernameTaken(r.Context(), values.Username); err == nil && taken {
		return "Username sudah dipakai, pilih yang lain."
	}
	return ""
}

// loadInvitation memuat undangan dari token di URL lalu memvalidasi
// syaratnya. Gagal = halaman penolakan (403).
func (h *Handlers) loadInvitation(w http.ResponseWriter, r *http.Request) (db.Invitation, db.Company, bool) {
	fail := func(msg string) (db.Invitation, db.Company, bool) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_ = templates.InviteErrorPage(msg).Render(r.Context(), w)
		return db.Invitation{}, db.Company{}, false
	}

	inv, err := h.DB.GetInvitationByToken(r.Context(), auth.TokenHashHex(chi.URLParam(r, "token")))
	if err != nil {
		return fail("Undangan tidak valid atau sudah dibatalkan.")
	}
	if inv.Status != db.InvitationStatusPending {
		return fail("Undangan sudah dipakai atau sudah dibatalkan.")
	}
	if time.Now().After(inv.ExpiresAt.Time) {
		return fail("Link undangan sudah kedaluwarsa (berlaku 24 jam).")
	}
	if time.Now().After(inv.PinExpiresAt.Time) {
		return fail("Kode verifikasi sudah kedaluwarsa (berlaku 5 menit sejak link dibuat). Minta owner membuat link baru.")
	}

	company, err := h.DB.GetCompanyByID(r.Context(), inv.CompanyID)
	if err != nil {
		http.Error(w, "404 perusahaan tidak ditemukan", http.StatusNotFound)
		return db.Invitation{}, db.Company{}, false
	}
	return inv, company, true
}

func (h *Handlers) renderInviteAccept(w http.ResponseWriter, r *http.Request,
	inv db.Invitation, company db.Company, values templates.SignupValues, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	_ = templates.InviteAcceptPage(company.Name, string(inv.Role), values, errMsg).Render(r.Context(), w)
}

func (h *Handlers) renderInvitePin(w http.ResponseWriter, r *http.Request,
	inv db.Invitation, company db.Company, draftToken, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if errMsg != "" {
		w.WriteHeader(http.StatusBadRequest)
	}
	_ = templates.InvitePinPage(company.Name, string(inv.Role), draftToken, errMsg).Render(r.Context(), w)
}

func (h *Handlers) renderInvitations(w http.ResponseWriter, r *http.Request,
	user appmw.SessionUser, role string, company db.Company, result *models.NewInviteResult, errMsg string) {
	invites, err := h.DB.ListInvitations(r.Context(), company.ID)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}

	now := time.Now()
	views := make([]models.InviteView, 0, len(invites))
	for _, inv := range invites {
		v := models.InviteView{
			ID:         inv.ID,
			Role:       string(inv.Role),
			Status:     string(inv.Status),
			CreatedAgo: models.FormatTimeAgoID(inv.CreatedAt.Time, now),
			UsedBy:     "-",
		}
		if inv.Email.Valid && inv.Email.String != "" {
			v.Email = inv.Email.String
		} else {
			v.Email = "-"
		}
		switch inv.Status {
		case db.InvitationStatusPending:
			v.StatusLabel = "menunggu"
			v.Cancelable = true
			v.ExpiresIn = leftOf(inv.ExpiresAt.Time, now)
			v.PinLeft = leftOf(inv.PinExpiresAt.Time, now)
		case db.InvitationStatusUsed:
			v.StatusLabel = "terpakai"
			v.ExpiresIn, v.PinLeft = "-", "-"
		default:
			v.StatusLabel = "dibatalkan"
			v.ExpiresIn, v.PinLeft = "-", "-"
		}
		views = append(views, v)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if errMsg != "" {
		w.WriteHeader(http.StatusBadRequest)
	}
	_ = templates.InvitationsPage(h.newShell(r, user, company, role, "invitations", "Undangan "+company.Name), company, views, result, errMsg).Render(r.Context(), w)
}

func leftOf(deadline, now time.Time) string {
	if rem := deadline.Sub(now); rem > 0 {
		return models.FormatDurationID(rem)
	}
	return "kedaluwarsa"
}

// inviteBase menyusun alamat dasar tautan undangan. Sumber utama adalah
// PUBLIC_BASE_URL (env tepercaya yang dibaca saat start). Bila kosong, fallback
// selalu https:// + r.Host. Header X-Forwarded-Proto dan skema request TIDAK
// dipercaya: keduanya bisa dimanipulasi klien untuk membentuk link phishing.
func (h *Handlers) inviteBase(r *http.Request) string {
	if h.PublicBaseURL != "" {
		return h.PublicBaseURL
	}
	return "https://" + r.Host
}

func invitationsPath(companyKey string) string {
	return "/companies/" + companyKey + "/invitations"
}

package handlers

import (
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5/pgtype"

	"accounting/internal/database/db"
	appmw "accounting/internal/middleware"
	"accounting/internal/models"
	"accounting/templates"
)

// maxInputLength membatasi panjang isian teks dari formulir. Batas ini juga
// menjadi jaring pengaman terhadap isian raksasa yang memperlambat server.
const maxInputLength = 160

// sanitizeText membersihkan isian teks pengguna: memotong spasi tepi, membuang
// karakter kendali (termasuk baris baru), merapatkan spasi ganda, dan membatasi
// panjang. Nilai hasilnya selalu dikirim ke database lewat parameter terikat
// (sqlc), bukan dirangkai ke dalam SQL, jadi tidak ada celah injection.
func sanitizeText(s string, max int) string {
	if max <= 0 || max > maxInputLength {
		max = maxInputLength
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			b.WriteRune(' ')
		case unicode.IsControl(r):
			// dibuang
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	if len(out) > max {
		out = strings.TrimSpace(out[:max])
	}
	return out
}

// sanitizeEmail merapikan email: huruf kecil, tanpa spasi, tanpa karakter
// kendali, dan dibatasi panjangnya.
func sanitizeEmail(s string) string {
	s = strings.ToLower(sanitizeText(s, 120))
	return strings.ReplaceAll(s, " ", "")
}

// sanitizePhone menyisakan angka dan tanda + di depan.
func sanitizePhone(s string) string {
	s = sanitizeText(s, 30)
	var b strings.Builder
	for i, r := range s {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' && i == 0:
			b.WriteRune(r)
		}
	}
	if len(b.String()) > 20 {
		return b.String()[:20]
	}
	return b.String()
}

// sanitizePin menyisakan angka saja dan membatasi panjangnya, supaya kode
// verifikasi yang diketik tidak membawa karakter aneh.
func sanitizePin(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	if len(b.String()) > 6 {
		return b.String()[:6]
	}
	return b.String()
}

// emailValid memeriksa bentuk email secara sederhana namun ketat.
func emailValid(s string) bool {
	if s == "" || len(s) > 120 || strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	at := strings.Index(s, "@")
	if at < 1 || at != strings.LastIndex(s, "@") {
		return false
	}
	domain := s[at+1:]
	if !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return false
	}
	return true
}

// pgDate mengubah waktu menjadi pgtype.Date yang sah.
func pgDate(t time.Time) pgtype.Date {
	return pgtype.Date{Time: t, Valid: true, InfinityModifier: pgtype.Finite}
}

// profileData merangkum data profil beserta konteks sidebar yang dibutuhkan
// halaman di luar cakupan perusahaan.
type profileData struct {
	View      templates.ProfileView
	User      appmw.SessionUser
	Role      string
	Company   db.Company
	Companies []db.ListCompaniesForUserRow
}

// ProfileGet menampilkan pengaturan profil pengguna aktif.
func (h *Handlers) ProfileGet(w http.ResponseWriter, r *http.Request) {
	user, ok := appmw.UserFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	data, err := h.loadProfile(r, user)
	if err != nil {
		http.Error(w, "404 profil tidak ditemukan", http.StatusNotFound)
		return
	}
	h.renderProfile(w, r, data, http.StatusOK)
}

// ProfilePost menyimpan perubahan profil.
func (h *Handlers) ProfilePost(w http.ResponseWriter, r *http.Request) {
	user, ok := appmw.UserFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "400 form tidak valid", http.StatusBadRequest)
		return
	}
	data, err := h.loadProfile(r, user)
	if err != nil {
		http.Error(w, "404 profil tidak ditemukan", http.StatusNotFound)
		return
	}

	fullName := sanitizeText(r.FormValue("full_name"), 80)
	email := sanitizeEmail(r.FormValue("email"))
	phone := sanitizePhone(r.FormValue("phone"))
	birthRaw := strings.TrimSpace(r.FormValue("birth_date"))

	gagal := func(msg string, status int) {
		data.View.Error = msg
		h.renderProfile(w, r, data, status)
	}

	// Tanggal lahir boleh kosong; selain itu harus dd/mm/yyyy yang sah dan
	// tidak di masa depan.
	var birth pgtype.Date
	if birthRaw != "" {
		t, errTgl := models.ParseDateID(birthRaw)
		if errTgl != nil {
			gagal("Tanggal lahir harus berformat dd/mm/yyyy.", http.StatusBadRequest)
			return
		}
		if t.After(time.Now()) {
			gagal("Tanggal lahir tidak boleh di masa depan.", http.StatusBadRequest)
			return
		}
		birth = pgDate(t)
		data.View.BirthDateISO = t.Format("2006-01-02")
	}
	if len(fullName) < 2 {
		gagal("Nama lengkap minimal 2 karakter.", http.StatusBadRequest)
		return
	}
	if email != "" && !emailValid(email) {
		gagal("Email tidak valid.", http.StatusBadRequest)
		return
	}

	if err := h.DB.UpdateUserProfile(r.Context(), db.UpdateUserProfileParams{
		FullName:  fullName,
		Email:     email,
		Phone:     phone,
		BirthDate: birth,
		ID:        user.UserID,
	}); err != nil {
		gagal("Gagal menyimpan profil.", http.StatusInternalServerError)
		return
	}

	data, err = h.loadProfile(r, user)
	if err != nil {
		http.Error(w, "404 profil tidak ditemukan", http.StatusNotFound)
		return
	}
	data.View.Saved = true
	h.renderProfile(w, r, data, http.StatusOK)
}

// loadProfile membaca profil pengguna beserta peran dan daftar perusahaannya.
// Daftar perusahaan dibaca sekali saja supaya tidak berulang kali ke database.
func (h *Handlers) loadProfile(r *http.Request, user appmw.SessionUser) (profileData, error) {
	row, err := h.DB.GetUserProfile(r.Context(), user.UserID)
	if err != nil {
		return profileData{}, err
	}
	rows, err := h.DB.ListCompaniesForUser(r.Context(), user.UserID)
	if err != nil {
		rows = nil
	}
	companies := make([]templates.CompanyOption, 0, len(rows))
	for i, c := range rows {
		companies = append(companies, templates.CompanyOption{
			ID:      c.ID,
			Key:     c.UrlKey,
			Name:    c.Name,
			Role:    c.Role,
			Current: i == 0,
		})
	}

	view := templates.ProfileView{
		FullName:     row.FullName,
		Username:     row.Username,
		Email:        row.Email,
		Phone:        row.Phone,
		MemberSince:  models.FormatDateID(row.CreatedAt.Time),
		RoleAccess:   roleAccessText(rows),
		CompanyCount: len(rows),
	}
	if row.FullName != user.FullName {
		// Nama pada kerangka halaman selalu versi terbaru dari database.
		user.FullName = row.FullName
	}
	role := ""
	if len(rows) > 0 {
		role = rows[0].Role
	}
	_ = companies
	return profileData{View: view, User: user, Role: role, Companies: rows}, nil
}

// roleAccessText menyusun keterangan hak akses pengguna. Nilai ini hanya
// tampilan (kolom disabled): peran diatur owner lewat Pengaturan perusahaan.
func roleAccessText(rows []db.ListCompaniesForUserRow) string {
	if len(rows) == 0 {
		return "Belum terhubung ke pembukuan mana pun"
	}
	var bagian []string
	for _, c := range rows {
		bagian = append(bagian, roleLabelPlain(c.Role)+" di "+c.Name)
	}
	return strings.Join(bagian, " · ")
}

// roleLabelPlain menulis peran dalam huruf kecil agar enak dirangkai kalimat.
func roleLabelPlain(role string) string {
	switch role {
	case "owner":
		return "Owner"
	case "admin":
		return "Admin"
	case "staff":
		return "Staf"
	default:
		return role
	}
}

func (h *Handlers) renderProfile(w http.ResponseWriter, r *http.Request, data profileData, status int) {
	shell := templates.Shell{
		Title:    "Pengaturan Profil",
		UserName: data.User.FullName,
		Role:     data.Role,
		Active:   "profile",
	}
	if len(data.Companies) > 0 {
		shell.CompanyID = data.Companies[0].ID
		shell.CompanyKey = data.Companies[0].UrlKey
		shell.CompanyName = data.Companies[0].Name
	}
	for _, c := range data.Companies {
		shell.Companies = append(shell.Companies, templates.CompanyOption{
			ID: c.ID, Key: c.UrlKey, Name: c.Name, Role: c.Role, Current: c.UrlKey == shell.CompanyKey,
		})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = templates.ProfilePage(shell, data.View).Render(r.Context(), w)
}

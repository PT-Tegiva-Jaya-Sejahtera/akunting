package templates

// Shell adalah data kerangka halaman: judul, identitas pengguna, perusahaan
// aktif, dan daftar perusahaan yang boleh diakses (untuk picker perusahaan).
// Satu struct dipakai semua halaman supaya menambah isi sidebar tidak
// menyentuh setiap pemanggil Layout.
type Shell struct {
	Title       string
	UserName    string
	CompanyID   int64  // id internal, tidak pernah dipakai di URL
	CompanyKey  string // kunci acak untuk alamat /companies/<kunci>
	CompanyName string
	Role        string
	Active      string // dashboard | journals | ledger | pl | bs | adjustments | worksheet | statements | periods | settings | invitations
	Companies   []CompanyOption
	// Plain = true merender halaman tanpa sidebar dan tanpa menu perusahaan,
	// dipakai halaman pemilihan pembukuan.
	Plain bool
}

// CompanyOption adalah satu perusahaan pada picker sidebar.
type CompanyOption struct {
	ID      int64
	Key     string
	Name    string
	Role    string
	Current bool
}

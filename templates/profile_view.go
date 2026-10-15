package templates

// ProfileView adalah data halaman Pengaturan profil. Dipisahkan dari berkas
// .templ supaya templ hanya berisi markup.
type ProfileView struct {
	FullName     string
	Username     string
	Email        string
	Phone        string
	BirthDateISO string
	RoleAccess   string
	MemberSince  string
	CompanyCount int
	Error        string
	Saved        bool
}

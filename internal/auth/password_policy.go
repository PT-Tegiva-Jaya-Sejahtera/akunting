package auth

import (
	"strings"
	"unicode"
)

// AturanPassword adalah hasil pemeriksaan satu password: daftar masalah yang
// harus diperbaiki dan tingkat kekuatannya. Dipakai server agar aturan yang
// ditampilkan di layar sama dengan yang ditegakkan di server.
type AturanPassword struct {
	MinPanjang bool
	AdaKapital bool
	AdaAngka   bool
	AdaSimbol  bool
	Sama       bool
	Masalah    []string
	Kekuatan   string // "Lemah", "Kuat", atau "Sangat kuat"
	Skor       int    // 0-4, untuk indikator kekuatan
}

// MinPasswordLength dan MaxPasswordBytes: bcrypt hanya memakai 72 byte pertama,
// jadi password yang lebih panjang ditolak agar tidak terpotong diam-diam.
const (
	MinPasswordLength = 8
	MaxPasswordBytes  = 72
	KuatPasswordBytes = 12
)

// PeriksaPassword memeriksa password dan konfirmasinya. Pesan masalah ditulis
// dalam Bahasa Indonesia supaya bisa langsung ditampilkan ke pengguna.
func PeriksaPassword(password, konfirmasi string) AturanPassword {
	hasil := AturanPassword{
		MinPanjang: len([]rune(password)) >= MinPasswordLength,
		Sama:       password != "" && password == konfirmasi,
	}
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			hasil.AdaKapital = true
		case unicode.IsDigit(r):
			hasil.AdaAngka = true
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			hasil.AdaSimbol = true
		}
	}

	if !hasil.MinPanjang {
		hasil.Masalah = append(hasil.Masalah, "Password minimal 8 karakter.")
	}
	if !hasil.AdaKapital {
		hasil.Masalah = append(hasil.Masalah, "Password harus memuat huruf kapital.")
	}
	if !hasil.AdaAngka {
		hasil.Masalah = append(hasil.Masalah, "Password harus memuat angka.")
	}
	if !hasil.AdaSimbol {
		hasil.Masalah = append(hasil.Masalah, "Password harus memuat simbol, misalnya ! @ # $ %.")
	}
	if !hasil.Sama {
		hasil.Masalah = append(hasil.Masalah, "Konfirmasi password tidak sama dengan password.")
	}
	if len(password) > MaxPasswordBytes {
		hasil.Masalah = append(hasil.Masalah, "Password terlalu panjang, maksimal 72 karakter.")
	}
	if strings.ContainsAny(password, "\r\n\t") {
		hasil.Masalah = append(hasil.Masalah, "Password tidak boleh memuat tab atau baris baru.")
	}

	for _, ok := range []bool{hasil.MinPanjang, hasil.AdaKapital, hasil.AdaAngka, hasil.AdaSimbol} {
		if ok {
			hasil.Skor++
		}
	}
	switch {
	case len(hasil.Masalah) > 0:
		hasil.Kekuatan = "Lemah"
	case len(password) >= KuatPasswordBytes:
		hasil.Kekuatan = "Sangat kuat"
	default:
		hasil.Kekuatan = "Kuat"
	}
	return hasil
}

// PesanMasalah menggabungkan masalah menjadi satu kalimat pesan.
func (a AturanPassword) PesanMasalah() string {
	return strings.Join(a.Masalah, " ")
}

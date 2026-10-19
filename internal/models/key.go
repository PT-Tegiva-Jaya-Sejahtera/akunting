package models

import (
	"crypto/rand"
	"math/big"
	"strings"
)

// Huruf dan angka untuk kunci URL. Huruf yang mudah tertukar saat dibaca
// (i, l, o, 0, 1) sengaja dibuang supaya kunci tetap enak dibaca dan diketik.
const companyKeyAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

// companyKeyLength adalah panjang kunci URL perusahaan. Dua belas karakter dari
// 31 pilihan menghasilkan sekitar 59 bit keacakan, cukup untuk membuat urutan
// pembukuan tidak bisa ditebak, tanpa membuat alamat menjadi terlalu panjang.
const companyKeyLength = 12

// NewCompanyKey membuat kunci URL perusahaan yang acak dan aman dari tebakan.
func NewCompanyKey() (string, error) {
	var b strings.Builder
	max := big.NewInt(int64(len(companyKeyAlphabet)))
	for i := 0; i < companyKeyLength; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b.WriteByte(companyKeyAlphabet[n.Int64()])
	}
	return b.String(), nil
}

// CompanyKeyValid memeriksa bentuk kunci URL sebelum dipakai untuk mencari
// perusahaan, supaya permintaan yang jelas salah tidak sampai ke database.
// Bentuk yang diterima: huruf kecil atau angka, panjang 8 sampai 32 karakter.
// Pemeriksaan ini hanya penyaring awal; kebenaran kunci tetap ditentukan
// pencarian di database dan pemeriksaan hak akses.
func CompanyKeyValid(key string) bool {
	if len(key) < 8 || len(key) > 32 {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			continue
		}
		return false
	}
	return true
}

// SlugifyName mengubah nama perusahaan menjadi slug URL: huruf kecil ASCII,
// pemisah tanda hubung, tanpa karakter lain. Nama Indonesia umumnya cukup;
// huruf non-ASCII (mis. é) dibuang setelah diterjemahkan seperlunya.
// Hasil kosong jatuh ke "perusahaan".
func SlugifyName(name string) string {
	var b strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 48 {
		out = strings.TrimSuffix(out[:48], "-")
	}
	if out == "" {
		return "perusahaan"
	}
	return out
}

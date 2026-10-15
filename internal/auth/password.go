// Package auth berisi logika password hashing dan manajemen session cookie.
package auth

import "golang.org/x/crypto/bcrypt"

// bcryptCost 12 = trade-off aman untuk server dengan RAM terbatas.
const bcryptCost = 12

// HashPassword menghasilkan hash bcrypt dari password mentah.
func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// VerifyPassword membandingkan password mentah dengan hash tersimpan.
func VerifyPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// NewVerificationPIN membuat PIN verifikasi 6 digit beserta hash-nya.
// PIN asli hanya ditampilkan sekali; database menyimpan hash-nya saja.
func NewVerificationPIN() (pin, pinHash string, err error) {
	var buf [3]byte
	if _, err = rand.Read(buf[:]); err != nil {
		return "", "", err
	}
	n := int(buf[0])<<16 | int(buf[1])<<8 | int(buf[2])
	pin = fmt.Sprintf("%06d", n%1000000)
	return pin, HashToken(pin), nil
}

// VerifyPIN membandingkan PIN yang diketik dengan hash tersimpan.
func VerifyPIN(pinHash, pin string) bool {
	return pinHash == HashToken(pin)
}

// TokenHashHex sama dengan HashToken; dipakai untuk lookup token undangan.
func TokenHashHex(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

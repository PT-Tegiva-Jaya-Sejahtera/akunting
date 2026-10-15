package models

import (
	"fmt"
	"time"
)

// InviteView adalah satu baris riwayat undangan (tanpa rahasia).
type InviteView struct {
	ID          int64
	Email       string // email penerima saat signup; kosong bila belum dipakai
	Role        string
	Status      string // pending | used | cancelled
	StatusLabel string
	CreatedAgo  string
	ExpiresIn   string // sisa berlaku link (24 jam)
	PinLeft     string // sisa berlaku PIN (5 menit)
	UsedBy      string
	Cancelable  bool
}

// NewInviteResult adalah hasil pembuatan undangan: link + PIN, ditampilkan sekali.
type NewInviteResult struct {
	Link string
	PIN  string
}

// Sub-type akun (klasifikasi PSAK 201) beserta labelnya.
const (
	SubCurrentAsset     = "aset_lancar"
	SubNonCurrentAsset  = "aset_tidak_lancar"
	SubCurrentLiability = "liabilitas_jangka_pendek"
	SubLongLiability    = "liabilitas_jangka_panjang"
	SubEquity           = "ekuitas"
	SubRevenue          = "pendapatan"
	SubExpense          = "beban"
)

// SubTypeLabel menerjemahkan sub_type ke label tampilan.
func SubTypeLabel(sub string) string {
	switch sub {
	case SubCurrentAsset:
		return "Aset Lancar"
	case SubNonCurrentAsset:
		return "Aset Tidak Lancar"
	case SubCurrentLiability:
		return "Liabilitas Jangka Pendek"
	case SubLongLiability:
		return "Liabilitas Jangka Panjang"
	case SubEquity:
		return "Ekuitas"
	case SubRevenue:
		return "Pendapatan"
	case SubExpense:
		return "Beban"
	default:
		return "-"
	}
}

// FormatDurationID menerjemahkan selisih waktu ke teks ringkas berbahasa Indonesia.
func FormatDurationID(d time.Duration) string {
	switch {
	case d < 0:
		return "0"
	case d < time.Minute:
		return "kurang dari 1 menit"
	case d < time.Hour:
		return fmt.Sprintf("%d menit", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d jam", int(d.Hours()))
	default:
		return fmt.Sprintf("%d hari", int(d.Hours()/24))
	}
}

// FormatTimeAgoID menampilkan waktu lampau: "2 jam lalu", "baru saja", dst.
func FormatTimeAgoID(t time.Time, now time.Time) string {
	if t.IsZero() {
		return "belum pernah"
	}
	d := now.Sub(t)
	if d < time.Minute {
		return "baru saja"
	}
	return FormatDurationID(d) + " lalu"
}

package models

import "testing"

// TestSubledgerFlags memastikan penanda subledger untuk akun BARU sama dengan
// aturan backfill migrasi 0014 (is_receivable) dan 0015 (is_payable), supaya
// jalur insert tidak perlu menunggu migrasi ulang.
func TestSubledgerFlags(t *testing.T) {
	kasus := []struct {
		nama                string
		tipe, sub, psak     string
		mauRecv, mauPayable bool
	}{
		{"piutang usaha", TypeAsset, SubCurrentAsset, PsakPiutang, true, false},
		{"utang usaha", TypeLiability, SubCurrentLiability, PsakUtangUsaha, false, true},
		{"kas", TypeAsset, SubCurrentAsset, PsakKasSetaraKas, false, false},
		{"utang bank", TypeLiability, SubCurrentLiability, PsakUtangBank, false, false},
		{"utang usaha jangka panjang", TypeLiability, SubLongLiability, PsakUtangUsaha, false, false},
		{"psak piutang di luar aset", TypeRevenue, SubRevenue, PsakPiutang, false, false},
	}
	for _, k := range kasus {
		recv, pay := SubledgerFlags(k.tipe, k.sub, k.psak)
		if recv != k.mauRecv || pay != k.mauPayable {
			t.Errorf("SubledgerFlags(%q, %q, %q) = (%v, %v), mau (%v, %v) untuk kasus %s",
				k.tipe, k.sub, k.psak, recv, pay, k.mauRecv, k.mauPayable, k.nama)
		}
	}
}

// TestSubledgerFlagsIkutGuessPsakLine memastikan akun yang psak_line-nya
// ditebak otomatis (pola yang dipakai jalur insert) langsung bertanda.
func TestSubledgerFlagsIkutGuessPsakLine(t *testing.T) {
	piutang := GuessPsakLine("Piutang Usaha", TypeAsset, SubCurrentAsset, "")
	recv, pay := SubledgerFlags(TypeAsset, SubCurrentAsset, piutang)
	if !recv || pay {
		t.Errorf("akun Piutang Usaha harus is_receivable, dapat (%v, %v) dengan psak_line %q", recv, pay, piutang)
	}

	utang := GuessPsakLine("Utang Usaha", TypeLiability, SubCurrentLiability, "")
	recv, pay = SubledgerFlags(TypeLiability, SubCurrentLiability, utang)
	if recv || !pay {
		t.Errorf("akun Utang Usaha harus is_payable, dapat (%v, %v) dengan psak_line %q", recv, pay, utang)
	}

	bank := GuessPsakLine("Utang Bank", TypeLiability, SubCurrentLiability, "")
	recv, pay = SubledgerFlags(TypeLiability, SubCurrentLiability, bank)
	if recv || pay {
		t.Errorf("akun Utang Bank tidak boleh bertanda, dapat (%v, %v) dengan psak_line %q", recv, pay, bank)
	}
}

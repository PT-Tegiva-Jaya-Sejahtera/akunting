// Uji Catatan atas Laporan Keuangan (CaLK): kebijakan bernomor, referensi
// silang akun ke kebijakan, dan sumber ketidakpastian estimasi.
package models

import "testing"

func notesFixture() (balance, priorBalance, movements, priorMovements []AccountTotal) {
	// Periode pembanding = transaksi 2025 (pendapatan 40 juta, beban gaji
	// 25 juta), sesuai bagian "opening" pada skenario penuh.
	prior := []testJournal{
		{bucket: "movements", lines: []testLine{{"1-1000", 40_000_000, 0}, {"4-1000", 0, 40_000_000}}},
		{bucket: "movements", lines: []testLine{{"5-1000", 25_000_000, 0}, {"1-1000", 0, 25_000_000}}},
	}
	return aggregate(fullScenario(), "opening", "movements", "closing"),
		aggregate(fullScenario(), "opening"),
		aggregate(fullScenario(), "movements"),
		aggregate(prior, "movements")
}

func TestBuildNotePoliciesBernomorDanBersilang(t *testing.T) {
	policies, numbers := BuildNotePolicies(aggregate(fullScenario(), "opening", "movements", "closing"))
	if len(policies) == 0 {
		t.Fatal("tidak ada kebijakan yang dihasilkan")
	}
	for i, p := range policies {
		if p.Number != i+1 {
			t.Fatalf("nomor kebijakan tidak berurutan: %+v", p)
		}
		if p.Anchor == "" || p.Title == "" || p.Body == "" {
			t.Fatalf("kebijakan tidak lengkap: %+v", p)
		}
	}
	// Referensi silang: setiap akun dengan pemetaan pos beroleh nomor yang ada
	// di daftar kebijakan (PSAK 201 par. 113).
	for _, a := range aggregate(fullScenario(), "movements") {
		policy, ok := PolicyFor(a.PsakLine)
		if !ok {
			continue
		}
		num, ok := numbers[a.PsakLine]
		if !ok {
			t.Fatalf("akun %s tidak punya nomor kebijakan", a.Code)
		}
		if num < 1 || num > len(policies) || policies[num-1].Anchor != policy.Anchor {
			t.Fatalf("rujukan silang akun %s salah: nomor=%d anchor=%s", a.Code, num, policy.Anchor)
		}
	}
}

func TestNotesViewBuild(t *testing.T) {
	balance, priorBalance, movements, priorMovements := notesFixture()
	equity := BuildEquity(
		aggregate(fullScenario(), "opening"),
		aggregate(fullScenario(), "movements"),
		aggregate(fullScenario(), "closing"),
	)
	var v NotesView
	v.BaseCurrency = "IDR"
	v.RoundingLabel = "dalam Rupiah"
	v.CompanyName = "PT Uji"
	v.Build(balance, priorBalance, movements, priorMovements, equity)

	if v.Compliance == "" || v.BasisNotes == nil {
		t.Fatal("pernyataan kepatuhan dan dasar penyusunan wajib ada")
	}
	if len(v.Policies) == 0 {
		t.Fatal("kebijakan akuntansi kosong")
	}
	if v.Distributions != rp(5_000_000) {
		t.Fatalf("distribusi pemilik = %d, mau %d", v.Distributions, rp(5_000_000))
	}

	// Estimasi: aset tetap, piutang, persediaan semuanya ada di skenario.
	if len(v.Estimates) != 3 {
		t.Fatalf("jumlah estimasi = %d, mau 3: %+v", len(v.Estimates), v.Estimates)
	}
	for _, e := range v.Estimates {
		if e.Title == "" || e.Nature == "" {
			t.Fatalf("estimasi tidak lengkap: %+v", e)
		}
		if e.PolicyNumber < 1 {
			t.Fatalf("estimasi %q tanpa rujukan kebijakan", e.Title)
		}
	}

	// Rincian per akun: kelompok beban menampilkan angka positif mengikuti
	// penyajian Laporan Laba Rugi.
	var beban, pendapatan *NoteGroup
	for i := range v.Groups {
		switch v.Groups[i].Label {
		case "Beban periode berjalan":
			beban = &v.Groups[i]
		case "Pendapatan periode berjalan":
			pendapatan = &v.Groups[i]
		}
	}
	if beban == nil || pendapatan == nil {
		t.Fatalf("kelompok laba rugi tidak lengkap: %+v", v.Groups)
	}
	if pendapatan.TotalCents != rp(30_000_000) {
		t.Fatalf("jumlah pendapatan = %d, mau %d", pendapatan.TotalCents, rp(30_000_000))
	}
	if beban.TotalCents != rp(23_500_000) {
		t.Fatalf("jumlah beban = %d, mau %d (positif)", beban.TotalCents, rp(23_500_000))
	}
	for _, line := range beban.Lines {
		if line.AmountCents < 0 {
			t.Fatalf("baris beban negatif: %+v", line)
		}
		if line.PolicyNumber < 1 {
			t.Fatalf("baris beban %s tanpa rujukan kebijakan", line.Code)
		}
	}
}

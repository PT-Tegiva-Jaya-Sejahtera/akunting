// Catatan atas Laporan Keuangan (CaLK), PSAK 201 par. 112-138. Bagian catatan
// disusun dari data akun perusahaan dan katalog kebijakan akuntansi di file
// ini, dengan referensi silang akun ke kebijakan sesuai par. 113.
package models

import "strings"

// NotePolicy adalah satu kebijakan akuntansi signifikan (PSAK 201 par. 117-124).
type NotePolicy struct {
	Number int
	Anchor string
	Title  string
	Body   string
}

// NoteAccountLine adalah rincian satu akun pada catatan, lengkap dengan nomor
// catatan kebijakan yang mengaturnya (referensi silang, par. 113).
type NoteAccountLine struct {
	Code             string
	Name             string
	AmountCents      int64
	PriorAmountCents int64
	PolicyNumber     int
	PolicyTitle      string
	PolicyAnchor     string
}

// NoteGroup adalah satu kelompok rincian akun mengikuti urutan penyajian
// laporan posisi keuangan dan laba rugi (par. 114 huruf c).
type NoteGroup struct {
	Label           string
	Lines           []NoteAccountLine
	TotalCents      int64
	PriorTotalCents int64
}

// NoteEstimate adalah satu sumber ketidakpastian estimasi (PSAK 201 par. 125):
// sifat asumsi dan jumlah tercatat aset/liabilitas yang terpengaruh.
type NoteEstimate struct {
	Title        string
	Nature       string
	AmountCents  int64
	PolicyNumber int
}

// NotesView adalah data halaman Catatan atas Laporan Keuangan.
type NotesView struct {
	ReportView

	Compliance    string         // pernyataan kepatuhan (par. 16)
	BasisNotes    []string       // dasar penyusunan (par. 25-28, 49-53)
	Policies      []NotePolicy   // ringkasan kebijakan akuntansi signifikan
	Estimates     []NoteEstimate // sumber ketidakpastian estimasi (par. 125)
	Groups        []NoteGroup    // rincian per akun material (par. 113)
	Distributions int64          // distribusi kepada pemilik (par. 107 dan 137)
	EntityFacts   []string       // informasi entitas (par. 138)
	CapitalNote   string         // pengelolaan modal (par. 134-136)
}

// Katalog kebijakan akuntansi menurut pemetaan pos akun (accounts.psak_line).
// Hanya kebijakan yang dirujuk akun yang ada yang disajikan, mengikuti
// par. 117A: kebijakan untuk transaksi yang tidak material tidak diungkapkan.
var notePolicyCatalog = []struct {
	psakLine string
	title    string
	body     string
}{
	{
		PsakKasSetaraKas, "Kas dan setara kas",
		"Kas dicatat sebesar nilai nominalnya. Setara kas adalah investasi jangka " +
			"pendek yang sangat likuid, mudah diubah menjadi kas dalam jumlah pasti, " +
			"dan memiliki risiko perubahan nilai yang tidak signifikan dengan jatuh " +
			"tempo tiga bulan atau kurang sejak tanggal perolehan (PSAK 207 par. 06-09).",
	},
	{
		PsakPiutang, "Piutang usaha dan piutang lainnya",
		"Piutang diakui sebesar nilai wajar tagihan dan dicatat sebesar jumlah yang " +
			"diharapkan dapat diterima. Penurunan nilai piutang diakui sebagai beban " +
			"pada saat timbulnya kerugian yang diharapkan.",
	},
	{
		PsakPersediaan, "Persediaan",
		"Persediaan dicatat sebesar harga perolehan dengan metode rata-rata tertimbang " +
			"dan disajikan sebesar nilai terendah antara harga perolehan dan nilai " +
			"realisasi bersih.",
	},
	{
		PsakAsetTetap, "Aset tetap",
		"Aset tetap dicatat sebesar harga perolehan dikurangi akumulasi penyusutan " +
			"dan penurunan nilai. Pengeluaran setelah perolehan diakui sebagai beban " +
			"kecuali bila meningkatkan manfaat ekonomik masa depan.",
	},
	{
		PsakAsetTetapAkum, "Penyusutan aset tetap",
		"Penyusutan dihitung dengan metode garis lurus selama masa manfaat aset, " +
			"dengan mempertimbangkan nilai residu yang diharapkan. Masa manfaat dan " +
			"nilai residu ditinjau pada akhir setiap periode pelaporan.",
	},
	{
		PsakAsetLainnya, "Aset lainnya",
		"Aset lainnya dicatat sebesar harga perolehan atau nilai wajarnya bila lebih " +
			"rendah, dan ditinjau bila terdapat indikasi penurunan nilai.",
	},
	{
		PsakUtangUsaha, "Utang usaha",
		"Utang usaha diakui sebesar kewajiban yang harus dibayar kepada pemasok dan " +
			"dicatat sebesar nilai wajar tagihan.",
	},
	{
		PsakUtangBank, "Pinjaman bank",
		"Pinjaman bank diakui pada saat diterima dan dicatat sebesar jumlah diterima " +
			"dikurangi biaya transaksi langsung. Bunga pinjaman diakui sebagai beban " +
			"selama periode berjalan dengan metode bunga efektif.",
	},
	{
		PsakPajak, "Utang pajak",
		"Utang pajak merupakan kewajiban perpajakan yang masih harus dibayar sampai " +
			"dengan tanggal pelaporan dan disajikan sebagai liabilitas jangka pendek.",
	},
	{
		PsakModal, "Modal disetor",
		"Modal disetor dicatat sebesar jumlah yang disetor pemilik kepada entitas dan " +
			"diungkapkan menurut kelasnya dalam Laporan Perubahan Ekuitas.",
	},
	{
		PsakSaldoLaba, "Saldo laba",
		"Saldo laba merupakan akumulasi laba periode berjalan dan periode sebelumnya " +
			"setelah dikurangi distribusi kepada pemilik. Laba periode berjalan " +
			"dipindahkan ke saldo laba melalui jurnal penutup pada saat periode ditutup.",
	},
	{
		PsakPendapatan, "Pendapatan",
		"Pendapatan diakui pada saat manfaat ekonomik masa depan yang terkait dengan " +
			"peningkatan aset atau penurunan liabilitas telah terjadi dan dapat diukur " +
			"secara andal, sebesar nilai wajar imbalan yang diterima atau yang akan " +
			"diterima.",
	},
	{
		PsakBebanOperasional, "Beban operasional",
		"Beban diakui pada saat terjadi penurunan manfaat ekonomik masa depan dan " +
			"disajikan menurut sifatnya masing-masing pada Laporan Laba Rugi.",
	},
	{
		PsakBebanKeuangan, "Beban keuangan",
		"Beban keuangan diakui sebagai beban periode berjalan dan disajikan sebagai " +
			"pos tersendiri dalam Laporan Laba Rugi (PSAK 201 par. 82 huruf b).",
	},
	{
		PsakBebanPajak, "Beban pajak penghasilan",
		"Beban pajak penghasilan periode berjalan disajikan sebagai pos tersendiri " +
			"dalam Laporan Laba Rugi (PSAK 201 par. 82 huruf d).",
	},
}

// PolicyFor mengembalikan kebijakan yang mengatur akun tertentu. Akun tanpa
// pemetaan pos dirujuk ke kebijakan umum catatan pertama.
func PolicyFor(psakLine string) (NotePolicy, bool) {
	for _, p := range notePolicyCatalog {
		if p.psakLine == psakLine {
			return NotePolicy{Anchor: "kebijakan-" + p.psakLine, Title: p.title, Body: p.body}, true
		}
	}
	return NotePolicy{}, false
}

// NotePolicyNumbers adalah hasil pemetaan psak_line ke nomor catatan kebijakan
// yang dipakai saat menyusun rincian akun.
type NotePolicyNumbers map[string]int

// BuildNotePolicies menentukan kebijakan mana yang disajikan (berdasarkan akun
// yang benar-benar ada) lalu memberinya nomor catatan berurutan.
func BuildNotePolicies(accounts []AccountTotal) ([]NotePolicy, NotePolicyNumbers) {
	present := map[string]bool{}
	for _, a := range accounts {
		if _, ok := PolicyFor(a.PsakLine); ok {
			present[a.PsakLine] = true
		}
	}
	var out []NotePolicy
	numbers := NotePolicyNumbers{}
	for _, p := range notePolicyCatalog {
		if !present[p.psakLine] {
			continue
		}
		number := len(out) + 1
		out = append(out, NotePolicy{
			Number: number,
			Anchor: "kebijakan-" + p.psakLine,
			Title:  p.title,
			Body:   p.body,
		})
		numbers[p.psakLine] = number
	}
	return out, numbers
}

// BuildNoteEstimates menyusun sumber ketidakpastian estimasi (par. 125) dari
// pos yang benar-benar ada di catatan entitas: penyusutan aset tetap,
// penyisihan piutang tak tertagih, dan nilai persediaan.
func BuildNoteEstimates(accounts []AccountTotal, numbers NotePolicyNumbers) []NoteEstimate {
	var fixedAsset, accum, receivable, inventory int64
	for _, a := range accounts {
		switch a.PsakLine {
		case PsakAsetTetap:
			fixedAsset += a.balanceSigned()
		case PsakAsetTetapAkum:
			accum += a.balanceSigned()
		case PsakPiutang:
			receivable += a.balanceSigned()
		case PsakPersediaan:
			inventory += a.balanceSigned()
		}
	}
	var out []NoteEstimate
	if fixedAsset != 0 {
		out = append(out, NoteEstimate{
			Title: "Estimasi masa manfaat dan nilai residu aset tetap",
			Nature: "Penyusutan aset tetap dihitung dengan asumsi masa manfaat dan " +
				"nilai residu. Bila asumsi tersebut berbeda dari kenyataan, jumlah " +
				"tercatat aset tetap dan beban penyusutan periode berikutnya perlu " +
				"disesuaikan.",
			AmountCents:  fixedAsset + accum,
			PolicyNumber: numbers[PsakAsetTetapAkum],
		})
	}
	if receivable != 0 {
		out = append(out, NoteEstimate{
			Title: "Estimasi piutang yang tidak dapat ditagih",
			Nature: "Jumlah tercatat piutang bergantung pada estimasi porsi tagihan " +
				"yang diharapkan dapat diterima. Perubahan asumsi atas kemampuan " +
				"piutang membayar akan mengubah jumlah tercatat piutang dan beban " +
				"kerugian piutang periode berikutnya.",
			AmountCents:  receivable,
			PolicyNumber: numbers[PsakPiutang],
		})
	}
	if inventory != 0 {
		out = append(out, NoteEstimate{
			Title: "Estimasi nilai realisasi bersih persediaan",
			Nature: "Persediaan disajikan sebesar nilai terendah antara harga perolehan " +
				"dan nilai realisasi bersih. Penilaian tersebut melibatkan estimasi " +
				"harga jual dan biaya penyelesaian di masa depan.",
			AmountCents:  inventory,
			PolicyNumber: numbers[PsakPersediaan],
		})
	}
	return out
}

// Build menyusun seluruh isi Catatan atas Laporan Keuangan dari data akun:
// saldo akun (periode berjalan dan pembanding), mutasi periode berjalan, dan
// Laporan Perubahan Ekuitas (untuk jumlah distribusi kepada pemilik).
func (v *NotesView) Build(balance, priorBalance, movements, priorMovements []AccountTotal, equity EquityStatement) {
	policies, numbers := BuildNotePolicies(append(append([]AccountTotal{}, balance...), movements...))
	v.Policies = policies
	v.Estimates = BuildNoteEstimates(balance, numbers)
	v.Groups = buildNoteGroups(balance, priorBalance, movements, priorMovements, numbers)
	v.Distributions = equity.Distributions()

	v.Compliance = "Laporan keuangan ini disusun dan disajikan menurut Standar " +
		"Akuntansi Keuangan (SAK) Indonesia, sebagaimana dimaksud PSAK 201 par. 16, " +
		"termasuk bagian penghasilan komprehensif lain (PSAK 201 par. 82A) yang " +
		"disajikan pada Laporan Laba Rugi dan Penghasilan Komprehensif Lain, serta " +
		"PSAK 207: Laporan Arus Kas."

	v.BasisNotes = []string{
		"Laporan keuangan disusun menurut dasar akrual (PSAK 201 par. 27-28): transaksi diakui pada saat terjadi dan dicatat dalam catatan akuntansi, bukan pada saat kas diterima atau dibayarkan.",
		"Laporan keuangan disusun dengan asumsi kelangsungan usaha (going concern, PSAK 201 par. 25-26).",
		"Mata uang pelaporan adalah " + v.BaseCurrency + " dan angka disajikan dalam " + v.RoundingLabel + " (PSAK 201 par. 49-53).",
		"Pengukuran sebagian besar pos dalam laporan keuangan didasarkan pada biaya perolehan historis.",
		"Informasi komparatif periode sebelumnya disajikan untuk seluruh laporan keuangan (PSAK 201 par. 38).",
		"Pos dianggap material dan dirinci dalam catatan ini bila saldo atau mutasinya tidak nol pada periode berjalan maupun periode pembanding (PSAK 201 par. 117-117E).",
	}

	v.EntityFacts = []string{
		"Nama entitas: " + v.CompanyName + ".",
		"Domisili dan alamat kantor pusat: " + entityOrEmpty(v.CompanyAddr) + ".",
		"NPWP: " + entityOrEmpty(v.CompanyNpwp) + ".",
		"Bentuk hukum, sifat operasi, dan kegiatan utama entitas: belum tersedia dalam data perusahaan dan perlu dilengkapi oleh manajemen (PSAK 201 par. 138).",
		v.ConsolidationLabel() + ".",
	}

	v.CapitalNote = "Tujuan entitas dalam mengelola modal adalah menjaga kelangsungan usaha " +
		"sehingga mampu memenuhi kewajibannya tepat waktu. Modal yang dikelola adalah " +
		"seluruh saldo ekuitas yang tercatat dalam laporan keuangan, sebagaimana " +
		"disajikan dalam Laporan Perubahan Ekuitas. Sampai dengan akhir periode " +
		"pelaporan, sepanjang yang tercatat dalam catatan entitas, tidak terdapat " +
		"persyaratan permodalan eksternal yang wajib dipenuhi (PSAK 201 par. 134-136)."
}

// entityOrEmpty menampilkan keterangan jujur bila data entitas belum diisi.
func entityOrEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return "belum dicatat dalam data perusahaan"
	}
	return s
}

// buildNoteGroups menyusun rincian per akun mengikuti urutan laporan posisi
// keuangan lalu laporan laba rugi (par. 114 huruf c), lengkap dengan referensi
// silang ke nomor kebijakan (par. 113).
func buildNoteGroups(balance, priorBalance, movements, priorMovements []AccountTotal, numbers NotePolicyNumbers) []NoteGroup {
	prior := map[string]AccountTotal{}
	for _, a := range priorBalance {
		prior[a.Code] = a
	}
	priorMov := map[string]AccountTotal{}
	for _, a := range priorMovements {
		priorMov[a.Code] = a
	}

	var groups []NoteGroup
	for _, sub := range []string{SubCurrentAsset, SubNonCurrentAsset, SubCurrentLiability, SubLongLiability, SubEquity} {
		g := NoteGroup{Label: SubTypeLabel(sub)}
		for _, a := range balance {
			if a.SubType != sub {
				continue
			}
			amount := a.balanceSigned()
			priorAmount := prior[a.Code].balanceSigned()
			if amount == 0 && priorAmount == 0 {
				continue
			}
			g.Lines = append(g.Lines, noteLine(a, amount, priorAmount, numbers))
			g.TotalCents += amount
			g.PriorTotalCents += priorAmount
		}
		if len(g.Lines) > 0 {
			groups = append(groups, g)
		}
	}

	for _, kind := range []struct {
		accountType string
		label       string
	}{
		{TypeRevenue, "Pendapatan periode berjalan"},
		{TypeExpense, "Beban periode berjalan"},
	} {
		g := NoteGroup{Label: kind.label}
		for _, a := range movements {
			if a.AccountType != kind.accountType {
				continue
			}
			// Angka ditampilkan menurut sisi normal akun (pendapatan dan
			// beban positif), konsisten dengan penyajian Laporan Laba Rugi.
			amount := a.balanceSigned()
			priorAmount := priorMov[a.Code].balanceSigned()
			if amount == 0 && priorAmount == 0 {
				continue
			}
			g.Lines = append(g.Lines, noteLine(a, amount, priorAmount, numbers))
			g.TotalCents += amount
			g.PriorTotalCents += priorAmount
		}
		if len(g.Lines) > 0 {
			groups = append(groups, g)
		}
	}
	return groups
}

// noteLine membentuk satu baris rincian akun beserta rujukan kebijakannya.
func noteLine(a AccountTotal, amount, priorAmount int64, numbers NotePolicyNumbers) NoteAccountLine {
	line := NoteAccountLine{
		Code:             a.Code,
		Name:             a.Name,
		AmountCents:      amount,
		PriorAmountCents: priorAmount,
	}
	if policy, ok := PolicyFor(a.PsakLine); ok {
		line.PolicyNumber = numbers[a.PsakLine]
		line.PolicyTitle = policy.Title
		line.PolicyAnchor = policy.Anchor
	}
	return line
}

package exchange

import (
	"bytes"
	"testing"
)

func contoh() *Table {
	t := NewTable("jurnal", []string{"Tanggal", "Referensi", "Memo", "Kode Akun", "Keterangan", "Debit", "Kredit"},
		[]bool{false, false, false, false, false, true, true})
	t.AddRow("01/01/2026", "JV-0001", "Saldo awal", "1-1010", "Bank BCA", "1500000.00", "0.00")
	t.AddRow("05/01/2026", "JV-0002", "Sewa diterima, bulan Januari", "1-1010", "Bank BCA", "250000.00", "0.00")
	t.AddRow("05/01/2026", "JV-0002", "Sewa diterima, bulan Januari", "4-4010", "Pendapatan Sewa", "0.00", "250000.00")
	t.AddRow("31/01/2026", "JV-0003", "Beban <uji> & tanda \"kutip\"", "2-2000", "Utang Usaha", "0.00", "125000.50")
	return t
}

// TestRoundTrip memastikan setiap format yang ditulis bisa dibaca kembali
// dengan nilai yang sama.
func TestRoundTrip(t *testing.T) {
	asli := contoh()
	for _, f := range AllFormats() {
		t.Run(f.Value, func(t *testing.T) {
			var buf bytes.Buffer
			if err := Write(&buf, asli, f.Value); err != nil {
				t.Fatalf("gagal menulis %s: %v", f.Value, err)
			}
			if buf.Len() == 0 {
				t.Fatalf("hasil %s kosong", f.Value)
			}
			hasil, err := Read(buf.Bytes(), "data."+f.Ext)
			if err != nil {
				t.Fatalf("gagal membaca %s: %v", f.Value, err)
			}
			if len(hasil.Header) != len(asli.Header) {
				t.Fatalf("jumlah kolom %d, ingin %d", len(hasil.Header), len(asli.Header))
			}
			for i, h := range asli.Header {
				if hasil.Header[i] != h {
					t.Errorf("judul kolom %d = %q, ingin %q", i, hasil.Header[i], h)
				}
			}
			if len(hasil.Rows) != len(asli.Rows) {
				t.Fatalf("jumlah baris %d, ingin %d", len(hasil.Rows), len(asli.Rows))
			}
			for r := range asli.Rows {
				for c := range asli.Header {
					if hasil.Rows[r][c] != asli.Rows[r][c] {
						t.Errorf("%s baris %d kolom %d = %q, ingin %q", f.Value, r, c, hasil.Rows[r][c], asli.Rows[r][c])
					}
				}
			}
		})
	}
}

// TestDeteksiFormat memastikan berkas tanpa nama ekstensi dikenali isinya.
func TestDeteksiFormat(t *testing.T) {
	asli := contoh()
	for _, f := range AllFormats() {
		var buf bytes.Buffer
		if err := Write(&buf, asli, f.Value); err != nil {
			t.Fatalf("gagal menulis %s: %v", f.Value, err)
		}
		if _, err := Read(buf.Bytes(), "tanpa-ekstensi"); err != nil {
			t.Errorf("deteksi isi untuk %s gagal: %v", f.Value, err)
		}
	}
}

// TestBacaCSVIndonesia memastikan pembacaan menerima format angka Indonesia
// (titik ribuan, koma desimal) dan tanggal dd/mm/yyyy.
func TestBacaCSVIndonesia(t *testing.T) {
	isi := "Tanggal;Referensi;Kode Akun;Debit;Kredit\n" +
		"01/01/2026;JV-9;1-1010;1.250.000,00;0,00\n"
	tab, err := Read([]byte(isi), "x.csv")
	if err != nil {
		t.Fatalf("gagal membaca: %v", err)
	}
	if tab.Kolom("Kode Akun") < 0 {
		t.Fatalf("kolom Kode Akun tidak dikenali: %v", tab.Header)
	}
	cents, err := ParseMoneyColumn(tab.Nilai(tab.Rows[0], "Debit"))
	if err != nil {
		t.Fatalf("gagal membaca angka: %v", err)
	}
	if cents != 125_000_000 {
		t.Fatalf("nilai debit %d sen, ingin 125000000", cents)
	}
	tgl, err := ParseTanggal(tab.Nilai(tab.Rows[0], "Tanggal"))
	if err != nil {
		t.Fatalf("gagal membaca tanggal: %v", err)
	}
	if tgl.Day() != 1 || int(tgl.Month()) != 1 || tgl.Year() != 2026 {
		t.Fatalf("tanggal terbaca %v", tgl)
	}
}

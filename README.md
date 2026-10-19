# Akunting

Akunting adalah sistem akuntansi berbasis web buatan PT Tegiva Jaya Sejahtera: aplikasi Go monolitik satu binary untuk pembukuan double-entry multi-perusahaan, lengkap dengan jurnal umum, buku besar, penyesuaian, laporan laba rugi dan neraca (PSAK 201, dengan pilar SAK EMKM menyusul), periode fiskal, kunci kurs valas, serta impor mutasi dan rekonsiliasi kas bank (selisih kurs otomatis menjadi jurnal penyesuaian). Aplikasi ini dirancang untuk di-hosting sendiri di jaringan Tailscale, dengan seluruh aset (HTML, CSS, font) tertanam di dalam binary.

## Prasyarat

1. Go 1.27 atau lebih baru.
2. PostgreSQL 15 atau lebih baru, terpasang lokal.
3. Tailscale, untuk mengakses layanan dari jaringan mesh (server hanya bind ke IP Tailscale).

## Cara Build

```bash
export PATH=$HOME/tools/go/bin:$HOME/go/bin:$HOME/.local/bin:$PATH GOCACHE=$HOME/.cache/go-build GOPATH=$HOME/go
go build -o bin/accountingd ./cmd/server
```

Jalankan migrasi (20 file, 0001 s/d 0020) lalu mulai server:

```bash
psql "postgres://aiagent@/accounting_dev?host=/var/run/postgresql" -v ON_ERROR_STOP=1 -f migrations/0001_init.sql
# ... ulangi untuk 0002 s/d 0020, atau pakai cmd/smoketest yang menjalankan semuanya
```

## Konfigurasi (env)

| Variabel | Keterangan |
|---|---|
| `DATABASE_URL` | Wajib. Koneksi PostgreSQL, mis. `postgres://aiagent@/accounting_dev?host=/var/run/postgresql`. |
| `LISTEN_ADDR` | Alamat bind, default `127.0.0.1:8080`. Untuk preview lokal: `127.0.0.1:5050`. |
| `COOKIE_SECURE` | Setel `1` saat di belakang HTTPS (cookie sesi memakai flag Secure). |
| `PUBLIC_BASE_URL` | Alamat publik aplikasi untuk menyusun link undangan; bila kosong, fallback ke `https://` + Host request. |

## Deployment di Tailscale

Server bind ke `LISTEN_ADDR` (mis. `127.0.0.1:5050`) lalu diekspos lewat `tailscale serve --bg --https=443`, sehingga HTTPS dijamin Tailscale, bukan aplikasi. Karena lalu lintas ke aplikasi masih HTTP lokal, setel `COOKIE_SECURE=1` agar cookie sesi tetap berflag Secure di browser. Kesehatan aplikasi ditandai `{"status":"ok"}` dari `/healthz`, yang mengecek koneksi DB dan keberadaan kolom skema kritis (migrasi 0014-0019); kolom hilang menghasilkan HTTP 500, bukan 200.

## Tautan

1. Repositori: https://github.com/PT-Tegiva-Jaya-Sejahtera/akunting
2. Domain perusahaan: https://tegiva.id

## Lisensi

AGPL-3.0, lihat [LICENSE](LICENSE). Copyright PT Tegiva Jaya Sejahtera.

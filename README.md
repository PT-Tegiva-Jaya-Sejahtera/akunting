# Akunting

Akunting adalah sistem akuntansi berbasis web buatan PT Tegiva Jaya Sejahtera: aplikasi Go monolitik satu binary untuk pembukuan double-entry multi-perusahaan, lengkap dengan jurnal umum, buku besar, penyesuaian, laporan laba rugi dan neraca (PSAK 201, dengan pilar SAK EMKM menyusul), periode fiskal, serta kunci kurs valas. Aplikasi ini dirancang untuk di-hosting sendiri di jaringan Tailscale, dengan seluruh aset (HTML, CSS, font) tertanam di dalam binary.

## Prasyarat

1. Go 1.27 atau lebih baru.
2. PostgreSQL 15 atau lebih baru, terpasang lokal.
3. Tailscale, untuk mengakses layanan dari jaringan mesh (server hanya bind ke IP Tailscale).

## Cara Build

```bash
export PATH=$HOME/tools/go/bin:$HOME/go/bin:$HOME/.local/bin:$PATH GOCACHE=$HOME/.cache/go-build GOPATH=$HOME/go
go build -o bin/server ./cmd/server
```

Jalankan migrasi (13 file, 0001 s/d 0013) lalu mulai server:

```bash
psql "postgres://aiagent@/accounting_dev?host=/var/run/postgresql" -v ON_ERROR_STOP=1 -f migrations/0001_init.sql
# ... ulangi untuk 0002 s/d 0013, atau pakai cmd/smoketest yang menjalankan semuanya
DATABASE_URL="postgres://aiagent@/accounting_dev?host=/var/run/postgresql" LISTEN_ADDR="127.0.0.1:5050" ./bin/server
```

## Tautan

1. Repositori: https://github.com/PT-Tegiva-Jaya-Sejahtera/akunting
2. Domain perusahaan: https://tegiva.id

## Lisensi

AGPL-3.0, lihat [LICENSE](LICENSE). Copyright PT Tegiva Jaya Sejahtera.

# Quickstart Akunting (Linux)

Tiga langkah dari nol sampai aplikasi jalan sebagai service dan tersedia lewat HTTPS Tailscale.

## Prasyarat

1. Linux + Go (https://go.dev/dl) + PostgreSQL (server dan `psql`) + curl.
2. Role PostgreSQL untuk user Linux kamu yang boleh membuat database (contoh: `sudo -u postgres psql -c "CREATE ROLE $USER LOGIN SUPERUSER"`).
3. Opsional untuk HTTPS: Tailscale terpasang dan sudah `sudo tailscale up`, dengan MagicDNS + HTTPS certificates aktif di admin console tailnet.

## Jalankan

```bash
git clone https://github.com/PT-Tegiva-Jaya-Sejahtera/akunting.git
cd akunting
scripts/quickstart.sh
```

Script mengerjakan semua: buat database `akunting`, terapkan seluruh migrasi berurutan, build binary `bin/akunting`, tulis konfigurasi di `~/.config/akunting/akunting.env`, pasang dan menyalakan systemd user unit `akunting.service` (auto-restart), mengaktifkan `tailscale serve` (HTTPS), lalu menutup dengan health check. URL final dicetak di akhir.

## Variasi

1. Port dan nama database: `AKUNTING_PORT=9000 AKUNTING_DB=akunting_cabang scripts/quickstart.sh`
2. Port HTTPS Tailscale (bila 443 sudah dipakai layanan lain): `AKUNTING_TS_PORT=8443 scripts/quickstart.sh`
3. Tanpa Tailscale (HTTP lokal saja): `scripts/quickstart.sh --no-tailscale`
4. Tanpa systemd (mis. di container tanpa systemd): `scripts/quickstart.sh --no-systemd --no-tailscale` lalu jalankan `bin/akunting` manual dengan env dari `~/.config/akunting/akunting.env`.

## Perawatan

1. Update aplikasi: `git pull` lalu jalankan ulang `scripts/quickstart.sh` (idempoten: migrasi baru saja yang dijalankan, binary dibangun ulang, service di-restart).
2. Log: `journalctl --user -u akunting -f`
3. Restart/berhenti: `systemctl --user restart akunting` / `systemctl --user stop akunting`
4. Supaya service tetap hidup setelah logout: `sudo loginctl enable-linger $USER`

## Catatan keamanan

1. Aplikasi hanya bind ke `127.0.0.1`; satu-satunya pintu publik adalah `tailscale serve` (HTTPS, hanya terlihat di tailnet kamu).
2. `COOKIE_SECURE=true` otomatis saat ekspos lewat Tailscale; bila kamu memakai HTTP lokal saja, cookie sesi tidak ditandai Secure.

## Troubleshooting

1. `database ... gagal dibuat`: role PostgreSQL belum ada atau tidak boleh createdb, lihat prasyarat no. 2.
2. `/healthz tidak kunjung hijau`: lihat log `journalctl --user -u akunting -e`; penyebab umum `DATABASE_URL` salah atau PostgreSQL belum berjalan.
3. `tailscale serve https:443 sudah dipakai`: port 443 sudah melayani aplikasi lain; ulangi dengan `AKUNTING_TS_PORT=<port lain>` atau tambahkan serve manual sesuai petunjuk yang dicetak script.

#!/usr/bin/env bash
# Quickstart Akunting (Linux): dari nol sampai jalan sebagai service dan
# tersedia lewat HTTPS Tailscale.
#
#   scripts/quickstart.sh                 # jalur standar
#   AKUNTING_PORT=8090 scripts/quickstart.sh
#   scripts/quickstart.sh --no-tailscale  # tanpa ekspos Tailscale
#
# Yang dikerjakan (idempoten, aman diulang untuk update):
#   1. cek prasyarat (go, psql, curl, tailscale opsional)
#   2. buat database akunting bila belum ada + jalankan migrasi berurutan
#   3. build binary bin/akunting
#   4. tulis konfigurasi ~/.config/akunting/akunting.env
#   5. pasang + nyalakan systemd user unit akunting.service
#   6. tailscale serve (HTTPS) bila tersedia dan portnya belum terpakai
#   7. health check /healthz dan cetak ringkasan
#
# Konfigurasi lewat env/flag:
#   AKUNTING_DB       nama database           (default akunting)
#   AKUNTING_PORT     port listen lokal       (default 8080)
#   AKUNTING_TS_PORT  port HTTPS Tailscale    (default 443)
set -euo pipefail

DB_NAME="${AKUNTING_DB:-akunting}"
APP_PORT="${AKUNTING_PORT:-8080}"
TS_PORT="${AKUNTING_TS_PORT:-443}"
NO_TS=0
NO_SYSTEMD=0

usage() { sed -n '2,30p' "$0"; exit 0; }
for arg in "$@"; do
    case "$arg" in
        --no-tailscale) NO_TS=1 ;;
        --no-systemd) NO_SYSTEMD=1 ;;
        --help|-h) usage ;;
        *) echo "argumen tidak dikenal: $arg (--help untuk bantuan)" >&2; exit 1 ;;
    esac
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT"

# Lokasi umum Go/dll di luar PATH default (mis. go install atau tarball resmi).
for p in /usr/local/go/bin "$HOME/.local/bin" "$HOME/Resources/tools/go/bin" "$HOME/Resources/go/bin"; do
    if [ -d "$p" ] && ! echo ":$PATH:" | grep -q ":$p:"; then
        PATH="$PATH:$p"
    fi
done
export PATH

say()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
ok()   { printf '\033[1;32m OK\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m !!\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31m xx\033[0m %s\n' "$*" >&2; exit 1; }

# ===== 1. Prasyarat =====
command -v go   >/dev/null || die "go tidak ditemukan di PATH (https://go.dev/dl)."
command -v psql >/dev/null || die "psql tidak ditemukan di PATH (instal postgresql-client)."
command -v curl >/dev/null || die "curl tidak ditemukan di PATH."
have_ts=1
if ! command -v tailscale >/dev/null; then
    have_ts=0; warn "tailscale tidak ditemukan, langkah ekspos HTTPS dilewati."
elif [ "$NO_TS" = 1 ]; then
    have_ts=0; warn "ekspos Tailscale dimatikan (--no-tailscale)."
elif ! tailscale status >/dev/null 2>&1; then
    have_ts=0; warn "tailscale terpasang tapi belum login/berjalan: 'sudo tailscale up' dulu."
fi

# DSN: pakai DATABASE_URL bila disetel (jalur lanjutan), kalau tidak socket
# lokal milik user saat ini (peer auth).
DSN="${DATABASE_URL:-postgres://${USER}@/${DB_NAME}?host=/var/run/postgresql}"
ADMIN_DSN="${DATABASE_URL:-postgres://${USER}@/postgres?host=/var/run/postgresql}"

# ===== 2. Database + migrasi =====
say "database: $DB_NAME"
if ! psql "$ADMIN_DSN" -tAc "SELECT 1 FROM pg_database WHERE datname='$DB_NAME'" | grep -q 1; then
    psql "$ADMIN_DSN" -c "CREATE DATABASE $DB_NAME" \
        || die "gagal membuat database. Pastikan role PostgreSQL '$USER' ada dan boleh createdb (contoh: sudo -u postgres psql -c \"CREATE ROLE $USER LOGIN SUPERUSER\")."
    ok "database $DB_NAME dibuat"
else
    ok "database $DB_NAME sudah ada"
fi

psql "$DSN" -v ON_ERROR_STOP=1 -c "
CREATE TABLE IF NOT EXISTS _akunting_migrations (
    filename   TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
)" >/dev/null

n_new=0
for f in "$ROOT"/migrations/*.sql; do
    name="$(basename "$f")"
    if [ "$(psql "$DSN" -tAc "SELECT count(*) FROM _akunting_migrations WHERE filename='$name'")" = "0" ]; then
        psql "$DSN" -v ON_ERROR_STOP=1 -q -f "$f" \
            || die "migrasi $name gagal diterapkan."
        psql "$DSN" -v ON_ERROR_STOP=1 -q -c "INSERT INTO _akunting_migrations (filename) VALUES ('$name')"
        n_new=$((n_new + 1))
    fi
done
ok "migrasi: $n_new diterapkan, sisanya sudah pernah dijalankan"

# ===== 3. Tailscale (dikumpulkan dulu untuk PUBLIC_BASE_URL) =====
TS_DNS=""
ts_busy=0
if [ "$have_ts" = 1 ]; then
    TS_DNS="$(tailscale status --json | sed -n 's/.*"DNSName": *"\([^"]*\)".*/\1/p' | head -1 | sed 's/\.$//')"
    [ -n "$TS_DNS" ] || TS_DNS="$(tailscale ip -4 2>/dev/null | head -1)"
    serve_status="$(tailscale serve status --json 2>/dev/null || true)"
    if echo "$serve_status" | grep -qF "\"${TS_PORT}\":"; then
        # Port sudah dipakai entri serve (kemungkinan jalan pertama script ini,
        # atau layanan lain). Konfigurasi HTTPS tetap; serve tidak diubah.
        ts_busy=1
        warn "tailscale serve https:$TS_PORT sudah terpasang, tidak diubah."
        warn "port lain: AKUNTING_TS_PORT=8443 $0"
    fi
fi
PUBLIC_BASE_URL="https://$TS_DNS"
COOKIE_SECURE=true
if [ -z "$TS_DNS" ]; then
    PUBLIC_BASE_URL=""
    COOKIE_SECURE=false
    [ "$NO_TS" = 1 ] || warn "PUBLIC_BASE_URL kosong: aplikasi jalan di HTTP lokal (loopback) saja."
fi

# ===== 4. Build =====
say "build bin/akunting"
go build -o bin/akunting ./cmd/server || die "build gagal."
ok "binary siap: $ROOT/bin/akunting"

# ===== 5. Konfigurasi + service =====
ENV_FILE="$HOME/.config/akunting/akunting.env"
mkdir -p "$(dirname "$ENV_FILE")"
cat > "$ENV_FILE" <<EOF
LISTEN_ADDR=127.0.0.1:$APP_PORT
DATABASE_URL=$DSN
COOKIE_SECURE=$COOKIE_SECURE
PUBLIC_BASE_URL=$PUBLIC_BASE_URL
EOF
chmod 600 "$ENV_FILE"
ok "konfigurasi ditulis: $ENV_FILE"

if [ "$NO_SYSTEMD" = 0 ]; then
    UNIT_DIR="$HOME/.config/systemd/user"
    mkdir -p "$UNIT_DIR"
    cat > "$UNIT_DIR/akunting.service" <<EOF
[Unit]
Description=Akunting (Go monolith, port $APP_PORT loopback via tailscale serve)
After=network-online.target postgresql.service
Wants=network-online.target

[Service]
WorkingDirectory=$ROOT
EnvironmentFile=%h/.config/akunting/akunting.env
ExecStart=$ROOT/bin/akunting
Restart=always
RestartSec=3

[Install]
WantedBy=default.target
EOF
    systemctl --user daemon-reload
    systemctl --user enable --now akunting.service >/dev/null 2>&1 || true
    systemctl --user is-active --quiet akunting.service || die "akunting.service gagal jalan: journalctl --user -u akunting -e"
    ok "service aktif: akunting.service (auto-restart)"
    loginctl enable-linger "$USER" 2>/dev/null \
        || warn "lingering belum aktif; agar service tetap hidup setelah logout: sudo loginctl enable-linger $USER"
fi

# ===== 6. Ekspos Tailscale =====
if [ "$have_ts" = 1 ] && [ -n "$TS_DNS" ] && [ "$ts_busy" = 0 ]; then
    if tailscale serve --bg --https="$TS_PORT" "http://127.0.0.1:$APP_PORT" >/dev/null 2>&1; then
        ok "tailscale serve: https://$TS_DNS:$TS_PORT -> http://127.0.0.1:$APP_PORT"
    else
        warn "tailscale serve gagal dijalankan; cek 'tailscale serve status'."
    fi
fi

# ===== 7. Health check =====
say "health check"
for _ in $(seq 1 15); do
    if curl -fsS "http://127.0.0.1:$APP_PORT/healthz" >/dev/null 2>&1; then break; fi
    sleep 1
done
curl -fsS "http://127.0.0.1:$APP_PORT/healthz" >/dev/null || die "/healthz tidak kunjung hijau: journalctl --user -u akunting -e"

echo
echo "  Aplikasi akunting siap."
echo "    Lokal    : http://127.0.0.1:$APP_PORT"
if [ -n "$TS_DNS" ]; then
    echo "    Tailnet  : https://$TS_DNS:$TS_PORT (HTTPS)"
fi
echo "    Database : $DB_NAME"
echo "    Service  : systemctl --user status|restart|stop akunting ; log: journalctl --user -u akunting -f"
echo
echo "  Langkah pertama: buka URL di atas; bila belum ada pengguna, aplikasi"
echo "  mengarahkan ke halaman Buat Akun Pertama (/setup). Akun pertama membuat"
echo "  pembukuan lewat kartu 'Tambah pembukuan baru', lalu mengundang pengguna"
echo "  lain dari Pengaturan perusahaan dan mengimpor data dari Pertukaran Data."

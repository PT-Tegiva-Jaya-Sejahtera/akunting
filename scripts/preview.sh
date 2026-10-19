#!/usr/bin/env bash
# Preview Akunting tanpa daemon: menyalakan/mematikan binary secara
# temporer, tanpa systemd. Dipakai saat menguji instalasi baru lewat
# halaman setup (roleplay pengguna pertama) atau mencoba perubahan kode.
#
#   scripts/preview.sh start    # jalankan di 127.0.0.1:8090 (DB akunting)
#   scripts/preview.sh stop     # matikan
#   scripts/preview.sh status   # cek
#
# Port default 8090 = yang diproxy tailscale serve (https://443 dan :5050
# keduanya diarahkan ke 127.0.0.1:8090), jadi URL HTTPS publik tetap hidup
# selama preview jalan. Port dan DB bisa ditimpa: AKUNTING_PORT=8080 ...
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PID_FILE="$ROOT/var/preview.pid"
LOG_FILE="$ROOT/var/preview.log"
BIN="$ROOT/bin/akunting"
PORT="${AKUNTING_PORT:-8090}"
DB="${AKUNTING_DB:-akunting}"
PUBLIC_URL="${AKUNTING_PUBLIC_URL:-https://vps.lionfish-spica.ts.net}"

case "${1:-}" in
  start)
    if [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
        echo "preview sudah jalan (pid $(cat "$PID_FILE"))."
        exit 0
    fi
    if ss -tln 2>/dev/null | grep -q "127.0.0.1:$PORT "; then
        echo "port $PORT sudah terpakai proses lain. Matikan itu dulu atau pakai AKUNTING_PORT=<lain>."
        exit 1
    fi
    [ -x "$BIN" ] || { echo "binary $BIN belum ada. Jalankan: go build -o bin/akunting ./cmd/server"; exit 1; }
    mkdir -p "$ROOT/var"
    nohup env \
        LISTEN_ADDR="127.0.0.1:$PORT" \
        DATABASE_URL="postgres://${USER}@/${DB}?host=/var/run/postgresql" \
        COOKIE_SECURE=true \
        PUBLIC_BASE_URL="$PUBLIC_URL" \
        "$BIN" >>"$LOG_FILE" 2>&1 &
    echo $! > "$PID_FILE"
    for _ in $(seq 1 15); do
        curl -fsS "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1 && break
        sleep 1
    done
    if ! curl -fsS "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1; then
        echo "gagal start. Log: $LOG_FILE"
        exit 1
    fi
    echo "preview jalan: http://127.0.0.1:$PORT (pid $(cat "$PID_FILE"), DB $DB)"
    echo "matikan: scripts/preview.sh stop  |  log: tail -f $LOG_FILE"
    ;;
  stop)
    if [ -f "$PID_FILE" ] && kill "$(cat "$PID_FILE")" 2>/dev/null; then
        rm -f "$PID_FILE"
        echo "preview dimatikan."
    else
        rm -f "$PID_FILE"
        echo "preview tidak sedang jalan."
    fi
    ;;
  status)
    if [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
        echo "preview jalan (pid $(cat "$PID_FILE")): http://127.0.0.1:$PORT"
    else
        echo "preview tidak jalan."
    fi
    ;;
  *)
    echo "pakai: $0 start|stop|status"
    exit 1
    ;;
esac

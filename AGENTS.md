# AGENTS.md — akunting (aturan umum semua agent)

1. App Go monolit akuntansi double-entry multi-company di /home/aiagent/Projects/akunting.
2. Toolchain Go di luar PATH. Export dulu: `export PATH=$HOME/tools/go/bin:$HOME/go/bin:$HOME/.local/bin:$PATH`.
3. Build: `go build -o /tmp/accountingd ./cmd/server`. Test: `go test ./...`. Smoketest mandiri: `go run ./cmd/smoketest` (buat lalu drop DB smoke sendiri).
4. Migrasi SQL di `migrations/` (0001 s/d 0020), dijalankan manual via psql.
5. Larangan: tanpa sudo. Jangan sentuh `~/.hermes`, `.worktrees/` task lain, atau restart `akunting.service` tanpa perintah. Data properti/tenant/klien hanya di repo private atau D1/KV/R2, bukan di repo kode ini.

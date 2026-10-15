# IDEA: Self-Hosted, Single-Binary Multi-Company Accounting System

## Overview
A private, self-hosted, multi-company double-entry accounting web application built as a monolithic Go service. Designed for local Linux environments, accessible exclusively via a secure Tailscale mesh network, and packaged into a single deployable binary.

---

## Architecture & Technology Stack

### Infrastructure & Networking
- **Host Environment:** Linux (Ubuntu or Debian) local PC or server.
- **Networking:** Tailscale peer-to-peer mesh. The web server binds strictly to the Tailscale network interface IP (e.g., `100.x.x.x`), remaining completely invisible to the public internet and local LAN.
- **Backups:** Built-in background worker using a Go cron scheduler (`robfig/cron`) to trigger automated `pg_dump` routines, encrypt the outputs, and push them to S3-compatible object storage (e.g., AWS S3 or Cloudflare R2).

### Backend
- **Language:** Go (Golang), compiled into a single static binary containing all business logic, HTTP routes, and embedded assets.
- **Web Framework:** `go-chi/chi` for a lightweight, idiomatic routing layer compatible with the standard library.
- **Database:** PostgreSQL installed natively.
- **Database Driver & Tooling:** `jackc/pgx` for PostgreSQL connectivity and `sqlc` to generate type-safe Go code directly from pure SQL queries.
- **Authentication:** Secure, HTTP-only cookie-based session management.

### Frontend
- **Templating:** `a-h/templ` (Go-native HTML templating language providing compile-time type safety).
- **Interactivity:** HTMX for partial UI updates and seamless client-server interactions without full page reloads.
- **Styling:** Tailwind CSS, compiled via CLI and embedded directly into the Go binary using `go:embed`.

---

## Core System Requirements

1. **Multi-Tenancy:** 
   - A single database instance using a shared schema approach.
   - Every core transactional table must include a `company_id` foreign key.
   - All database queries must be strictly scoped to the active company context found in the user's active session.

2. **Role-Based Access Control (RBAC):**
   - Users can belong to one or more companies.
   - Roles (e.g., `admin`, `accountant`, `read_only`) are scoped per company via a junction table (`company_users`).

3. **Double-Entry Accounting Ledger:**
   - Strict double-entry rules enforced at the database level.
   - Core accounting models: `accounts` (Chart of Accounts), `journals` (transaction headers), and `ledger_entries` (line items where total debits must equal total credits).

4. **Zero External Runtime Dependencies:**
   - No external Node.js, Python, or heavy frontend build tools are required on the production server. Everything runs through the compiled Go binary.

---

## Project Structure (Standard Go Layout)

```text
.
├── cmd
│   └── server
│       └── main.go          # Application entrypoint
├── internal
│   ├── auth                 # Session and authentication logic
│   ├── config               # Environment and configuration loading
│   ├── database             # Database connection setup & sqlc generated code
│   ├── handlers             # HTTP handlers (Chi routers, HTMX controllers)
│   ├── middleware           # Auth, tenant scoping, and logging middleware
│   ├── models               # Domain structs and business logic helpers
│   └── worker               # Background cron jobs (e.g., S3 backups)
├── migrations               # Raw PostgreSQL migration SQL files
├── static                   # Compiled Tailwind CSS and static assets
├── templates                # Templ component files (.templ)
├── sqlc.yaml                # sqlc configuration file
├── go.mod
└── go.sum

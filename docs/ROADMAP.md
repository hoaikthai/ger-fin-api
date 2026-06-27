# GerFin API — Roadmap

> Personal finance REST API built in Go. This document tracks implemented features, planned work, and future ideas.

---

## Legend

- `[x]` — Done
- `[ ]` — Planned / Not started
- `[~]` — Partially done

---

## Phase 1 — Core Foundation ✅

> Authentication, accounts, transactions, categories, recurring transactions.

- [x] User registration & login (JWT)
- [x] User profile (GET/PATCH `/users/me`)
- [x] Account management (CRUD, soft delete)
- [x] Running account balance (auto-updated on transaction create/update/delete)
- [x] Category management (CRUD, two-level hierarchy)
- [x] Default category seeding (idempotent on startup)
- [x] Transaction tracking (income/expense)
- [x] Transfer transactions (two-leg, linked by `relatedTransactionId`)
- [x] Transaction filtering (period, type, accountId, categoryId)
- [x] Transaction summary (aggregate totals by period)
- [x] CSV export (`GET /transactions/export`)
- [x] Recurring transactions (daily/weekly/monthly/yearly via cron scheduler)
- [x] Swagger/OpenAPI documentation
- [x] Versioned SQL migrations
- [x] Docker Compose local dev setup
- [x] Integration test suite (real DB via testutil)

---

## Phase 2 — Reporting & Analytics

> Make the data useful — summaries, trends, breakdowns.

- [x] **Category breakdown report** — income/expense split by category for a period
- [x] **Monthly trend report** — income vs expense by month, zero-filled for empty months
- [x] **Net worth snapshot** — sum of all active account balances
- [x] **Cash flow report** — projected vs actual cash flow for a period
- [x] **Top spending categories** — ranked by amount for a period (`limit` param, default 5)
- [x] **Year-over-year comparison** — compare the same period across years
- [x] **Custom date range filtering** — `from`/`to` on all report endpoints (default: current month)

---

## Phase 2.5 — User Preferences & Settings ✅

> Per-user configuration store. Unlocks persistent defaults for reporting (Phase 2), base currency (Phase 5), and notification thresholds (Phase 7).

- [x] **User settings model** — typed columns per user (`userId`, auto-created on first access)
- [x] **Settings CRUD** (`GET /users/settings`, `PATCH /users/settings`)
- [x] **Reporting defaults** — `cashFlowMonths` and `yearOverYearYears` used as fallback when query param is omitted
- [x] **Base currency preference** — placeholder field (`baseCurrency`, default `USD`), activated in Phase 5
- [x] **Default account** — optional `defaultAccountId` field for quick-add flows

---

## Phase 3 — Budget Planning

> Set spending limits and track progress against them.

- [ ] **Budget model** — amount cap per category per period (monthly default)
- [ ] **Budget CRUD** (`POST/GET/PATCH/DELETE /budgets`)
- [ ] **Budget progress** — actual spend vs budget cap, percent used
- [ ] **Over-budget alert flag** — API response field indicating exceeded budget
- [ ] **Budget rollover** — optionally carry unspent budget to next period

---

## Phase 4 — Financial Goals

> Track savings targets and long-term objectives.

- [ ] **Goal model** — target amount, target date, linked account (optional)
- [ ] **Goal CRUD** (`POST/GET/PATCH/DELETE /goals`)
- [ ] **Goal progress** — current saved vs target, projected completion date
- [ ] **Contribution tracking** — log deposits toward a goal
- [ ] **Goal completion** — mark done when target reached

---

## Phase 5 — Multi-Currency Support

> Handle accounts and transactions in different currencies.

- [ ] **Exchange rate store** — fetch & cache rates (e.g. via ECB or Open Exchange Rates)
- [ ] **Base currency config** — per-user preferred display currency
- [ ] **Balance normalization** — convert all account balances to base currency for net worth
- [ ] **Transaction currency field** — record original currency + amount alongside base amount
- [ ] **Rate history** — store historical rates used at transaction time

---

## Phase 6 — Import / Export

> Move data in and out of the system.

- [x] CSV export (transactions)
- [ ] **CSV import** — parse and bulk-insert transactions from uploaded file
- [ ] **Excel export** — `.xlsx` format with multiple sheets (transactions, summary, category breakdown)
- [ ] **Excel import** — parse `.xlsx` uploads
- [ ] **Import preview** — dry-run endpoint that returns parsed rows without committing
- [ ] **Import error report** — per-row validation errors returned in response

---

## Phase 7 — Notifications & Reminders

> Proactive alerts for budgets, goals, and recurring events.

- [ ] **Notification model** — type, message, read/unread, userId
- [ ] **In-app notification list** (`GET /notifications`, mark-read, clear)
- [ ] **Budget exceeded notification** — triggered when transaction pushes spend over cap
- [ ] **Recurring transaction reminder** — N days before a scheduled charge
- [ ] **Goal milestone notification** — at 50%, 90%, and 100% of target
- [ ] **Low balance alert** — configurable threshold per account

---

## Phase 8 — Tags & Advanced Filtering

> Fine-grained labeling beyond categories.

- [ ] **Tag model** — free-form labels on transactions
- [ ] **Tag CRUD** (`POST/GET/PATCH/DELETE /tags`)
- [ ] **Attach tags to transactions** — many-to-many
- [ ] **Filter transactions by tag**
- [ ] **Spending by tag report**

---

## Phase 9 — Security & Hardening

- [ ] **Refresh tokens** — short-lived access token + long-lived refresh token flow
- [ ] **Token revocation** — logout invalidates the refresh token
- [ ] **Rate limiting** — per-IP and per-user on auth endpoints
- [ ] **Password change endpoint** (`PATCH /users/me/password`)
- [ ] **Password reset flow** — email-based reset token
- [ ] **Audit log** — immutable record of create/update/delete events per user

---

## Phase 10 — Developer Experience & Ops

- [ ] **Health check endpoint** (`GET /health` — db connectivity, scheduler status)
- [ ] **Structured JSON logging** — replace plain log with slog/zerolog
- [ ] **OpenTelemetry tracing** — trace IDs on all requests
- [ ] **Prometheus metrics endpoint** (`/metrics` — request count, latency, db pool stats)
- [ ] **CI pipeline** — lint + test on PR (GitHub Actions)
- [ ] **Docker multi-stage build** — slim production image
- [ ] **Database connection pooling config** — expose pool size via env vars
- [ ] **Config validation on startup** — fail fast if required env vars missing

---

## Backlog / Ideas

> Unscheduled ideas — evaluate before committing to a phase.

- **Shared access** — let a user grant read-only access to another user (household finance)
- **Webhooks** — POST to a user-configured URL on transaction events
- **Mobile push notifications** — Firebase/APNs integration
- **Account aggregation** — read-only import from bank APIs (Open Banking / Plaid)
- **Admin panel API** — user listing, stats, manual overrides (ops use)
- **Dark mode for Swagger** — trivial but nice
- **GraphQL endpoint** — alternative to REST for richer client queries
- **Data archival** — move old soft-deleted records to cold storage after N months

---

## Current Status

| Phase | Status | Notes |
|-------|--------|-------|
| 1 — Core Foundation | ✅ Done | All CRUD, scheduler, tests |
| 2 — Reporting & Analytics | ✅ Done | All 7 reports complete |
| 2.5 — User Preferences & Settings | ✅ Done | Settings CRUD, reporting defaults wired in |
| 3 — Budget Planning | 🔲 Planned | New domain |
| 4 — Financial Goals | 🔲 Planned | New domain |
| 5 — Multi-Currency | 🔲 Planned | Needs external rate source |
| 6 — Import / Export | 🟡 Partial | CSV export done; import pending |
| 7 — Notifications | 🔲 Planned | New domain |
| 8 — Tags | 🔲 Planned | New domain |
| 9 — Security & Hardening | 🔲 Planned | Refresh tokens are highest priority |
| 10 — DX & Ops | 🔲 Planned | Health check is quick win |

# Phase 5 — Multi-Currency Support: Design Spec

**Date:** 2026-06-27
**Status:** Approved

---

## Goal

Allow accounts and transactions to exist in different currencies. Automatically convert balances and amounts to each user's preferred base currency for display in reports and responses.

---

## Scope

| Area | Change |
|---|---|
| New `internal/currency/` package | Exchange rate model, service, scheduler, handler |
| `exchangeRate` DB table | Stores latest rates fetched from Open Exchange Rates |
| `transaction` table | Two new nullable columns: `originalCurrency`, `originalAmount` |
| Config | New `OXR_APP_ID` env var |
| Account responses | Add computed `baseCurrencyBalance` field |
| Transaction responses | Add computed `baseAmount` field |
| Net worth report | Normalize all account balances to base currency |
| Category breakdown report | Normalize transaction amounts to base currency |
| Monthly trend report | Normalize income/expense totals to base currency |
| New API endpoints | `GET /currencies/rates`, `POST /currencies/rates/refresh` |

---

## Architecture

### Exchange Rate Provider

**Open Exchange Rates** (free plan, USD base). API endpoint:
```
GET https://openexchangerates.org/api/latest.json?app_id={OXR_APP_ID}
```
Response: `{ "base": "USD", "rates": { "EUR": 0.92, "VND": 25400, ... } }`

Rates are USD-denominated: `1 USD = rate * targetCurrency`.

**Conversion formula** (from currency A to currency B, both non-USD):
```
result = amount / rates[A] * rates[B]
```
Special cases:
- A == USD: `result = amount * rates[B]`
- B == USD: `result = amount / rates[A]`
- A == B: `result = amount`

### New Package: `internal/currency/`

```
internal/currency/
  model.go       — ExchangeRate struct
  service.go     — FetchAndStoreRates(), Convert(), GetLatestRates()
  scheduler.go   — hourly cron job
  handler.go     — GET /currencies/rates, POST /currencies/rates/refresh
  dto.go         — response types
```

### Dependency Injection

`currency.Service` is injected into:
- `account.Handler` (for `baseCurrencyBalance`)
- `transaction.Handler` (for `baseAmount`)
- `report.Service` (for net worth, category breakdown, monthly trend)
- `user.Service` is already injected into `report.Handler` for `baseCurrency` lookup

---

## Data Model

### `exchangeRate` Table (new)

```sql
CREATE TABLE "exchangeRate" (
  "id"             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  "baseCurrency"   VARCHAR(3)    NOT NULL,
  "targetCurrency" VARCHAR(3)    NOT NULL,
  "rate"           NUMERIC(20,8) NOT NULL,
  "fetchedAt"      TIMESTAMPTZ   NOT NULL,
  "createdAt"      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
  "updatedAt"      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
  "deletedAt"      TIMESTAMPTZ
);
CREATE UNIQUE INDEX ON "exchangeRate" ("baseCurrency", "targetCurrency")
  WHERE "deletedAt" IS NULL;
```

`baseCurrency` is always `"USD"` for the OXR free plan. On each refresh, all rows are upserted (update `rate` and `fetchedAt` if (base, target) exists).

### `transaction` Table (modified)

Two nullable columns added (migration 007):
```sql
ALTER TABLE "transaction"
  ADD COLUMN "originalCurrency" VARCHAR(3),
  ADD COLUMN "originalAmount"   NUMERIC(15,2);
```

`originalCurrency` / `originalAmount` record what the user typed. The existing `amount` field remains in the account's currency and drives all balance math — no change.

### Go Model: `ExchangeRate`

```go
type ExchangeRate struct {
    common.BaseModel
    BaseCurrency   string    `gorm:"column:baseCurrency;type:varchar(3);not null"`
    TargetCurrency string    `gorm:"column:targetCurrency;type:varchar(3);not null"`
    Rate           float64   `gorm:"column:rate;type:numeric(20,8);not null"`
    FetchedAt      time.Time `gorm:"column:fetchedAt;not null"`
}
func (ExchangeRate) TableName() string { return "exchangeRate" }
```

---

## Currency Service

### `FetchAndStoreRates() error`
- GET request to OXR with `OXR_APP_ID`
- Parse JSON response
- Upsert each currency pair into `exchangeRate` table:
  - If row exists by (baseCurrency, targetCurrency): update `rate` and `fetchedAt`
  - If not: insert new row
- Log count of rates stored

### `Convert(amount float64, from, to string) (float64, error)`
- If `from == to`: return `amount` immediately
- Load latest rates for `from` and `to` from DB (ordered by `fetchedAt DESC`, limit 1 each)
- Apply conversion formula
- If a rate is missing: return `(0, ErrRateNotFound)` — callers treat missing rate as `null` in response, not a hard error

### `GetLatestRates() ([]ExchangeRate, error)`
- Returns all current rates (one per targetCurrency, latest fetchedAt)
- Used by the list endpoint

---

## Scheduler

```go
// internal/currency/scheduler.go
type Scheduler struct {
    c   *cron.Cron
    svc *Service
}

func (s *Scheduler) Start() {
    s.c.AddFunc("0 * * * *", func() { // every hour
        if err := s.svc.FetchAndStoreRates(); err != nil {
            log.Printf("currency rate refresh error: %v", err)
        }
    })
    s.c.Start()
}
```

On startup (`cmd/main.go`), also call `svc.FetchAndStoreRates()` once immediately so rates are available without waiting an hour.

---

## Config

Add to `Config` struct and `Load()`:
```go
type Config struct {
    // ... existing
    OXRAppID string
}
// In Load():
cfg.OXRAppID = viper.GetString("OXR_APP_ID")
// No fatal if empty — log warning, conversions return null
```

Add `OXR_APP_ID=` to `.env.example`.

---

## API Changes

### New Endpoints

#### `GET /currencies/rates`
- Auth: required
- Returns: array of `{ baseCurrency, targetCurrency, rate, fetchedAt }`
- Shows all latest cached rates

#### `POST /currencies/rates/refresh`
- Auth: required
- Triggers `FetchAndStoreRates()` immediately
- Returns: `{ message: "rates refreshed", count: N }`

### Modified Responses

#### Account (`GET /accounts`, `GET /accounts/:id`)
Add computed field to response:
```json
{
  "id": "...",
  "name": "Checking",
  "currency": "EUR",
  "balance": 1000.00,
  "baseCurrencyBalance": 1087.50,   // new — null if rate unavailable
  "baseCurrency": "USD"             // new — user's base currency for context
}
```
Requires `userId` → `UserSettings.BaseCurrency` lookup per request (via `userSvc`).

#### Transaction (`GET /transactions`, `GET /transactions/:id`)
Add computed field:
```json
{
  "id": "...",
  "amount": 100.00,
  "originalCurrency": "EUR",        // new — null if same as account currency
  "originalAmount": 92.00,          // new — null if same as account currency
  "baseAmount": 100.00,             // new — amount in user's base currency, null if no rate
  "baseCurrency": "USD"             // new — user's base currency for context
}
```

### Modified Reports

All three reports add a `baseCurrency` field to the response envelope and normalize amounts:

#### Net Worth (`GET /reports/net-worth`)
Before: `totalBalance = sum(account.balance)` (mixed currencies, meaningless)
After: `totalBalance = sum(Convert(account.balance, account.currency, baseCurrency))`
Accounts with unavailable rates are excluded from the sum (logged).

#### Category Breakdown (`GET /reports/category-breakdown`)
Before: `total = sum(transaction.amount)` per category
After: `total = sum(Convert(transaction.amount, account.currency, baseCurrency))` per category
Transactions with unavailable rates are excluded from category totals.

#### Monthly Trend (`GET /reports/monthly-trend`)
Before: `income/expense = sum(transaction.amount)` per month
After: normalized to base currency — same exclusion rule for missing rates.

---

## Migrations

| Number | File | Description |
|---|---|---|
| 006 | `006_exchange_rate_table.go` | Create `exchangeRate` table |
| 007 | `007_transaction_currency_fields.go` | Add `originalCurrency`, `originalAmount` to `transaction` |

---

## DTOs

### `CreateTransactionDto` — add optional fields
```go
OriginalCurrency *string  `json:"originalCurrency"`
OriginalAmount   *float64 `json:"originalAmount"`
```

### `UpdateTransactionDto` — add optional fields
```go
OriginalCurrency *string  `json:"originalCurrency"`
OriginalAmount   *float64 `json:"originalAmount"`
```

### `TransactionResponse` — add computed fields
```go
OriginalCurrency *string  `json:"originalCurrency,omitempty"`
OriginalAmount   *float64 `json:"originalAmount,omitempty"`
BaseAmount       *float64 `json:"baseAmount,omitempty"`
BaseCurrency     string   `json:"baseCurrency"`
```

---

## Error Handling

- Missing rate → `baseAmount: null` / `baseCurrencyBalance: null` in response. Not a 500. Log at WARN level.
- OXR unreachable during refresh → log error, keep existing cached rates (stale but functional).
- `OXR_APP_ID` not set → log warning on startup, `FetchAndStoreRates()` returns early. All conversion fields return null.
- Unknown currency code → treated as missing rate.

---

## Wiring in `cmd/main.go`

```go
currencySvc  := currency.NewService(db, cfg.OXRAppID)
currencySvc.FetchAndStoreRates() // initial fetch, ignore error
currencyScheduler := currency.NewScheduler(currencySvc)
currencyScheduler.Start()
defer currencyScheduler.Stop()

currencyHandler    := currency.NewHandler(currencySvc)
accountHandler     := account.NewHandler(accountSvc, currencySvc, userSvc)
transactionHandler := transaction.NewHandler(transactionSvc, currencySvc, userSvc)
reportSvc          := report.NewService(db, currencySvc)
reportHandler      := report.NewHandler(reportSvc, userSvc)

// register routes
currencyHandler.RegisterRoutes(api, jwtAuth)
```

---

## Out of Scope

- Per-transaction rate locking (rates are always "latest at query time")
- Rate history endpoint
- Currency validation against ISO 4217 list (any 3-char string is accepted)
- Admin-only rate management

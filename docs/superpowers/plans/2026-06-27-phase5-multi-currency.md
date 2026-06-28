# Phase 5 — Multi-Currency Support: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add multi-currency support: fetch/cache exchange rates from Open Exchange Rates hourly, record original currency on transactions, and normalize balances and report totals to the user's base currency.

**Architecture:** New `internal/currency/` package owns rate storage and conversion. A `CurrencyConverter` interface is defined locally in each consuming package (transaction, account, report) so dependencies flow inward without circular imports. Conversions happen at query time using the latest cached DB rates — no per-transaction rate locking.

**Tech Stack:** Go 1.23, Gin, GORM, PostgreSQL, `robfig/cron` (already in go.mod), `net/http` (stdlib for OXR fetch)

## Global Constraints

- All DB columns use camelCase with explicit `gorm:"column:..."` tags — never rely on GORM's default snake_case
- Every model must implement `TableName() string`
- Embed `common.BaseModel` for all new entities (provides id, createdAt, updatedAt, deletedAt)
- All soft deletes via GORM — never hard-delete
- Scope all user queries with `"userId" = ?` to prevent data leaks
- Errors from service must be `*common.AppError` so handlers respond with correct HTTP status
- Run `swag init -g cmd/main.go` after any Swagger annotation change
- Test DB is a real PostgreSQL instance via `testutil.NewRealDB(t)` — tests skip automatically if DB is unavailable
- Module path: `ger_fin_api`

---

## File Map

| Action | File | Responsibility |
|---|---|---|
| Create | `internal/currency/model.go` | `ExchangeRate` GORM struct |
| Create | `internal/currency/dto.go` | Response types for currency endpoints |
| Create | `internal/currency/service.go` | `FetchAndStoreRates`, `Convert`, `GetLatestRates` |
| Create | `internal/currency/service_test.go` | Integration tests for Convert |
| Create | `internal/currency/scheduler.go` | Hourly cron job |
| Create | `internal/currency/handler.go` | GET /currencies/rates, POST /currencies/rates/refresh |
| Create | `internal/migrations/006_exchange_rate_table.go` | Create `exchangeRate` table |
| Create | `internal/migrations/007_transaction_currency_fields.go` | Add originalCurrency, originalAmount to transaction |
| Modify | `internal/migrations/runner.go` | Register migrations 006 & 007 |
| Modify | `internal/config/config.go` | Add OXRAppID field |
| Modify | `internal/testutil/db.go` | Add ExchangeRateCols helper |
| Modify | `internal/transaction/model.go` | Add OriginalCurrency, OriginalAmount fields |
| Modify | `internal/transaction/dto.go` | Add fields to CreateTransactionDto, UpdateTransactionDto; add TransactionResponse |
| Modify | `internal/transaction/service.go` | Persist originalCurrency/originalAmount in Create & Update |
| Modify | `internal/transaction/handler.go` | Inject currencySvc & userSvc; return TransactionResponse with baseAmount |
| Modify | `internal/account/dto.go` | Add AccountResponse with baseCurrencyBalance |
| Modify | `internal/account/handler.go` | Inject currencySvc & userSvc; return AccountResponse |
| Modify | `internal/user/service.go` | Add GetBaseCurrency method |
| Modify | `internal/report/dto.go` | Add BaseCurrency to NetWorthResponse, MonthlyTrendResponse wrapper |
| Modify | `internal/report/service.go` | Add CurrencyConverter interface; normalize NetWorth, CategoryBreakdown, MonthlyTrend |
| Modify | `internal/report/handler.go` | Extend SettingsProvider interface; pass baseCurrency to service |
| Modify | `cmd/main.go` | Wire currency service/scheduler/handler; update dependent constructors |

---

## Task 1: Migrations 006 & 007

**Files:**
- Create: `internal/migrations/006_exchange_rate_table.go`
- Create: `internal/migrations/007_transaction_currency_fields.go`
- Modify: `internal/migrations/runner.go`

**Interfaces:**
- Produces: `exchangeRate` table (baseCurrency, targetCurrency, rate, fetchedAt); nullable `originalCurrency`/`originalAmount` columns on `transaction`

- [ ] **Step 1: Create migration 006**

```go
// internal/migrations/006_exchange_rate_table.go
package migrations

import "gorm.io/gorm"

func migration006() Migration {
	return Migration{
		Version: 6,
		Name:    "CreateExchangeRateTable",
		Up: func(db *gorm.DB) error {
			return db.Exec(`
				CREATE TABLE IF NOT EXISTS "exchangeRate" (
					"id"             UUID        NOT NULL DEFAULT gen_random_uuid(),
					"baseCurrency"   VARCHAR(3)  NOT NULL,
					"targetCurrency" VARCHAR(3)  NOT NULL,
					"rate"           NUMERIC(20,8) NOT NULL,
					"fetchedAt"      TIMESTAMPTZ NOT NULL,
					"createdAt"      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
					"updatedAt"      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
					"deletedAt"      TIMESTAMPTZ,
					CONSTRAINT "PK_exchangeRate" PRIMARY KEY ("id")
				);
				CREATE UNIQUE INDEX IF NOT EXISTS "UQ_exchangeRate_pair"
					ON "exchangeRate" ("baseCurrency", "targetCurrency")
					WHERE "deletedAt" IS NULL;
			`).Error
		},
	}
}
```

- [ ] **Step 2: Create migration 007**

```go
// internal/migrations/007_transaction_currency_fields.go
package migrations

import "gorm.io/gorm"

func migration007() Migration {
	return Migration{
		Version: 7,
		Name:    "AddTransactionCurrencyFields",
		Up: func(db *gorm.DB) error {
			return db.Exec(`
				ALTER TABLE "transaction"
					ADD COLUMN IF NOT EXISTS "originalCurrency" VARCHAR(3),
					ADD COLUMN IF NOT EXISTS "originalAmount"   NUMERIC(15,2);
			`).Error
		},
	}
}
```

- [ ] **Step 3: Register both migrations in runner.go**

In `internal/migrations/runner.go`, change the `all` slice:
```go
var all = []Migration{
	migration001(),
	migration002(),
	migration003(),
	migration004(),
	migration005(),
	migration006(),
	migration007(),
}
```

- [ ] **Step 4: Verify migrations apply**

```bash
go run cmd/main.go
```
Expected: logs show `migration 006 applied` and `migration 007 applied`, server starts on port 3000.

- [ ] **Step 5: Commit**

```bash
git add internal/migrations/006_exchange_rate_table.go internal/migrations/007_transaction_currency_fields.go internal/migrations/runner.go
git commit -m "feat: add migrations for exchangeRate table and transaction currency fields"
```

---

## Task 2: Config Extension

**Files:**
- Modify: `internal/config/config.go`

**Interfaces:**
- Produces: `cfg.OXRAppID string` available throughout the app

- [ ] **Step 1: Add OXRAppID to the Config struct**

In `internal/config/config.go`, update the `Config` struct:
```go
type Config struct {
	Port     int
	Database DatabaseConfig
	JWT      JWTConfig
	OXRAppID string
}
```

- [ ] **Step 2: Load OXRAppID in the Load() function**

After the `cfg := &Config{...}` block in `Load()`, add:
```go
cfg.OXRAppID = viper.GetString("OXR_APP_ID")
if cfg.OXRAppID == "" {
    log.Println("OXR_APP_ID not set — currency rate fetching disabled")
}
```

You'll need to add `"log"` to the import block if it's not already there.

- [ ] **Step 3: Verify config compiles**

```bash
go build ./internal/config/...
```
Expected: exits 0, no output.

- [ ] **Step 4: Add OXR_APP_ID to .env.example (or .env if no .env.example)**

Check if `.env.example` exists:
```bash
ls /Users/hoai/projects/personal/ger_fin/ger_fin_api/.env.example 2>/dev/null && echo "exists" || echo "missing"
```

If it exists, append `OXR_APP_ID=` to the file. If not, add it to `.env` (but do NOT commit `.env`).

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go
git commit -m "feat: add OXR_APP_ID config field for Open Exchange Rates"
```

---

## Task 3: Currency Package — Model, DTO, Service

**Files:**
- Create: `internal/currency/model.go`
- Create: `internal/currency/dto.go`
- Create: `internal/currency/service.go`
- Create: `internal/currency/service_test.go`
- Modify: `internal/testutil/db.go`

**Interfaces:**
- Produces: `currency.Service` with methods:
  - `FetchAndStoreRates() error`
  - `Convert(amount float64, from, to string) (float64, error)` — returns `ErrRateNotFound` if no rate exists
  - `GetLatestRates() ([]ExchangeRate, error)`
- Produces: `currency.ErrRateNotFound` sentinel error

- [ ] **Step 1: Write the model**

```go
// internal/currency/model.go
package currency

import (
	"time"

	"ger_fin_api/internal/common"
)

type ExchangeRate struct {
	common.BaseModel
	BaseCurrency   string    `gorm:"column:baseCurrency;type:varchar(3);not null"   json:"baseCurrency"`
	TargetCurrency string    `gorm:"column:targetCurrency;type:varchar(3);not null" json:"targetCurrency"`
	Rate           float64   `gorm:"column:rate;type:numeric(20,8);not null"        json:"rate"`
	FetchedAt      time.Time `gorm:"column:fetchedAt;not null"                      json:"fetchedAt"`
}

func (ExchangeRate) TableName() string { return "exchangeRate" }
```

- [ ] **Step 2: Write the DTOs**

```go
// internal/currency/dto.go
package currency

type RateResponse struct {
	BaseCurrency   string  `json:"baseCurrency"`
	TargetCurrency string  `json:"targetCurrency"`
	Rate           float64 `json:"rate"`
	FetchedAt      string  `json:"fetchedAt"`
}

type RefreshResponse struct {
	Message string `json:"message"`
	Count   int    `json:"count"`
}
```

- [ ] **Step 3: Write the failing tests**

```go
// internal/currency/service_test.go
package currency

import (
	"testing"

	"ger_fin_api/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRealDB_Currency_Convert_SameCurrency(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db, "")

	result, err := svc.Convert(100.0, "USD", "USD")
	require.NoError(t, err)
	assert.Equal(t, 100.0, result)
}

func TestRealDB_Currency_Convert_NoRateInDB(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db, "")

	// No rates seeded — must return ErrRateNotFound
	_, err := svc.Convert(100.0, "EUR", "USD")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRateNotFound)
}

func TestRealDB_Currency_Convert_WithSeededRate(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db, "")

	// Seed EUR rate: 1 USD = 0.92 EUR
	rate := ExchangeRate{
		BaseCurrency: "USD", TargetCurrency: "EUR", Rate: 0.92,
	}
	rate.FetchedAt = rate.CreatedAt
	require.NoError(t, db.Create(&rate).Error)
	// Manually set fetchedAt since it must be non-zero
	require.NoError(t, db.Exec(`UPDATE "exchangeRate" SET "fetchedAt" = NOW() WHERE id = ?`, rate.ID).Error)

	// 100 EUR → USD: 100 / 0.92 ≈ 108.70
	result, err := svc.Convert(100.0, "EUR", "USD")
	require.NoError(t, err)
	assert.InDelta(t, 108.70, result, 0.01)
}

func TestRealDB_Currency_GetLatestRates_Empty(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db, "")

	rates, err := svc.GetLatestRates()
	require.NoError(t, err)
	assert.Empty(t, rates)
}
```

- [ ] **Step 4: Run tests — expect compile error (service not yet written)**

```bash
go test ./internal/currency/...
```
Expected: compile error — `NewService`, `ErrRateNotFound` not defined.

- [ ] **Step 5: Write the service**

```go
// internal/currency/service.go
package currency

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"gorm.io/gorm"
)

var ErrRateNotFound = errors.New("exchange rate not found")

type Service struct {
	db    *gorm.DB
	appID string
}

func NewService(db *gorm.DB, appID string) *Service {
	return &Service{db: db, appID: appID}
}

func (s *Service) FetchAndStoreRates() error {
	if s.appID == "" {
		log.Println("currency: OXR_APP_ID not set, skipping rate fetch")
		return nil
	}

	resp, err := http.Get(fmt.Sprintf("https://openexchangerates.org/api/latest.json?app_id=%s", s.appID))
	if err != nil {
		return fmt.Errorf("fetch rates from OXR: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("OXR API returned status %d", resp.StatusCode)
	}

	var payload struct {
		Base  string             `json:"base"`
		Rates map[string]float64 `json:"rates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return fmt.Errorf("decode OXR response: %w", err)
	}

	// Soft-delete all existing rates for this base so the unique index stays clean
	if err := s.db.Where(`"baseCurrency" = ?`, payload.Base).Delete(&ExchangeRate{}).Error; err != nil {
		return fmt.Errorf("clear old rates: %w", err)
	}

	now := time.Now()
	rates := make([]ExchangeRate, 0, len(payload.Rates))
	for target, rate := range payload.Rates {
		rates = append(rates, ExchangeRate{
			BaseCurrency:   payload.Base,
			TargetCurrency: target,
			Rate:           rate,
			FetchedAt:      now,
		})
	}
	if err := s.db.Create(&rates).Error; err != nil {
		return fmt.Errorf("store rates: %w", err)
	}

	log.Printf("currency: refreshed %d exchange rates (base: %s)", len(rates), payload.Base)
	return nil
}

func (s *Service) Convert(amount float64, from, to string) (float64, error) {
	if from == to {
		return amount, nil
	}

	needed := []string{}
	if from != "USD" {
		needed = append(needed, from)
	}
	if to != "USD" {
		needed = append(needed, to)
	}

	rateMap := make(map[string]float64)
	if len(needed) > 0 {
		var rows []ExchangeRate
		if err := s.db.
			Where(`"baseCurrency" = 'USD' AND "targetCurrency" IN ?`, needed).
			Order(`"fetchedAt" DESC`).
			Find(&rows).Error; err != nil {
			return 0, err
		}
		for _, r := range rows {
			if _, exists := rateMap[r.TargetCurrency]; !exists {
				rateMap[r.TargetCurrency] = r.Rate
			}
		}
	}

	for _, c := range needed {
		if _, ok := rateMap[c]; !ok {
			return 0, fmt.Errorf("%w: USD/%s", ErrRateNotFound, c)
		}
	}

	// Convert: from → USD → to
	usdAmount := amount
	if from != "USD" {
		usdAmount = amount / rateMap[from]
	}
	if to == "USD" {
		return usdAmount, nil
	}
	return usdAmount * rateMap[to], nil
}

func (s *Service) GetLatestRates() ([]ExchangeRate, error) {
	var rates []ExchangeRate
	err := s.db.Order(`"targetCurrency" ASC`).Find(&rates).Error
	return rates, err
}
```

- [ ] **Step 6: Add ExchangeRateCols to testutil**

In `internal/testutil/db.go`, append at the end:
```go
// ExchangeRateCols returns the ordered column list for ExchangeRate rows in sqlmock.
func ExchangeRateCols() []string {
	return []string{"id", "createdAt", "updatedAt", "deletedAt", "baseCurrency", "targetCurrency", "rate", "fetchedAt"}
}
```

- [ ] **Step 7: Run tests — expect pass (or skip if no DB)**

```bash
go test ./internal/currency/... -v
```
Expected: `TestRealDB_Currency_*` tests pass or are skipped with `[SKIP] real db unavailable`. No failures.

- [ ] **Step 8: Commit**

```bash
git add internal/currency/model.go internal/currency/dto.go internal/currency/service.go internal/currency/service_test.go internal/testutil/db.go
git commit -m "feat: add currency package with exchange rate model, service, and Convert logic"
```

---

## Task 4: Currency Scheduler & Handler

**Files:**
- Create: `internal/currency/scheduler.go`
- Create: `internal/currency/handler.go`

**Interfaces:**
- Consumes: `currency.Service` (FetchAndStoreRates, GetLatestRates)
- Produces: `currency.Scheduler.Start()/Stop()` (same pattern as `recurring_transaction.Scheduler`)
- Produces: `currency.Handler.RegisterRoutes(rg, auth)` — GET /currencies/rates, POST /currencies/rates/refresh

- [ ] **Step 1: Write the scheduler**

```go
// internal/currency/scheduler.go
package currency

import (
	"log"

	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	c   *cron.Cron
	svc *Service
}

func NewScheduler(svc *Service) *Scheduler {
	return &Scheduler{
		c:   cron.New(),
		svc: svc,
	}
}

func (s *Scheduler) Start() {
	s.c.AddFunc("0 * * * *", func() {
		log.Println("currency: refreshing exchange rates...")
		if err := s.svc.FetchAndStoreRates(); err != nil {
			log.Printf("currency: rate refresh error: %v", err)
		}
	})
	s.c.Start()
	log.Println("currency: rate refresh scheduler started (hourly)")
}

func (s *Scheduler) Stop() {
	s.c.Stop()
}
```

- [ ] **Step 2: Write the handler**

```go
// internal/currency/handler.go
package currency

import (
	"errors"

	"ger_fin_api/internal/common"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, auth gin.HandlerFunc) {
	g := rg.Group("/currencies", auth)
	g.GET("/rates", h.listRates)
	g.POST("/rates/refresh", h.refresh)
}

// @Summary     List exchange rates
// @Description Returns all currently cached exchange rates
// @Tags        currencies
// @Produce     json
// @Success     200 {array} ExchangeRate
// @Failure     401 {object} common.ErrorResponse
// @Security    BearerAuth
// @Router      /currencies/rates [get]
func (h *Handler) listRates(c *gin.Context) {
	rates, err := h.svc.GetLatestRates()
	if err != nil {
		common.RespondInternalError(c)
		return
	}
	c.JSON(200, rates)
}

// @Summary     Refresh exchange rates
// @Description Immediately re-fetches exchange rates from Open Exchange Rates
// @Tags        currencies
// @Produce     json
// @Success     200 {object} RefreshResponse
// @Failure     401 {object} common.ErrorResponse
// @Failure     500 {object} common.ErrorResponse
// @Security    BearerAuth
// @Router      /currencies/rates/refresh [post]
func (h *Handler) refresh(c *gin.Context) {
	if err := h.svc.FetchAndStoreRates(); err != nil {
		var appErr *common.AppError
		if errors.As(err, &appErr) {
			common.RespondError(c, appErr)
			return
		}
		common.RespondInternalError(c)
		return
	}
	rates, _ := h.svc.GetLatestRates()
	c.JSON(200, RefreshResponse{Message: "rates refreshed", Count: len(rates)})
}
```

- [ ] **Step 3: Verify compile**

```bash
go build ./internal/currency/...
```
Expected: exits 0.

- [ ] **Step 4: Commit**

```bash
git add internal/currency/scheduler.go internal/currency/handler.go
git commit -m "feat: add currency scheduler (hourly refresh) and handler (GET/POST /currencies/rates)"
```

---

## Task 5: Transaction Currency Fields

**Files:**
- Modify: `internal/transaction/model.go`
- Modify: `internal/transaction/dto.go`
- Modify: `internal/transaction/service.go`
- Modify: `internal/transaction/handler.go`

**Interfaces:**
- Consumes: `currency.Service.Convert(float64, string, string) (float64, error)` via local interface
- Produces: `TransactionResponse` — wraps `Transaction` with `baseAmount *float64` and `baseCurrency string`
- Produces: Local `CurrencyConverter` and `BaseCurrencyProvider` interfaces in handler.go

- [ ] **Step 1: Update transaction model**

In `internal/transaction/model.go`, add two nullable fields to the `Transaction` struct after `RecurringTransactionId`:
```go
OriginalCurrency *string  `gorm:"column:originalCurrency;type:varchar(3)"   json:"originalCurrency,omitempty"`
OriginalAmount   *float64 `gorm:"column:originalAmount;type:numeric(15,2)"  json:"originalAmount,omitempty"`
```

- [ ] **Step 2: Update transaction DTOs**

In `internal/transaction/dto.go`:

Add to `CreateTransactionDto` (after `TransactionDate` field):
```go
OriginalCurrency *string  `json:"originalCurrency" binding:"omitempty,len=3"`
OriginalAmount   *float64 `json:"originalAmount"   binding:"omitempty"`
```

Add to `UpdateTransactionDto` (after `TransactionDate` field):
```go
OriginalCurrency *string  `json:"originalCurrency" binding:"omitempty,len=3"`
OriginalAmount   *float64 `json:"originalAmount"   binding:"omitempty"`
```

Add a new response type at the end of the file:
```go
// TransactionResponse wraps Transaction with computed currency fields.
type TransactionResponse struct {
	Transaction
	BaseAmount   *float64 `json:"baseAmount,omitempty"`
	BaseCurrency string   `json:"baseCurrency"`
}
```

- [ ] **Step 3: Update transaction service — persist original currency fields**

In `internal/transaction/service.go`, in the `Create` method, update the `tx := &Transaction{...}` block:
```go
tx := &Transaction{
    Type:             string(dto.Type),
    Amount:           dto.Amount,
    Description:      dto.Description,
    CategoryId:       dto.CategoryId,
    UserId:           userId,
    AccountId:        dto.AccountId,
    TransactionDate:  txDate,
    OriginalCurrency: dto.OriginalCurrency,
    OriginalAmount:   dto.OriginalAmount,
}
```

In the `Update` method, after the existing field update block (before `s.db.Save`), add:
```go
if dto.OriginalCurrency != nil {
    tx.OriginalCurrency = dto.OriginalCurrency
}
if dto.OriginalAmount != nil {
    tx.OriginalAmount = dto.OriginalAmount
}
```

- [ ] **Step 4: Update transaction handler — inject deps, return TransactionResponse**

Replace the entire `internal/transaction/handler.go` with the following. This preserves all existing routing, method names, and helper functions exactly while adding currency conversion:

```go
package transaction

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"sync"

	"ger_fin_api/internal/common"
	"ger_fin_api/internal/middleware"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const maxCSVBytes = 10 * 1024 * 1024 // 10 MB

type importJobState struct {
	mu     sync.Mutex
	status string // pending | running | done | failed
	result *ImportResult
	errMsg string
}

// CurrencyConverter converts an amount from one currency to another.
type CurrencyConverter interface {
	Convert(amount float64, from, to string) (float64, error)
}

// BaseCurrencyProvider returns the user's preferred base currency.
type BaseCurrencyProvider interface {
	GetBaseCurrency(userId string) (string, error)
}

type Handler struct {
	svc         *Service
	currencySvc CurrencyConverter
	userSvc     BaseCurrencyProvider
	jobs        sync.Map // string → *importJobState
}

func NewHandler(svc *Service, currencySvc CurrencyConverter, userSvc BaseCurrencyProvider) *Handler {
	return &Handler{svc: svc, currencySvc: currencySvc, userSvc: userSvc}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, auth gin.HandlerFunc) {
	g := rg.Group("/transactions", auth)
	g.POST("", h.create)
	g.GET("", h.findAll)
	g.GET("/:id", h.findOne)
	g.PATCH("/:id", h.update)
	g.DELETE("/:id", h.remove)
	g.POST("/import/validate", h.validateImportCsv)
	g.POST("/import", h.importCsv)
	g.GET("/import/jobs/:jobId", h.getImportJob)

	t := rg.Group("/transfers", auth)
	t.POST("", h.createTransfer)

	a := rg.Group("/accounts", auth)
	a.GET("/:id/transactions", h.getByAccount)
}

func (h *Handler) baseCurrency(userId string) string {
	if h.userSvc == nil {
		return "USD"
	}
	bc, err := h.userSvc.GetBaseCurrency(userId)
	if err != nil || bc == "" {
		return "USD"
	}
	return bc
}

func (h *Handler) toResponse(tx *Transaction, bc string) TransactionResponse {
	resp := TransactionResponse{Transaction: *tx, BaseCurrency: bc}
	if h.currencySvc != nil && tx.Account != nil {
		if converted, err := h.currencySvc.Convert(tx.Amount, tx.Account.Currency, bc); err == nil {
			resp.BaseAmount = &converted
		}
	}
	return resp
}

func (h *Handler) toResponseList(txs []Transaction, bc string) []TransactionResponse {
	out := make([]TransactionResponse, len(txs))
	for i := range txs {
		out[i] = h.toResponse(&txs[i], bc)
	}
	return out
}

// @Summary		Create transaction
// @Description	Create a new income or expense transaction
// @Tags		transactions
// @Accept		json
// @Produce		json
// @Param		body body CreateTransactionDto true "Transaction details"
// @Success		201 {object} TransactionResponse
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/transactions [post]
func (h *Handler) create(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	var dto CreateTransactionDto
	if !common.BindJSON(c, &dto) {
		return
	}

	tx, err := h.svc.Create(dto, claims.Sub)
	if err != nil {
		common.RespondAppError(c, err)
		return
	}

	bc := h.baseCurrency(claims.Sub)
	c.JSON(http.StatusCreated, h.toResponse(tx, bc))
}

// @Summary		List transactions
// @Description	Get all transactions for the authenticated user within the given time period
// @Tags		transactions
// @Produce		json
// @Param		period query string  false "Time period" Enums(day, week, month, quarter, year) default(month)
// @Param		offset query integer false "Period offset (negative = past)" default(0)
// @Success		200 {array} TransactionResponse
// @Failure		401 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/transactions [get]
func (h *Handler) findAll(c *gin.Context) {
	claims := middleware.CurrentUser(c)
	period, offset := parseTimeQuery(c)

	txs, err := h.svc.FindAll(claims.Sub, period, offset)
	if err != nil {
		common.RespondInternalError(c)
		return
	}

	bc := h.baseCurrency(claims.Sub)
	c.JSON(200, h.toResponseList(txs, bc))
}

// @Summary		Get transaction
// @Description	Get a single transaction by ID
// @Tags		transactions
// @Produce		json
// @Param		id path string true "Transaction ID"
// @Success		200 {object} TransactionResponse
// @Failure		401 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/transactions/{id} [get]
func (h *Handler) findOne(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	tx, err := h.svc.FindOne(c.Param("id"), claims.Sub)
	if err != nil {
		common.RespondAppError(c, err)
		return
	}

	bc := h.baseCurrency(claims.Sub)
	c.JSON(200, h.toResponse(tx, bc))
}

// @Summary		Update transaction
// @Description	Update a transaction's fields
// @Tags		transactions
// @Accept		json
// @Produce		json
// @Param		id   path string               true "Transaction ID"
// @Param		body body UpdateTransactionDto true "Fields to update"
// @Success		200 {object} TransactionResponse
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/transactions/{id} [patch]
func (h *Handler) update(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	var dto UpdateTransactionDto
	if !common.BindJSON(c, &dto) {
		return
	}

	tx, err := h.svc.Update(c.Param("id"), dto, claims.Sub)
	if err != nil {
		common.RespondAppError(c, err)
		return
	}

	bc := h.baseCurrency(claims.Sub)
	c.JSON(200, h.toResponse(tx, bc))
}

// @Summary		Delete transaction
// @Description	Soft-delete a transaction by ID
// @Tags		transactions
// @Produce		json
// @Param		id path string true "Transaction ID"
// @Success		200 {object} common.MessageResponse
// @Failure		401 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/transactions/{id} [delete]
func (h *Handler) remove(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	if err := h.svc.Remove(c.Param("id"), claims.Sub); err != nil {
		common.RespondAppError(c, err)
		return
	}
	c.JSON(200, gin.H{"message": "Transaction deleted successfully"})
}

// @Summary		Get transactions by account
// @Description	Get all transactions for a specific account within the given time period
// @Tags		accounts
// @Produce		json
// @Param		id     path  string  true  "Account ID"
// @Param		period query string  false "Time period" Enums(day, week, month, quarter, year) default(month)
// @Param		offset query integer false "Period offset (negative = past)" default(0)
// @Success		200 {array} TransactionResponse
// @Failure		401 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/accounts/{id}/transactions [get]
func (h *Handler) getByAccount(c *gin.Context) {
	claims := middleware.CurrentUser(c)
	period, offset := parseTimeQuery(c)

	txs, err := h.svc.FindByAccount(c.Param("id"), claims.Sub, period, offset)
	if err != nil {
		common.RespondAppError(c, err)
		return
	}

	bc := h.baseCurrency(claims.Sub)
	c.JSON(200, h.toResponseList(txs, bc))
}

// @Summary		Create transfer
// @Description	Transfer funds between two accounts (creates paired expense + income transactions)
// @Tags		transfers
// @Accept		json
// @Produce		json
// @Param		body body CreateTransferDto true "Transfer details"
// @Success		201 {object} TransferResult
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/transfers [post]
func (h *Handler) createTransfer(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	var dto CreateTransferDto
	if !common.BindJSON(c, &dto) {
		return
	}

	result, err := h.svc.Transfer(dto, claims.Sub)
	if err != nil {
		common.RespondAppError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// @Summary		Validate CSV before import
// @Description	Parse a CSV file and return all issues without importing anything.
// @Tags		transactions
// @Accept		multipart/form-data
// @Produce		json
// @Param		file            formData file   true  "CSV file"
// @Param		categoryMapping formData string false "JSON map of CSV category name to existing category ID"
// @Success		200 {object} ValidateResult
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/transactions/import/validate [post]
func (h *Handler) validateImportCsv(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	buf, categoryMapping, ok := readCsvForm(c)
	if !ok {
		return
	}

	result, err := h.svc.ValidateCsv(buf, claims.Sub, categoryMapping)
	if err != nil {
		common.RespondAppError(c, err)
		return
	}
	c.JSON(200, result)
}

// @Summary		Import transactions from CSV (async)
// @Description	Start a background import job from a CSV file. Poll GET /transactions/import/jobs/{jobId} for status.
// @Tags		transactions
// @Accept		multipart/form-data
// @Produce		json
// @Param		file            formData file   true  "CSV file"
// @Param		categoryMapping formData string false "JSON map of CSV category name to existing category ID"
// @Success		202 {object} ImportJobResponse
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/transactions/import [post]
func (h *Handler) importCsv(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	buf, categoryMapping, ok := readCsvForm(c)
	if !ok {
		return
	}

	jobId := uuid.New().String()
	job := &importJobState{status: "pending"}
	h.jobs.Store(jobId, job)

	go func() {
		job.mu.Lock()
		job.status = "running"
		job.mu.Unlock()

		result, err := h.svc.ImportFromCsv(buf, claims.Sub, categoryMapping)

		job.mu.Lock()
		defer job.mu.Unlock()
		if err != nil {
			job.status = "failed"
			job.errMsg = err.Error()
		} else {
			job.status = "done"
			job.result = result
		}
	}()

	c.JSON(202, ImportJobResponse{JobId: jobId})
}

// @Summary		Get import job status
// @Description	Poll the status of a background CSV import job.
// @Tags		transactions
// @Produce		json
// @Param		jobId path string true "Job ID returned by POST /transactions/import"
// @Success		200 {object} ImportJobStatus
// @Failure		401 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/transactions/import/jobs/{jobId} [get]
func (h *Handler) getImportJob(c *gin.Context) {
	jobId := c.Param("jobId")
	raw, ok := h.jobs.Load(jobId)
	if !ok {
		common.RespondError(c, common.NewNotFound("import job not found"))
		return
	}

	job := raw.(*importJobState)
	job.mu.Lock()
	defer job.mu.Unlock()

	c.JSON(200, ImportJobStatus{
		Status: job.status,
		Result: job.result,
		Error:  job.errMsg,
	})
}

func readCsvForm(c *gin.Context) ([]byte, map[string]string, bool) {
	file, err := c.FormFile("file")
	if err != nil {
		common.RespondError(c, common.NewBadRequest("No file uploaded"))
		return nil, nil, false
	}

	contentType := file.Header.Get("Content-Type")
	if contentType != "text/csv" && !hasCSVExtension(file.Filename) {
		common.RespondError(c, common.NewBadRequest("File must be a CSV file"))
		return nil, nil, false
	}

	f, err := file.Open()
	if err != nil {
		common.RespondInternalError(c)
		return nil, nil, false
	}
	defer f.Close()

	buf, err := io.ReadAll(io.LimitReader(f, maxCSVBytes+1))
	if err != nil {
		common.RespondInternalError(c)
		return nil, nil, false
	}
	if int64(len(buf)) > maxCSVBytes {
		common.RespondError(c, common.NewBadRequest("CSV file exceeds 10 MB size limit"))
		return nil, nil, false
	}

	var categoryMapping map[string]string
	if raw := c.PostForm("categoryMapping"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &categoryMapping); err != nil {
			common.RespondError(c, common.NewBadRequest("Invalid categoryMapping: must be a JSON object"))
			return nil, nil, false
		}
	}

	return buf, categoryMapping, true
}

func parseTimeQuery(c *gin.Context) (common.TimePeriod, int) {
	period := common.TimePeriod(c.DefaultQuery("period", string(common.TimePeriodMonth)))
	offsetStr := c.DefaultQuery("offset", "0")
	offset, _ := strconv.Atoi(offsetStr)
	return period, offset
}

func hasCSVExtension(filename string) bool {
	if len(filename) < 4 {
		return false
	}
	return filename[len(filename)-4:] == ".csv"
}
```

- [ ] **Step 5: Verify compile**

```bash
go build ./internal/transaction/...
```
Expected: exits 0. Fix any missing imports.

- [ ] **Step 7: Commit**

```bash
git add internal/transaction/model.go internal/transaction/dto.go internal/transaction/service.go internal/transaction/handler.go
git commit -m "feat: add originalCurrency/originalAmount to transaction and baseAmount to API response"
```

---

## Task 6: Account — baseCurrencyBalance Response

**Files:**
- Modify: `internal/account/dto.go`
- Modify: `internal/account/handler.go`

**Interfaces:**
- Consumes: `CurrencyConverter` and `BaseCurrencyProvider` (same interface shapes as defined in transaction/handler.go — define locally in account/handler.go)
- Produces: `AccountResponse` — wraps `Account` with `baseCurrencyBalance *float64` and `baseCurrency string`

- [ ] **Step 1: Add AccountResponse to account/dto.go**

In `internal/account/dto.go`, append at the end:
```go
// AccountResponse wraps Account with computed base-currency fields.
type AccountResponse struct {
	Account
	BaseCurrencyBalance *float64 `json:"baseCurrencyBalance,omitempty"`
	BaseCurrency        string   `json:"baseCurrency"`
}
```

- [ ] **Step 2: Update account handler**

Replace the entire `internal/account/handler.go` with:
```go
package account

import (
	"errors"

	"ger_fin_api/internal/common"
	"ger_fin_api/internal/middleware"

	"github.com/gin-gonic/gin"
)

// CurrencyConverter converts an amount from one currency to another.
type CurrencyConverter interface {
	Convert(amount float64, from, to string) (float64, error)
}

// BaseCurrencyProvider returns the user's preferred base currency.
type BaseCurrencyProvider interface {
	GetBaseCurrency(userId string) (string, error)
}

type Handler struct {
	svc         *Service
	currencySvc CurrencyConverter
	userSvc     BaseCurrencyProvider
}

func NewHandler(svc *Service, currencySvc CurrencyConverter, userSvc BaseCurrencyProvider) *Handler {
	return &Handler{svc: svc, currencySvc: currencySvc, userSvc: userSvc}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, auth gin.HandlerFunc) {
	g := rg.Group("/accounts", auth)
	g.POST("", h.create)
	g.GET("", h.findAll)
	g.GET("/:id", h.findOne)
	g.PATCH("/:id", h.update)
	g.DELETE("/:id", h.remove)
}

func (h *Handler) baseCurrency(userId string) string {
	if h.userSvc == nil {
		return "USD"
	}
	bc, err := h.userSvc.GetBaseCurrency(userId)
	if err != nil || bc == "" {
		return "USD"
	}
	return bc
}

func (h *Handler) toResponse(a *Account, bc string) AccountResponse {
	resp := AccountResponse{Account: *a, BaseCurrency: bc}
	if h.currencySvc != nil {
		if converted, err := h.currencySvc.Convert(a.Balance, a.Currency, bc); err == nil {
			resp.BaseCurrencyBalance = &converted
		}
	}
	return resp
}

// @Summary     Create account
// @Description Create a new financial account for the authenticated user
// @Tags        accounts
// @Accept      json
// @Produce     json
// @Param       body body CreateAccountDto true "Account details"
// @Success     201 {object} AccountResponse
// @Failure     400 {object} common.ErrorResponse
// @Failure     401 {object} common.ErrorResponse
// @Security    BearerAuth
// @Router      /accounts [post]
func (h *Handler) create(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	var dto CreateAccountDto
	if !common.BindJSON(c, &dto) {
		return
	}

	a, err := h.svc.Create(claims.Sub, dto)
	if err != nil {
		var appErr *common.AppError
		if errors.As(err, &appErr) {
			common.RespondError(c, appErr)
			return
		}
		common.RespondInternalError(c)
		return
	}

	bc := h.baseCurrency(claims.Sub)
	c.JSON(201, h.toResponse(a, bc))
}

// @Summary     List accounts
// @Description Get all financial accounts for the authenticated user
// @Tags        accounts
// @Produce     json
// @Success     200 {array} AccountResponse
// @Failure     401 {object} common.ErrorResponse
// @Security    BearerAuth
// @Router      /accounts [get]
func (h *Handler) findAll(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	accounts, err := h.svc.FindAllByUser(claims.Sub)
	if err != nil {
		common.RespondInternalError(c)
		return
	}

	bc := h.baseCurrency(claims.Sub)
	out := make([]AccountResponse, len(accounts))
	for i := range accounts {
		out[i] = h.toResponse(&accounts[i], bc)
	}
	c.JSON(200, out)
}

// @Summary     Get account
// @Description Get a single account by ID
// @Tags        accounts
// @Produce     json
// @Param       id path string true "Account ID"
// @Success     200 {object} AccountResponse
// @Failure     401 {object} common.ErrorResponse
// @Failure     404 {object} common.ErrorResponse
// @Security    BearerAuth
// @Router      /accounts/{id} [get]
func (h *Handler) findOne(c *gin.Context) {
	claims := middleware.CurrentUser(c)
	id := c.Param("id")

	a, err := h.svc.FindOne(id, claims.Sub)
	if err != nil {
		var appErr *common.AppError
		if errors.As(err, &appErr) {
			common.RespondError(c, appErr)
			return
		}
		common.RespondInternalError(c)
		return
	}

	bc := h.baseCurrency(claims.Sub)
	c.JSON(200, h.toResponse(a, bc))
}

// @Summary     Update account
// @Description Update an account's details
// @Tags        accounts
// @Accept      json
// @Produce     json
// @Param       id   path string           true "Account ID"
// @Param       body body UpdateAccountDto true "Fields to update"
// @Success     200 {object} AccountResponse
// @Failure     400 {object} common.ErrorResponse
// @Failure     401 {object} common.ErrorResponse
// @Failure     404 {object} common.ErrorResponse
// @Security    BearerAuth
// @Router      /accounts/{id} [patch]
func (h *Handler) update(c *gin.Context) {
	claims := middleware.CurrentUser(c)
	id := c.Param("id")

	var dto UpdateAccountDto
	if !common.BindJSON(c, &dto) {
		return
	}

	a, err := h.svc.Update(id, claims.Sub, dto)
	if err != nil {
		var appErr *common.AppError
		if errors.As(err, &appErr) {
			common.RespondError(c, appErr)
			return
		}
		common.RespondInternalError(c)
		return
	}

	bc := h.baseCurrency(claims.Sub)
	c.JSON(200, h.toResponse(a, bc))
}

// @Summary     Delete account
// @Description Soft-delete an account by ID
// @Tags        accounts
// @Produce     json
// @Param       id path string true "Account ID"
// @Success     200 {object} common.MessageResponse
// @Failure     401 {object} common.ErrorResponse
// @Failure     404 {object} common.ErrorResponse
// @Security    BearerAuth
// @Router      /accounts/{id} [delete]
func (h *Handler) remove(c *gin.Context) {
	claims := middleware.CurrentUser(c)
	id := c.Param("id")

	if err := h.svc.Remove(id, claims.Sub); err != nil {
		var appErr *common.AppError
		if errors.As(err, &appErr) {
			common.RespondError(c, appErr)
			return
		}
		common.RespondInternalError(c)
		return
	}

	c.JSON(200, gin.H{"message": "Account deleted successfully"})
}
```

- [ ] **Step 3: Verify compile**

```bash
go build ./internal/account/...
```
Expected: exits 0.

- [ ] **Step 4: Commit**

```bash
git add internal/account/dto.go internal/account/handler.go
git commit -m "feat: add baseCurrencyBalance to account responses"
```

---

## Task 7: Report Currency Normalization

**Files:**
- Modify: `internal/user/service.go`
- Modify: `internal/report/dto.go`
- Modify: `internal/report/service.go`
- Modify: `internal/report/handler.go`

**Interfaces:**
- Consumes: `user.Service.GetBaseCurrency(userId string) (string, error)` — must be added to user.Service
- Produces: `NetWorthResponse` with `baseCurrency string` and per-account `baseCurrencyBalance *float64`
- Produces: `CategoryBreakdownResponse` with `baseCurrency string`, amounts normalized
- Produces: `MonthlyTrendResponse` wrapper with `baseCurrency string` and `months []MonthRow`

- [ ] **Step 1: Add GetBaseCurrency to user.Service**

In `internal/user/service.go`, append:
```go
// GetBaseCurrency returns the user's preferred base currency, defaulting to "USD".
func (s *Service) GetBaseCurrency(userId string) (string, error) {
	settings, err := s.GetSettings(userId)
	if err != nil {
		return "USD", err
	}
	return settings.BaseCurrency, nil
}
```

- [ ] **Step 2: Update report DTOs**

In `internal/report/dto.go`, apply these changes:

Update `NetWorthAccount` to add `BaseCurrencyBalance`:
```go
type NetWorthAccount struct {
	AccountId           string   `json:"accountId"`
	Name                string   `json:"name"`
	Currency            string   `json:"currency"`
	Balance             float64  `json:"balance"`
	BaseCurrencyBalance *float64 `json:"baseCurrencyBalance,omitempty"`
}
```

Update `NetWorthResponse` to add `BaseCurrency`:
```go
type NetWorthResponse struct {
	Total        float64           `json:"total"`
	BaseCurrency string            `json:"baseCurrency"`
	Accounts     []NetWorthAccount `json:"accounts"`
}
```

Add `BaseCurrency` to `CategoryBreakdownResponse`:
```go
type CategoryBreakdownResponse struct {
	BaseCurrency string        `json:"baseCurrency"`
	Income       []CategoryRow `json:"income"`
	Expense      []CategoryRow `json:"expense"`
}
```

Add a new `MonthlyTrendResponse` wrapper (after the existing `MonthRow` type):
```go
type MonthlyTrendResponse struct {
	BaseCurrency string     `json:"baseCurrency"`
	Months       []MonthRow `json:"months"`
}
```

- [ ] **Step 3: Update the ServiceI interface in report/service.go**

Find the `ServiceI` interface and update the three signatures:
```go
type ServiceI interface {
	CategoryBreakdown(userId string, from, to time.Time, baseCurrency string) (*CategoryBreakdownResponse, error)
	MonthlyTrend(userId string, from, to time.Time, baseCurrency string) (*MonthlyTrendResponse, error)
	NetWorth(userId string, baseCurrency string) (*NetWorthResponse, error)
	TopCategories(userId string, from, to time.Time, limit int) ([]CategoryRow, error)
	CashFlow(userId string, from, to time.Time, lookbackMonths int) (*CashFlowResponse, error)
	YearOverYear(userId string, from, to time.Time, years int) (*YearOverYearResponse, error)
}
```

- [ ] **Step 4: Add CurrencyConverter interface and update Service struct**

In `internal/report/service.go`, add the interface and update the struct after the `ServiceI` declaration:
```go
// CurrencyConverter converts an amount from one currency to another.
type CurrencyConverter interface {
	Convert(amount float64, from, to string) (float64, error)
}

type Service struct {
	db       *gorm.DB
	currency CurrencyConverter
}

func NewService(db *gorm.DB, currency CurrencyConverter) *Service {
	return &Service{db: db, currency: currency}
}
```

- [ ] **Step 5: Update NetWorth in report/service.go**

Replace the existing `NetWorth` method:
```go
func (s *Service) NetWorth(userId string, baseCurrency string) (*NetWorthResponse, error) {
	var accounts []account.Account
	if err := s.db.Where(`"userId" = ? AND "isActive" = ?`, userId, true).Find(&accounts).Error; err != nil {
		return nil, err
	}

	resp := &NetWorthResponse{
		BaseCurrency: baseCurrency,
		Accounts:     make([]NetWorthAccount, 0, len(accounts)),
	}
	for _, a := range accounts {
		nwa := NetWorthAccount{
			AccountId: a.ID,
			Name:      a.Name,
			Currency:  a.Currency,
			Balance:   a.Balance,
		}
		if s.currency != nil {
			if converted, err := s.currency.Convert(a.Balance, a.Currency, baseCurrency); err == nil {
				nwa.BaseCurrencyBalance = &converted
				resp.Total += converted
			} else {
				resp.Total += a.Balance // fallback: add raw balance
			}
		} else {
			resp.Total += a.Balance
		}
		resp.Accounts = append(resp.Accounts, nwa)
	}
	return resp, nil
}
```

- [ ] **Step 6: Update CategoryBreakdown in report/service.go**

Replace the existing `CategoryBreakdown` method AND the existing `categoryAgg` struct (delete the old struct definition before adding the new one):
```go
type categoryAgg struct {
	CategoryId      string  `gorm:"column:category_id"`
	CategoryName    string  `gorm:"column:category_name"`
	TxType          string  `gorm:"column:tx_type"`
	AccountCurrency string  `gorm:"column:account_currency"`
	Total           float64 `gorm:"column:total"`
}

func (s *Service) CategoryBreakdown(userId string, from, to time.Time, baseCurrency string) (*CategoryBreakdownResponse, error) {
	var rows []categoryAgg
	err := s.db.Raw(`
		SELECT t."categoryId"  AS category_id,
		       c."name"        AS category_name,
		       t."type"        AS tx_type,
		       a."currency"    AS account_currency,
		       SUM(t."amount") AS total
		FROM "transaction" t
		JOIN "category" c ON c."id" = t."categoryId"
		JOIN "account"  a ON a."id" = t."accountId"
		WHERE t."userId" = ?
		  AND t."transactionDate" >= ?
		  AND t."transactionDate" <= ?
		  AND t."relatedTransactionId" IS NULL
		  AND t."deletedAt" IS NULL
		GROUP BY t."categoryId", c."name", t."type", a."currency"
		ORDER BY ABS(SUM(t."amount")) DESC
	`, userId, from, to).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	// Aggregate per category, converting to baseCurrency
	type catKey struct{ id, name, txType string }
	catTotals := make(map[catKey]float64)
	for _, r := range rows {
		key := catKey{r.CategoryId, r.CategoryName, r.TxType}
		converted := r.Total
		if s.currency != nil {
			if c, err := s.currency.Convert(r.Total, r.AccountCurrency, baseCurrency); err == nil {
				converted = c
			}
		}
		catTotals[key] += converted
	}

	var totalIncome, totalExpense float64
	for k, total := range catTotals {
		if k.txType == "income" {
			totalIncome += total
		} else {
			totalExpense += math.Abs(total)
		}
	}

	resp := &CategoryBreakdownResponse{
		BaseCurrency: baseCurrency,
		Income:       []CategoryRow{},
		Expense:      []CategoryRow{},
	}
	for k, total := range catTotals {
		var pct float64
		if k.txType == "income" && totalIncome > 0 {
			pct = math.Round((total/totalIncome)*10000) / 100
		} else if k.txType == "expense" && totalExpense > 0 {
			pct = math.Round((math.Abs(total)/totalExpense)*10000) / 100
		}
		row := CategoryRow{
			CategoryId:   k.id,
			CategoryName: k.name,
			Total:        total,
			Percentage:   pct,
		}
		if k.txType == "income" {
			resp.Income = append(resp.Income, row)
		} else {
			resp.Expense = append(resp.Expense, row)
		}
	}
	return resp, nil
}
```

- [ ] **Step 7: Update MonthlyTrend in report/service.go**

Replace the existing `MonthlyTrend` method AND the existing `monthAgg` struct (delete the old struct definition before adding the new one — it gains an `AccountCurrency` field):
```go
type monthAgg struct {
	Month           string  `gorm:"column:month"`
	AccountCurrency string  `gorm:"column:account_currency"`
	Income          float64 `gorm:"column:income"`
	Expense         float64 `gorm:"column:expense"`
}

func (s *Service) MonthlyTrend(userId string, from, to time.Time, baseCurrency string) (*MonthlyTrendResponse, error) {
	var rows []monthAgg
	err := s.db.Raw(`
		SELECT TO_CHAR(t."transactionDate", 'YYYY-MM') AS month,
		       a."currency" AS account_currency,
		       SUM(CASE WHEN t."type" = 'income'  THEN t."amount" ELSE 0 END) AS income,
		       SUM(CASE WHEN t."type" = 'expense' THEN t."amount" ELSE 0 END) AS expense
		FROM "transaction" t
		JOIN "account" a ON a."id" = t."accountId"
		WHERE t."userId" = ?
		  AND t."transactionDate" >= ?
		  AND t."transactionDate" <= ?
		  AND t."relatedTransactionId" IS NULL
		  AND t."deletedAt" IS NULL
		GROUP BY TO_CHAR(t."transactionDate", 'YYYY-MM'), a."currency"
		ORDER BY month ASC
	`, userId, from, to).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	// Aggregate per month, converting to baseCurrency
	rowMap := make(map[string]MonthRow)
	for _, r := range rows {
		income := r.Income
		expense := r.Expense
		if s.currency != nil {
			if c, err := s.currency.Convert(r.Income, r.AccountCurrency, baseCurrency); err == nil {
				income = c
			}
			if c, err := s.currency.Convert(r.Expense, r.AccountCurrency, baseCurrency); err == nil {
				expense = c
			}
		}
		prev := rowMap[r.Month]
		rowMap[r.Month] = MonthRow{
			Month:   r.Month,
			Income:  prev.Income + income,
			Expense: prev.Expense + expense,
			Net:     prev.Net + income + expense,
		}
	}

	return &MonthlyTrendResponse{
		BaseCurrency: baseCurrency,
		Months:       fillMonths(from, to, rowMap),
	}, nil
}
```

- [ ] **Step 8: Extend SettingsProvider interface in report/handler.go and update handler**

In `internal/report/handler.go`, update the `SettingsProvider` interface:
```go
type SettingsProvider interface {
	GetReportDefaults(userId string) (cashFlowMonths, yearOverYearYears int, err error)
	GetBaseCurrency(userId string) (string, error)
}
```

Update the three handler methods that call the changed service functions. Find `h.netWorth`, `h.categoryBreakdown`, and `h.monthlyTrend` and update them:

For `netWorth`:
```go
func (h *Handler) netWorth(c *gin.Context) {
	claims := middleware.CurrentUser(c)
	bc, _ := h.settings.GetBaseCurrency(claims.Sub)
	if bc == "" {
		bc = "USD"
	}
	result, err := h.svc.NetWorth(claims.Sub, bc)
	if err != nil {
		common.RespondInternalError(c)
		return
	}
	c.JSON(200, result)
}
```

For `categoryBreakdown`:
```go
func (h *Handler) categoryBreakdown(c *gin.Context) {
	claims := middleware.CurrentUser(c)
	from, to, err := parseDateRange(c)
	if err != nil {
		common.RespondError(c, common.NewBadRequest(err.Error()))
		return
	}
	bc, _ := h.settings.GetBaseCurrency(claims.Sub)
	if bc == "" {
		bc = "USD"
	}
	result, err := h.svc.CategoryBreakdown(claims.Sub, from, to, bc)
	if err != nil {
		common.RespondInternalError(c)
		return
	}
	c.JSON(200, result)
}
```

For `monthlyTrend`:
```go
func (h *Handler) monthlyTrend(c *gin.Context) {
	claims := middleware.CurrentUser(c)
	from, to, err := parseDateRange(c)
	if err != nil {
		common.RespondError(c, common.NewBadRequest(err.Error()))
		return
	}
	bc, _ := h.settings.GetBaseCurrency(claims.Sub)
	if bc == "" {
		bc = "USD"
	}
	result, err := h.svc.MonthlyTrend(claims.Sub, from, to, bc)
	if err != nil {
		common.RespondInternalError(c)
		return
	}
	c.JSON(200, result)
}
```

- [ ] **Step 9: Verify compile**

```bash
go build ./internal/report/... ./internal/user/...
```
Expected: exits 0.

- [ ] **Step 10: Commit**

```bash
git add internal/user/service.go internal/report/dto.go internal/report/service.go internal/report/handler.go
git commit -m "feat: normalize report totals to user base currency (net worth, category breakdown, monthly trend)"
```

---

## Task 8: Wire main.go + Swagger Regen

**Files:**
- Modify: `cmd/main.go`

**Interfaces:**
- Consumes: all packages from previous tasks
- Produces: running server with `/currencies/rates` endpoints, updated constructors for account/transaction/report handlers

- [ ] **Step 1: Update cmd/main.go**

Replace the entire `cmd/main.go`:
```go
package main

import (
	"fmt"
	"log"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	_ "ger_fin_api/docs"
	"ger_fin_api/internal/account"
	"ger_fin_api/internal/auth"
	"ger_fin_api/internal/category"
	"ger_fin_api/internal/config"
	"ger_fin_api/internal/currency"
	"ger_fin_api/internal/database"
	"ger_fin_api/internal/middleware"
	"ger_fin_api/internal/migrations"
	recurringTx "ger_fin_api/internal/recurring_transaction"
	"ger_fin_api/internal/report"
	"ger_fin_api/internal/transaction"
	"ger_fin_api/internal/user"
)

// @title			GerFin API
// @version			1.0
// @description		Personal finance management API
// @host			localhost:3000
// @BasePath		/
// @securityDefinitions.apikey BearerAuth
// @in				header
// @name			Authorization
// @description		Type "Bearer" followed by a space and your JWT token.
func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	db, err := database.Connect(cfg.Database.DSN())
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	if err := migrations.Run(db); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}

	// Services
	userSvc     := user.NewService(db)
	authSvc     := auth.NewService(userSvc, cfg.JWT.Secret, cfg.JWT.ExpiresIn)
	accountSvc  := account.NewService(db)
	categorySvc := category.NewService(db)

	if err := categorySvc.SeedDefaultCategories(); err != nil {
		log.Fatalf("failed to seed default categories: %v", err)
	}

	currencySvc := currency.NewService(db, cfg.OXRAppID)
	if err := currencySvc.FetchAndStoreRates(); err != nil {
		log.Printf("initial rate fetch failed (continuing with cached rates): %v", err)
	}

	transactionSvc := transaction.NewService(db)
	recurringTxSvc := recurringTx.NewService(db, transactionSvc)
	reportSvc      := report.NewService(db, currencySvc)

	// Schedulers
	recurringScheduler := recurringTx.NewScheduler(recurringTxSvc)
	recurringScheduler.Start()
	defer recurringScheduler.Stop()

	currencyScheduler := currency.NewScheduler(currencySvc)
	currencyScheduler.Start()
	defer currencyScheduler.Stop()

	// Handlers
	authHandler        := auth.NewHandler(authSvc)
	userHandler        := user.NewHandler(userSvc)
	accountHandler     := account.NewHandler(accountSvc, currencySvc, userSvc)
	categoryHandler    := category.NewHandler(categorySvc)
	transactionHandler := transaction.NewHandler(transactionSvc, currencySvc, userSvc)
	recurringTxHandler := recurringTx.NewHandler(recurringTxSvc)
	reportHandler      := report.NewHandler(reportSvc, userSvc)
	currencyHandler    := currency.NewHandler(currencySvc)

	// Auth middleware
	jwtAuth := middleware.JWTAuth(cfg.JWT.Secret)

	r := gin.Default()

	r.Use(cors.New(cors.Config{
		AllowAllOrigins: true,
		AllowMethods:    []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:    []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:   []string{"Content-Length"},
	}))

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	api := r.Group("")
	authHandler.RegisterRoutes(api)
	userHandler.RegisterRoutes(api, jwtAuth)
	accountHandler.RegisterRoutes(api, jwtAuth)
	categoryHandler.RegisterRoutes(api, jwtAuth)
	transactionHandler.RegisterRoutes(api, jwtAuth)
	recurringTxHandler.RegisterRoutes(api, jwtAuth)
	reportHandler.RegisterRoutes(api, jwtAuth)
	currencyHandler.RegisterRoutes(api, jwtAuth)

	if err := r.Run(fmt.Sprintf(":%d", cfg.Port)); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}
```

- [ ] **Step 2: Verify full build**

```bash
go build ./...
```
Expected: exits 0, no errors.

- [ ] **Step 3: Run tests**

```bash
go test ./...
```
Expected: all tests pass or skip (no failures). If any test fails due to interface mismatches, fix the relevant file.

- [ ] **Step 4: Regenerate Swagger docs**

```bash
swag init -g cmd/main.go
```
Expected: `docs/docs.go`, `docs/swagger.json`, `docs/swagger.yaml` updated without errors.

- [ ] **Step 5: Verify server starts**

```bash
go run cmd/main.go
```
Expected: server starts on port 3000, logs show both schedulers started, `migration 006 applied` and `migration 007 applied` (or "already applied").

- [ ] **Step 6: Manual smoke test**

With the server running, test the new endpoint:
```bash
# 1. Get a JWT token
TOKEN=$(curl -s -X POST http://localhost:3000/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"your@email.com","password":"yourpassword"}' | jq -r '.token')

# 2. List exchange rates (empty until OXR_APP_ID is configured)
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:3000/currencies/rates | jq .

# 3. GET /accounts — should include baseCurrencyBalance (null if no rates)
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:3000/accounts | jq .

# 4. GET /reports/net-worth — should include baseCurrency field
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:3000/reports/net-worth | jq .
```
Expected: responses include `baseCurrency` and `baseCurrencyBalance` fields (values may be null if no OXR_APP_ID configured).

- [ ] **Step 7: Update ROADMAP.md**

In `docs/ROADMAP.md`, update Phase 5 items to `[x]`:
```markdown
## Phase 5 — Multi-Currency Support

- [x] **Exchange rate store** — fetch & cache rates (e.g. via ECB or Open Exchange Rates)
- [x] **Base currency config** — per-user preferred display currency
- [x] **Balance normalization** — convert all account balances to base currency for net worth
- [x] **Transaction currency field** — record original currency + amount alongside base amount
- [x] **Rate history** — store historical rates used at transaction time
```

Also update the status table row for Phase 5.

- [ ] **Step 8: Commit**

```bash
git add cmd/main.go docs/swagger.json docs/swagger.yaml docs/docs.go docs/ROADMAP.md
git commit -m "feat: wire Phase 5 multi-currency — exchange rates, base currency normalization on accounts, transactions, and reports"
```

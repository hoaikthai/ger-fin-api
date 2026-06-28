# Budget Planning (Phase 3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add per-category and overall monthly/yearly spending budgets with progress tracking, over-budget detection, and automatic period roll-forward with rollover.

**Architecture:** New `internal/budget` domain following the existing one-package-per-domain pattern (model/dto/service/handler/scheduler/test). Budgets are stored as **one row per concrete period** (instance model), linked across periods by a `seriesId`. A daily cron rolls the latest active instance of each series forward into new periods, carrying unspent budget when `rollover` is enabled. Spend is computed at read time by mirroring the report service's currency-normalised aggregation (`internal/report/service.go`).

**Tech Stack:** Go 1.23, Gin, GORM, PostgreSQL, `robfig/cron/v3`, swaggo/swag. Tests use the real-DB integration harness (`internal/testutil.NewRealDB`).

## Global Constraints

- All DB columns use **camelCase** with explicit `gorm:"column:..."` tags. Never snake_case.
- Every model embeds `common.BaseModel` and implements `TableName()` returning a **singular** name.
- Soft deletes only (GORM `DeletedAt` in `BaseModel`). Never hard-delete.
- All queries scoped to `userId`. Never return another user's data.
- Services return `*common.AppError` (via `NewBadRequest`/`NewNotFound`/`NewForbidden`/`NewConflict`), never raw error strings, to the handler. Handlers use `common.RespondAppError(c, err)`.
- Handlers: extract `middleware.CurrentUser(c)` first, bind with `common.BindJSON`, delegate to service, respond. No business logic.
- Expense transactions are stored with **negative** `amount`. Spend = `ABS(SUM(amount))`.
- Spend excludes transfers (`relatedTransactionId IS NULL`) and soft-deleted rows (`deletedAt IS NULL`), and is converted to a target currency; rows whose conversion rate is unavailable are **excluded** (logged), matching `report/service.go`.
- Module path is `ger_fin_api`. Entry point `cmd/main.go`.
- After handler annotations change, regenerate docs: `swag init -g cmd/main.go`.

---

## File Structure

- Create `internal/budget/model.go` — `Budget` GORM struct + `TableName()`.
- Create `internal/budget/dto.go` — request/response shapes + period helper.
- Create `internal/budget/service.go` — CRUD, spend computation, roll-forward.
- Create `internal/budget/scheduler.go` — daily cron wrapper.
- Create `internal/budget/handler.go` — HTTP layer + route registration.
- Create `internal/budget/service_test.go` — integration tests (real DB).
- Create `internal/migrations/008_budget_table.go` — `budget` table + indexes.
- Modify `internal/migrations/runner.go` — register `migration008()`.
- Modify `internal/common/enums.go` — add `BudgetPeriod` enum.
- Modify `cmd/main.go` — wire service, scheduler, handler, routes.

### Key interfaces (used across tasks)

```go
// internal/budget/service.go
type CurrencyConverter interface {
    Convert(amount float64, from, to string) (float64, error) // satisfied by *currency.Service
}
type BaseCurrencyProvider interface {
    GetBaseCurrency(userId string) (string, error) // satisfied by *user.Service
}
```

The `budget.Service` depends on these two interfaces (defined locally, concrete types injected in `main.go`) so the scheduler can resolve a user's base currency without an HTTP request — consistent with how `report` injects `SettingsProvider`.

---

## Task 1: Migration, enum, and model

**Files:**
- Modify: `internal/common/enums.go`
- Create: `internal/migrations/008_budget_table.go`
- Modify: `internal/migrations/runner.go:14-22` (the `all` slice)
- Create: `internal/budget/model.go`
- Create: `internal/budget/service_test.go` (test added here, grows in later tasks)

**Interfaces:**
- Produces: `budget.Budget` struct; `common.BudgetPeriodMonthly`, `common.BudgetPeriodYearly`.

- [ ] **Step 1: Add the budget period enum**

In `internal/common/enums.go`, append after the `RecurrenceFrequency` block:

```go
type BudgetPeriod string

const (
	BudgetPeriodMonthly BudgetPeriod = "monthly"
	BudgetPeriodYearly  BudgetPeriod = "yearly"
)
```

- [ ] **Step 2: Create the migration**

Create `internal/migrations/008_budget_table.go`:

```go
package migrations

import "gorm.io/gorm"

func migration008() Migration {
	return Migration{
		Version: 8,
		Name:    "CreateBudgetTable",
		Up: func(db *gorm.DB) error {
			return db.Exec(`
				CREATE TABLE IF NOT EXISTS "budget" (
					"id"          uuid        NOT NULL DEFAULT uuid_generate_v4(),
					"createdAt"   timestamptz NOT NULL DEFAULT now(),
					"updatedAt"   timestamptz NOT NULL DEFAULT now(),
					"deletedAt"   timestamptz,
					"seriesId"    uuid        NOT NULL,
					"categoryId"  uuid,
					"userId"      uuid        NOT NULL,
					"periodType"  varchar     NOT NULL,
					"periodStart" date        NOT NULL,
					"periodEnd"   date        NOT NULL,
					"amount"      numeric(15,2) NOT NULL,
					"currency"    varchar(3)  NOT NULL,
					"carriedOver" numeric(15,2) NOT NULL DEFAULT 0,
					"rollover"    boolean     NOT NULL DEFAULT false,
					"isActive"    boolean     NOT NULL DEFAULT true,
					CONSTRAINT "PK_budget" PRIMARY KEY ("id"),
					CONSTRAINT "FK_budget_category"
						FOREIGN KEY ("categoryId") REFERENCES "category"("id") ON DELETE CASCADE,
					CONSTRAINT "FK_budget_user"
						FOREIGN KEY ("userId") REFERENCES "user"("id") ON DELETE CASCADE
				);

				CREATE INDEX IF NOT EXISTS "IDX_budget_series"
					ON "budget" ("seriesId") WHERE "deletedAt" IS NULL;

				CREATE UNIQUE INDEX IF NOT EXISTS "UQ_budget_category_period"
					ON "budget" ("userId", "categoryId", "periodType", "periodStart")
					WHERE "deletedAt" IS NULL AND "categoryId" IS NOT NULL;

				CREATE UNIQUE INDEX IF NOT EXISTS "UQ_budget_overall_period"
					ON "budget" ("userId", "periodType", "periodStart")
					WHERE "deletedAt" IS NULL AND "categoryId" IS NULL;
			`).Error
		},
	}
}
```

- [ ] **Step 3: Register the migration**

In `internal/migrations/runner.go`, add `migration008()` to the end of the `all` slice:

```go
var all = []Migration{
	migration001(),
	migration002(),
	migration003(),
	migration004(),
	migration005(),
	migration006(),
	migration007(),
	migration008(),
}
```

- [ ] **Step 4: Create the model**

Create `internal/budget/model.go`:

```go
package budget

import (
	"time"

	"ger_fin_api/internal/category"
	"ger_fin_api/internal/common"
)

type Budget struct {
	common.BaseModel
	SeriesId    string             `gorm:"column:seriesId;type:uuid;not null"          json:"seriesId"`
	CategoryId  *string            `gorm:"column:categoryId;type:uuid"                 json:"categoryId"`
	Category    *category.Category `gorm:"foreignKey:CategoryId"                       json:"category,omitempty"`
	UserId      string             `gorm:"column:userId;type:uuid;not null"            json:"userId"`
	PeriodType  string             `gorm:"column:periodType;not null"                  json:"periodType"`
	PeriodStart time.Time          `gorm:"column:periodStart;type:date;not null"       json:"periodStart"`
	PeriodEnd   time.Time          `gorm:"column:periodEnd;type:date;not null"         json:"periodEnd"`
	Amount      float64            `gorm:"column:amount;type:numeric(15,2);not null"   json:"amount"`
	Currency    string             `gorm:"column:currency;type:varchar(3);not null"    json:"currency"`
	CarriedOver float64            `gorm:"column:carriedOver;type:numeric(15,2);not null;default:0" json:"carriedOver"`
	Rollover    bool               `gorm:"column:rollover;not null;default:false"      json:"rollover"`
	IsActive    bool               `gorm:"column:isActive;not null;default:true"       json:"isActive"`
}

func (Budget) TableName() string { return "budget" }
```

- [ ] **Step 5: Write the failing migration/model test**

Create `internal/budget/service_test.go`:

```go
package budget

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/account"
	"ger_fin_api/internal/category"
	"ger_fin_api/internal/common"
	"ger_fin_api/internal/currency"
	"ger_fin_api/internal/testutil"
	"ger_fin_api/internal/transaction"
	"ger_fin_api/internal/user"
	"gorm.io/gorm"
)

func seedUser(t *testing.T, db *gorm.DB) string {
	t.Helper()
	u := &user.User{Email: "test-" + uuid.New().String() + "@example.com", Password: "hashed"}
	created, err := user.NewService(db).Create(u)
	require.NoError(t, err)
	return created.ID
}

func seedAccount(t *testing.T, db *gorm.DB, userId, ccy string) string {
	t.Helper()
	a, err := account.NewService(db).Create(userId, account.CreateAccountDto{Name: "Acct", Currency: ccy, Balance: 0})
	require.NoError(t, err)
	return a.ID
}

func seedExpenseCategory(t *testing.T, db *gorm.DB, userId, name string) string {
	t.Helper()
	cat, err := category.NewService(db).Create(category.CreateCategoryDto{Name: name, Type: common.TransactionTypeExpense}, userId)
	require.NoError(t, err)
	return cat.ID
}

func seedChildCategory(t *testing.T, db *gorm.DB, userId, name, parentId string) string {
	t.Helper()
	cat, err := category.NewService(db).Create(category.CreateCategoryDto{Name: name, Type: common.TransactionTypeExpense, ParentId: &parentId}, userId)
	require.NoError(t, err)
	return cat.ID
}

func seedIncomeCategory(t *testing.T, db *gorm.DB, userId string) string {
	t.Helper()
	cat, err := category.NewService(db).Create(category.CreateCategoryDto{Name: "Salary", Type: common.TransactionTypeIncome}, userId)
	require.NoError(t, err)
	return cat.ID
}

// seedExpense creates an expense transaction (negative amount) on the given date.
func seedExpense(t *testing.T, db *gorm.DB, userId, accountId, categoryId string, amount float64, date time.Time) {
	t.Helper()
	_, err := transaction.NewService(db).Create(transaction.CreateTransactionDto{
		Type:            common.TransactionTypeExpense,
		Amount:          -amount,
		Description:     "exp",
		CategoryId:      categoryId,
		AccountId:       accountId,
		TransactionDate: &date,
	}, userId)
	require.NoError(t, err)
}

// newSvc builds a budget service with real currency conversion and base-currency lookup.
func newSvc(db *gorm.DB) *Service {
	return NewService(db, currency.NewService(db, ""), user.NewService(db))
}

func TestRealDB_Budget_TableRoundTrip(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	catId := seedExpenseCategory(t, db, userId, "Food")

	b := &Budget{
		SeriesId:    uuid.New().String(),
		CategoryId:  &catId,
		UserId:      userId,
		PeriodType:  string(common.BudgetPeriodMonthly),
		PeriodStart: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
		Amount:      500,
		Currency:    "USD",
		Rollover:    true,
		IsActive:    true,
	}
	require.NoError(t, db.Create(b).Error)

	var got Budget
	require.NoError(t, db.First(&got, "id = ?", b.ID).Error)
	assert.Equal(t, 500.0, got.Amount)
	assert.Equal(t, "USD", got.Currency)
	assert.True(t, got.Rollover)
}
```

- [ ] **Step 6: Run the test (verify it passes against a real/migrated DB)**

Run: `go test ./internal/budget/ -run TestRealDB_Budget_TableRoundTrip -v`
Expected: PASS (or `SKIP: real db unavailable` if Postgres is not running — start it with `make docker-up` to actually exercise it).

- [ ] **Step 7: Commit**

```bash
git add internal/common/enums.go internal/migrations/008_budget_table.go internal/migrations/runner.go internal/budget/model.go internal/budget/service_test.go
git commit -m "feat(budget): add budget table, model, and period enum"
```

---

## Task 2: DTOs and period-bounds helper

**Files:**
- Create: `internal/budget/dto.go`
- Test: `internal/budget/period_test.go`

**Interfaces:**
- Produces: `CreateBudgetDto`, `UpdateBudgetDto`, `BudgetProgress`, `BudgetDetailResponse`; `periodBounds(periodType string, ref time.Time) (start, end time.Time)`; `nextPeriodStart(periodType string, start time.Time) time.Time`.
- Consumes: `common.BudgetPeriod` (Task 1).

`periodBounds` returns the inclusive `[start, end]` calendar dates for the period containing `ref`, normalised to UTC midnight. `end` is the last day of the period (e.g. `2026-06-30`). Spend queries use `periodEnd + 1 day` as an exclusive upper bound (Task 4).

- [ ] **Step 1: Write the failing test**

Create `internal/budget/period_test.go`:

```go
package budget

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"ger_fin_api/internal/common"
)

func TestPeriodBounds_Monthly(t *testing.T) {
	ref := time.Date(2026, 6, 15, 9, 30, 0, 0, time.UTC)
	start, end := periodBounds(string(common.BudgetPeriodMonthly), ref)
	assert.Equal(t, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), start)
	assert.Equal(t, time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC), end)
}

func TestPeriodBounds_Yearly(t *testing.T) {
	ref := time.Date(2026, 6, 15, 9, 30, 0, 0, time.UTC)
	start, end := periodBounds(string(common.BudgetPeriodYearly), ref)
	assert.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), start)
	assert.Equal(t, time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC), end)
}

func TestNextPeriodStart(t *testing.T) {
	jun := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		nextPeriodStart(string(common.BudgetPeriodMonthly), jun))

	y2026 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		nextPeriodStart(string(common.BudgetPeriodYearly), y2026))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/budget/ -run TestPeriodBounds -v`
Expected: FAIL — `undefined: periodBounds`.

- [ ] **Step 3: Create the DTOs and helpers**

Create `internal/budget/dto.go`:

```go
package budget

import (
	"time"

	"ger_fin_api/internal/common"
)

type CreateBudgetDto struct {
	CategoryId *string             `json:"categoryId" binding:"omitempty,uuid"` // nil = overall total-expense cap
	PeriodType common.BudgetPeriod `json:"periodType" binding:"required,oneof=monthly yearly"`
	Amount     float64             `json:"amount"     binding:"required,gt=0"`
	Rollover   *bool               `json:"rollover"   binding:"omitempty"`
}

type UpdateBudgetDto struct {
	Amount   *float64 `json:"amount"   binding:"omitempty,gt=0"`
	Rollover *bool    `json:"rollover" binding:"omitempty"`
	IsActive *bool    `json:"isActive" binding:"omitempty"`
}

// BudgetProgress holds the computed analytics for a single budget instance.
type BudgetProgress struct {
	EffectiveCap float64 `json:"effectiveCap"` // amount + carriedOver
	Spent        float64 `json:"spent"`        // positive
	Remaining    float64 `json:"remaining"`    // effectiveCap - spent
	PercentUsed  float64 `json:"percentUsed"`  // 0..(>100), 2dp
	IsOverBudget bool    `json:"isOverBudget"`
}

// BudgetDetailResponse is returned by GET /budgets/:id.
type BudgetDetailResponse struct {
	Budget
	Progress BudgetProgress `json:"progress"`
}

// periodBounds returns the inclusive [start, end] calendar dates (UTC midnight)
// of the period containing ref.
func periodBounds(periodType string, ref time.Time) (time.Time, time.Time) {
	y, m, _ := ref.Date()
	if periodType == string(common.BudgetPeriodYearly) {
		start := time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(y, 12, 31, 0, 0, 0, 0, time.UTC)
		return start, end
	}
	start := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)
	return start, end
}

// nextPeriodStart returns the start date of the period following the one
// beginning at start.
func nextPeriodStart(periodType string, start time.Time) time.Time {
	if periodType == string(common.BudgetPeriodYearly) {
		return start.AddDate(1, 0, 0)
	}
	return start.AddDate(0, 1, 0)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/budget/ -run "TestPeriodBounds|TestNextPeriodStart" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/budget/dto.go internal/budget/period_test.go
git commit -m "feat(budget): add DTOs and period-bounds helpers"
```

---

## Task 3: Service create (validation, currency snapshot, conflict)

**Files:**
- Create: `internal/budget/service.go`
- Test: `internal/budget/service_test.go` (append)

**Interfaces:**
- Consumes: `Budget` (T1), `CreateBudgetDto` (T2), `periodBounds` (T2), `common` enums.
- Produces:
  - `type Service struct`
  - `func NewService(db *gorm.DB, conv CurrencyConverter, settings BaseCurrencyProvider) *Service`
  - `func (s *Service) Create(dto CreateBudgetDto, userId string) (*Budget, error)`
  - interfaces `CurrencyConverter`, `BaseCurrencyProvider`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/budget/service_test.go`:

```go
func TestRealDB_Budget_Create_Category(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	catId := seedExpenseCategory(t, db, userId, "Food")
	svc := newSvc(db)

	roll := true
	b, err := svc.Create(CreateBudgetDto{
		CategoryId: &catId,
		PeriodType: common.BudgetPeriodMonthly,
		Amount:     500,
		Rollover:   &roll,
	}, userId)
	require.NoError(t, err)
	assert.NotEmpty(t, b.SeriesId)
	assert.Equal(t, "USD", b.Currency) // base currency default
	assert.Equal(t, string(common.BudgetPeriodMonthly), b.PeriodType)
	assert.True(t, b.Rollover)
	assert.Equal(t, 1, b.PeriodStart.Day()) // first of current month
}

func TestRealDB_Budget_Create_Overall(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	svc := newSvc(db)

	b, err := svc.Create(CreateBudgetDto{
		PeriodType: common.BudgetPeriodMonthly,
		Amount:     3000,
	}, userId)
	require.NoError(t, err)
	assert.Nil(t, b.CategoryId)
	assert.False(t, b.Rollover) // defaults false
}

func TestRealDB_Budget_Create_RejectsIncomeCategory(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	incomeCat := seedIncomeCategory(t, db, userId)
	svc := newSvc(db)

	_, err := svc.Create(CreateBudgetDto{
		CategoryId: &incomeCat,
		PeriodType: common.BudgetPeriodMonthly,
		Amount:     100,
	}, userId)
	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, 400, appErr.Code)
}

func TestRealDB_Budget_Create_RejectsForeignCategory(t *testing.T) {
	db := testutil.NewRealDB(t)
	owner := seedUser(t, db)
	intruder := seedUser(t, db)
	catId := seedExpenseCategory(t, db, owner, "Food")
	svc := newSvc(db)

	_, err := svc.Create(CreateBudgetDto{
		CategoryId: &catId,
		PeriodType: common.BudgetPeriodMonthly,
		Amount:     100,
	}, intruder)
	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, 403, appErr.Code)
}

func TestRealDB_Budget_Create_RejectsDuplicate(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	catId := seedExpenseCategory(t, db, userId, "Food")
	svc := newSvc(db)

	_, err := svc.Create(CreateBudgetDto{CategoryId: &catId, PeriodType: common.BudgetPeriodMonthly, Amount: 500}, userId)
	require.NoError(t, err)

	_, err = svc.Create(CreateBudgetDto{CategoryId: &catId, PeriodType: common.BudgetPeriodMonthly, Amount: 600}, userId)
	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, 409, appErr.Code)
}
```

> Note: `common.AppError` exposes the HTTP status as its `Code` int field (see `internal/common/errors.go`).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/budget/ -run TestRealDB_Budget_Create -v`
Expected: FAIL — `undefined: NewService` / `Create`.

- [ ] **Step 3: Write the service create + validation**

Create `internal/budget/service.go`:

```go
package budget

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"ger_fin_api/internal/common"
)

// CurrencyConverter converts an amount between currencies (satisfied by *currency.Service).
type CurrencyConverter interface {
	Convert(amount float64, from, to string) (float64, error)
}

// BaseCurrencyProvider resolves a user's base currency (satisfied by *user.Service).
type BaseCurrencyProvider interface {
	GetBaseCurrency(userId string) (string, error)
}

type Service struct {
	db       *gorm.DB
	conv     CurrencyConverter
	settings BaseCurrencyProvider
}

func NewService(db *gorm.DB, conv CurrencyConverter, settings BaseCurrencyProvider) *Service {
	return &Service{db: db, conv: conv, settings: settings}
}

func (s *Service) Create(dto CreateBudgetDto, userId string) (*Budget, error) {
	if err := s.validateCategory(dto.CategoryId, userId); err != nil {
		return nil, err
	}

	start, end := periodBounds(string(dto.PeriodType), time.Now().UTC())

	if err := s.assertNoConflict(userId, dto.CategoryId, string(dto.PeriodType), start); err != nil {
		return nil, err
	}

	baseCurrency, _ := s.settings.GetBaseCurrency(userId) // defaults to "USD" on error

	rollover := false
	if dto.Rollover != nil {
		rollover = *dto.Rollover
	}

	b := &Budget{
		SeriesId:    uuid.New().String(),
		CategoryId:  dto.CategoryId,
		UserId:      userId,
		PeriodType:  string(dto.PeriodType),
		PeriodStart: start,
		PeriodEnd:   end,
		Amount:      dto.Amount,
		Currency:    baseCurrency,
		CarriedOver: 0,
		Rollover:    rollover,
		IsActive:    true,
	}
	if err := s.db.Create(b).Error; err != nil {
		return nil, err
	}
	return b, nil
}

// validateCategory ensures a non-nil categoryId refers to an expense category
// owned by the user (or a default category). nil categoryId (overall budget) is allowed.
func (s *Service) validateCategory(categoryId *string, userId string) error {
	if categoryId == nil {
		return nil
	}
	var row struct {
		Type      string
		IsDefault bool
		UserId    *string
	}
	err := s.db.Raw(`
		SELECT "type" AS type, "isDefault" AS is_default, "userId" AS user_id
		FROM "category" WHERE "id" = ? AND "deletedAt" IS NULL
	`, *categoryId).Scan(&row).Error
	if err != nil {
		return err
	}
	if row.Type == "" {
		return common.NewNotFound("Category not found")
	}
	if !row.IsDefault && (row.UserId == nil || *row.UserId != userId) {
		return common.NewForbidden("Access denied to this category")
	}
	if row.Type != string(common.TransactionTypeExpense) {
		return common.NewBadRequest("Budgets can only target expense categories")
	}
	return nil
}

func (s *Service) assertNoConflict(userId string, categoryId *string, periodType string, start time.Time) error {
	q := s.db.Model(&Budget{}).
		Where(`"userId" = ? AND "periodType" = ? AND "periodStart" = ?`, userId, periodType, start)
	if categoryId == nil {
		q = q.Where(`"categoryId" IS NULL`)
	} else {
		q = q.Where(`"categoryId" = ?`, *categoryId)
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return common.NewConflict("A budget already exists for this category and period")
	}
	return nil
}

// isUniqueViolation reports whether err is a Postgres unique-constraint error
// (backstop for the partial unique indexes when two requests race).
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "duplicate key value")
}

var _ = errors.Is // kept for later tasks
```

> The `assertNoConflict` pre-check returns a friendly 409. Keep the `isUniqueViolation` helper; later tasks need it as a backstop. If `go vet` complains about the unused `errors` import after this task, remove the `var _ = errors.Is` line and the import — a later task re-adds `errors`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/budget/ -run TestRealDB_Budget_Create -v`
Expected: PASS (or SKIP if no DB).

- [ ] **Step 5: Commit**

```bash
git add internal/budget/service.go internal/budget/service_test.go
git commit -m "feat(budget): service Create with category validation and conflict check"
```

---

## Task 4: Spend computation and FindOne with progress

**Files:**
- Modify: `internal/budget/service.go`
- Test: `internal/budget/service_test.go` (append)

**Interfaces:**
- Consumes: `Budget`, `BudgetProgress`, `BudgetDetailResponse`, `CurrencyConverter`.
- Produces:
  - `func (s *Service) FindOne(id, userId string) (*BudgetDetailResponse, error)`
  - `func (s *Service) computeSpent(userId string, categoryId *string, start, end time.Time, target string) (float64, error)`
  - `func (s *Service) progressFor(b Budget) (BudgetProgress, error)`

Spend mirrors `report.CategoryBreakdown`: group expenses by account currency, convert `ABS(total)` to the budget's snapshot `currency`, exclude rate-unavailable rows. For a category budget, restrict to the category subtree (the category itself + its direct children — categories are only two levels deep). For an overall budget (`categoryId == nil`), include all expenses. Upper bound is exclusive at `periodEnd + 1 day` so the last day is fully counted.

- [ ] **Step 1: Write the failing tests**

Append to `internal/budget/service_test.go`:

```go
func TestRealDB_Budget_Progress_RollsUpChildren(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	acct := seedAccount(t, db, userId, "USD")
	parent := seedExpenseCategory(t, db, userId, "Food")
	child := seedChildCategory(t, db, userId, "Groceries", parent)
	svc := newSvc(db)

	now := time.Now().UTC()
	seedExpense(t, db, userId, acct, parent, 100, now) // on parent
	seedExpense(t, db, userId, acct, child, 50, now)   // on child

	b, err := svc.Create(CreateBudgetDto{CategoryId: &parent, PeriodType: common.BudgetPeriodMonthly, Amount: 500}, userId)
	require.NoError(t, err)

	detail, err := svc.FindOne(b.ID, userId)
	require.NoError(t, err)
	assert.InDelta(t, 150.0, detail.Progress.Spent, 0.001) // parent + child rolled up
	assert.InDelta(t, 350.0, detail.Progress.Remaining, 0.001)
	assert.InDelta(t, 30.0, detail.Progress.PercentUsed, 0.01)
	assert.False(t, detail.Progress.IsOverBudget)
}

func TestRealDB_Budget_Progress_OverBudget(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	acct := seedAccount(t, db, userId, "USD")
	cat := seedExpenseCategory(t, db, userId, "Food")
	svc := newSvc(db)

	seedExpense(t, db, userId, acct, cat, 560, time.Now().UTC())
	b, err := svc.Create(CreateBudgetDto{CategoryId: &cat, PeriodType: common.BudgetPeriodMonthly, Amount: 500}, userId)
	require.NoError(t, err)

	detail, err := svc.FindOne(b.ID, userId)
	require.NoError(t, err)
	assert.InDelta(t, 560.0, detail.Progress.Spent, 0.001)
	assert.InDelta(t, -60.0, detail.Progress.Remaining, 0.001)
	assert.True(t, detail.Progress.IsOverBudget)
}

func TestRealDB_Budget_Progress_Overall(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	acct := seedAccount(t, db, userId, "USD")
	food := seedExpenseCategory(t, db, userId, "Food")
	travel := seedExpenseCategory(t, db, userId, "Travel")
	svc := newSvc(db)

	now := time.Now().UTC()
	seedExpense(t, db, userId, acct, food, 200, now)
	seedExpense(t, db, userId, acct, travel, 300, now)

	b, err := svc.Create(CreateBudgetDto{PeriodType: common.BudgetPeriodMonthly, Amount: 1000}, userId)
	require.NoError(t, err)

	detail, err := svc.FindOne(b.ID, userId)
	require.NoError(t, err)
	assert.InDelta(t, 500.0, detail.Progress.Spent, 0.001) // all expenses
}

func TestRealDB_Budget_FindOne_NotFoundForOtherUser(t *testing.T) {
	db := testutil.NewRealDB(t)
	owner := seedUser(t, db)
	intruder := seedUser(t, db)
	cat := seedExpenseCategory(t, db, owner, "Food")
	svc := newSvc(db)

	b, err := svc.Create(CreateBudgetDto{CategoryId: &cat, PeriodType: common.BudgetPeriodMonthly, Amount: 500}, owner)
	require.NoError(t, err)

	_, err = svc.FindOne(b.ID, intruder)
	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, 404, appErr.Code)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/budget/ -run "TestRealDB_Budget_Progress|TestRealDB_Budget_FindOne" -v`
Expected: FAIL — `undefined: FindOne`.

- [ ] **Step 3: Add spend computation + FindOne**

Append to `internal/budget/service.go`:

```go
import (
	"log"
	"math"
)
```

> Add `log` and `math` to the existing import block at the top of `service.go` (do not create a second import block).

```go
type spendRow struct {
	AccountCurrency string  `gorm:"column:account_currency"`
	Total           float64 `gorm:"column:total"`
}

// computeSpent returns positive spend within [start, end] (end inclusive),
// converted to target currency. Rate-unavailable rows are excluded (logged),
// matching report/service.go.
func (s *Service) computeSpent(userId string, categoryId *string, start, end time.Time, target string) (float64, error) {
	upper := end.AddDate(0, 0, 1) // exclusive upper bound: include all of the last day

	q := `
		SELECT a."currency" AS account_currency, SUM(t."amount") AS total
		FROM "transaction" t
		JOIN "account" a ON a."id" = t."accountId"
		WHERE t."userId" = ?
		  AND t."type" = 'expense'
		  AND t."transactionDate" >= ?
		  AND t."transactionDate" < ?
		  AND t."relatedTransactionId" IS NULL
		  AND t."deletedAt" IS NULL`
	args := []interface{}{userId, start, upper}

	if categoryId != nil {
		q += ` AND t."categoryId" IN (SELECT "id" FROM "category" WHERE "id" = ? OR "parentId" = ?)`
		args = append(args, *categoryId, *categoryId)
	}
	q += ` GROUP BY a."currency"`

	var rows []spendRow
	if err := s.db.Raw(q, args...).Scan(&rows).Error; err != nil {
		return 0, err
	}

	var spent float64
	for _, r := range rows {
		amt := math.Abs(r.Total)
		if s.conv != nil {
			c, err := s.conv.Convert(amt, r.AccountCurrency, target)
			if err != nil {
				log.Printf("budget: spend skipping row (currency %s -> %s): %v", r.AccountCurrency, target, err)
				continue
			}
			amt = c
		}
		spent += amt
	}
	return spent, nil
}

func (s *Service) progressFor(b Budget) (BudgetProgress, error) {
	spent, err := s.computeSpent(b.UserId, b.CategoryId, b.PeriodStart, b.PeriodEnd, b.Currency)
	if err != nil {
		return BudgetProgress{}, err
	}
	effective := b.Amount + b.CarriedOver
	var pct float64
	if effective > 0 {
		pct = math.Round((spent/effective)*10000) / 100
	}
	return BudgetProgress{
		EffectiveCap: effective,
		Spent:        spent,
		Remaining:    effective - spent,
		PercentUsed:  pct,
		IsOverBudget: spent > effective,
	}, nil
}

func (s *Service) findOneRaw(id, userId string) (*Budget, error) {
	var b Budget
	err := s.db.Preload("Category").
		Where(`"id" = ? AND "userId" = ?`, id, userId).
		First(&b).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, common.NewNotFound("Budget not found")
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (s *Service) FindOne(id, userId string) (*BudgetDetailResponse, error) {
	b, err := s.findOneRaw(id, userId)
	if err != nil {
		return nil, err
	}
	prog, err := s.progressFor(*b)
	if err != nil {
		return nil, err
	}
	return &BudgetDetailResponse{Budget: *b, Progress: prog}, nil
}
```

> Now that `errors` is used (`errors.Is`), delete the `var _ = errors.Is` placeholder line added in Task 3.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/budget/ -run "TestRealDB_Budget_Progress|TestRealDB_Budget_FindOne" -v`
Expected: PASS (or SKIP).

- [ ] **Step 5: Commit**

```bash
git add internal/budget/service.go internal/budget/service_test.go
git commit -m "feat(budget): spend computation and FindOne with progress"
```

---

## Task 5: List, Update, and Remove

**Files:**
- Modify: `internal/budget/service.go`
- Test: `internal/budget/service_test.go` (append)

**Interfaces:**
- Produces:
  - `func (s *Service) FindAll(userId string, monthStart, yearStart time.Time) ([]Budget, error)`
  - `func (s *Service) Update(id string, dto UpdateBudgetDto, userId string) (*Budget, error)`
  - `func (s *Service) Remove(id, userId string) error`

`FindAll` returns active, non-deleted instances whose period matches the target month (for monthly budgets) or target year (for yearly budgets). The handler computes `monthStart`/`yearStart` from optional query params, defaulting to the current month/year.

- [ ] **Step 1: Write the failing tests**

Append to `internal/budget/service_test.go`:

```go
func TestRealDB_Budget_FindAll_CurrentPeriod(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	food := seedExpenseCategory(t, db, userId, "Food")
	travel := seedExpenseCategory(t, db, userId, "Travel")
	svc := newSvc(db)

	_, err := svc.Create(CreateBudgetDto{CategoryId: &food, PeriodType: common.BudgetPeriodMonthly, Amount: 500}, userId)
	require.NoError(t, err)
	_, err = svc.Create(CreateBudgetDto{CategoryId: &travel, PeriodType: common.BudgetPeriodYearly, Amount: 5000}, userId)
	require.NoError(t, err)

	now := time.Now().UTC()
	mStart, _ := periodBounds(string(common.BudgetPeriodMonthly), now)
	yStart, _ := periodBounds(string(common.BudgetPeriodYearly), now)

	list, err := svc.FindAll(userId, mStart, yStart)
	require.NoError(t, err)
	assert.Len(t, list, 2) // both current monthly + current yearly
}

func TestRealDB_Budget_Update_Fields(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	cat := seedExpenseCategory(t, db, userId, "Food")
	svc := newSvc(db)

	b, err := svc.Create(CreateBudgetDto{CategoryId: &cat, PeriodType: common.BudgetPeriodMonthly, Amount: 500}, userId)
	require.NoError(t, err)

	newAmt := 700.0
	active := false
	updated, err := svc.Update(b.ID, UpdateBudgetDto{Amount: &newAmt, IsActive: &active}, userId)
	require.NoError(t, err)
	assert.Equal(t, 700.0, updated.Amount)
	assert.False(t, updated.IsActive)
}

func TestRealDB_Budget_Remove_SoftDeletes(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	cat := seedExpenseCategory(t, db, userId, "Food")
	svc := newSvc(db)

	b, err := svc.Create(CreateBudgetDto{CategoryId: &cat, PeriodType: common.BudgetPeriodMonthly, Amount: 500}, userId)
	require.NoError(t, err)

	require.NoError(t, svc.Remove(b.ID, userId))

	_, err = svc.FindOne(b.ID, userId)
	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, 404, appErr.Code)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/budget/ -run "TestRealDB_Budget_FindAll|TestRealDB_Budget_Update|TestRealDB_Budget_Remove" -v`
Expected: FAIL — `undefined: FindAll`.

- [ ] **Step 3: Implement FindAll, Update, Remove**

Append to `internal/budget/service.go`:

```go
// FindAll returns active current-period instances: monthly budgets whose period
// starts at monthStart, plus yearly budgets whose period starts at yearStart.
func (s *Service) FindAll(userId string, monthStart, yearStart time.Time) ([]Budget, error) {
	var budgets []Budget
	err := s.db.Preload("Category").
		Where(`"userId" = ? AND "isActive" = ?`, userId, true).
		Where(`("periodType" = ? AND "periodStart" = ?) OR ("periodType" = ? AND "periodStart" = ?)`,
			string(common.BudgetPeriodMonthly), monthStart,
			string(common.BudgetPeriodYearly), yearStart).
		Order(`"createdAt" DESC`).
		Find(&budgets).Error
	return budgets, err
}

func (s *Service) Update(id string, dto UpdateBudgetDto, userId string) (*Budget, error) {
	b, err := s.findOneRaw(id, userId)
	if err != nil {
		return nil, err
	}
	if dto.Amount != nil {
		b.Amount = *dto.Amount
	}
	if dto.Rollover != nil {
		b.Rollover = *dto.Rollover
	}
	if dto.IsActive != nil {
		b.IsActive = *dto.IsActive
	}
	if err := s.db.Save(b).Error; err != nil {
		return nil, err
	}
	return b, nil
}

func (s *Service) Remove(id, userId string) error {
	b, err := s.findOneRaw(id, userId)
	if err != nil {
		return err
	}
	return s.db.Delete(b).Error
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/budget/ -run "TestRealDB_Budget_FindAll|TestRealDB_Budget_Update|TestRealDB_Budget_Remove" -v`
Expected: PASS (or SKIP).

- [ ] **Step 5: Commit**

```bash
git add internal/budget/service.go internal/budget/service_test.go
git commit -m "feat(budget): list current-period, update, and soft-delete"
```

---

## Task 6: Roll-forward (cron) with catch-up and rollover

**Files:**
- Modify: `internal/budget/service.go`
- Create: `internal/budget/scheduler.go`
- Test: `internal/budget/service_test.go` (append)

**Interfaces:**
- Produces:
  - `func (s *Service) ProcessDueBudgetRollovers() error`
  - `func (s *Service) rollSeriesForward(tail Budget, now time.Time) error`
  - `type Scheduler`, `func NewScheduler(svc *Service) *Scheduler`, `Start()`, `Stop()`.

**Roll-forward rules:**
- For each series, take the latest non-deleted, **active** instance ("tail").
- While `tail.periodStart` is before the period containing `now`, create the next instance and advance.
- `carriedOver` into the next instance: `rollover ? max(0, (amount+carriedOver) - prevSpent) : 0`, computed in the tail's `currency`.
- Snapshot the user's current base currency on the new instance. **If it differs from the tail's currency, convert both `amount` and `carriedOver` into the new currency** so the new instance is internally coherent. If either conversion fails (rate unavailable), keep the tail's currency, amount, and the rollover computed in that currency, and log.
- The partial unique indexes are a backstop against duplicate creation.

- [ ] **Step 1: Write the failing tests**

Append to `internal/budget/service_test.go`:

```go
// makeTailInLastMonth inserts an active monthly budget instance whose period is
// the previous calendar month, so a rollover run must create the current month.
func makeTailInLastMonth(t *testing.T, db *gorm.DB, userId string, catId *string, amount, carried float64, rollover bool) Budget {
	t.Helper()
	lastMonthRef := time.Now().UTC().AddDate(0, -1, 0)
	start, end := periodBounds(string(common.BudgetPeriodMonthly), lastMonthRef)
	b := Budget{
		SeriesId:    uuid.New().String(),
		CategoryId:  catId,
		UserId:      userId,
		PeriodType:  string(common.BudgetPeriodMonthly),
		PeriodStart: start,
		PeriodEnd:   end,
		Amount:      amount,
		Currency:    "USD",
		CarriedOver: carried,
		Rollover:    rollover,
		IsActive:    true,
	}
	require.NoError(t, db.Create(&b).Error)
	return b
}

func TestRealDB_Budget_Rollover_CarriesUnspent(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	acct := seedAccount(t, db, userId, "USD")
	cat := seedExpenseCategory(t, db, userId, "Food")
	svc := newSvc(db)

	tail := makeTailInLastMonth(t, db, userId, &cat, 500, 0, true)
	// spend 420 last month -> 80 should carry forward
	seedExpense(t, db, userId, acct, cat, 420, tail.PeriodStart.AddDate(0, 0, 5))

	require.NoError(t, svc.ProcessDueBudgetRollovers())

	curStart, _ := periodBounds(string(common.BudgetPeriodMonthly), time.Now().UTC())
	var next Budget
	require.NoError(t, db.Where(`"seriesId" = ? AND "periodStart" = ?`, tail.SeriesId, curStart).First(&next).Error)
	assert.InDelta(t, 80.0, next.CarriedOver, 0.001)
	assert.InDelta(t, 500.0, next.Amount, 0.001)
}

func TestRealDB_Budget_Rollover_OverspendClampsToZero(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	acct := seedAccount(t, db, userId, "USD")
	cat := seedExpenseCategory(t, db, userId, "Food")
	svc := newSvc(db)

	tail := makeTailInLastMonth(t, db, userId, &cat, 500, 0, true)
	seedExpense(t, db, userId, acct, cat, 560, tail.PeriodStart.AddDate(0, 0, 5))

	require.NoError(t, svc.ProcessDueBudgetRollovers())

	curStart, _ := periodBounds(string(common.BudgetPeriodMonthly), time.Now().UTC())
	var next Budget
	require.NoError(t, db.Where(`"seriesId" = ? AND "periodStart" = ?`, tail.SeriesId, curStart).First(&next).Error)
	assert.InDelta(t, 0.0, next.CarriedOver, 0.001)
}

func TestRealDB_Budget_Rollover_Disabled_NoCarry(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	acct := seedAccount(t, db, userId, "USD")
	cat := seedExpenseCategory(t, db, userId, "Food")
	svc := newSvc(db)

	tail := makeTailInLastMonth(t, db, userId, &cat, 500, 0, false)
	seedExpense(t, db, userId, acct, cat, 100, tail.PeriodStart.AddDate(0, 0, 5))

	require.NoError(t, svc.ProcessDueBudgetRollovers())

	curStart, _ := periodBounds(string(common.BudgetPeriodMonthly), time.Now().UTC())
	var next Budget
	require.NoError(t, db.Where(`"seriesId" = ? AND "periodStart" = ?`, tail.SeriesId, curStart).First(&next).Error)
	assert.InDelta(t, 0.0, next.CarriedOver, 0.001)
}

func TestRealDB_Budget_Rollover_Idempotent(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	cat := seedExpenseCategory(t, db, userId, "Food")
	svc := newSvc(db)

	tail := makeTailInLastMonth(t, db, userId, &cat, 500, 0, true)

	require.NoError(t, svc.ProcessDueBudgetRollovers())
	require.NoError(t, svc.ProcessDueBudgetRollovers()) // second run must not duplicate

	var count int64
	require.NoError(t, db.Model(&Budget{}).Where(`"seriesId" = ?`, tail.SeriesId).Count(&count).Error)
	assert.Equal(t, int64(2), count) // last month + current month only
}

func TestRealDB_Budget_Rollover_InactiveSkipped(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	cat := seedExpenseCategory(t, db, userId, "Food")
	svc := newSvc(db)

	tail := makeTailInLastMonth(t, db, userId, &cat, 500, 0, true)
	require.NoError(t, db.Model(&Budget{}).Where(`"id" = ?`, tail.ID).Update("isActive", false).Error)

	require.NoError(t, svc.ProcessDueBudgetRollovers())

	var count int64
	require.NoError(t, db.Model(&Budget{}).Where(`"seriesId" = ?`, tail.SeriesId).Count(&count).Error)
	assert.Equal(t, int64(1), count) // paused series does not roll forward
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/budget/ -run TestRealDB_Budget_Rollover -v`
Expected: FAIL — `undefined: ProcessDueBudgetRollovers`.

- [ ] **Step 3: Implement roll-forward**

Append to `internal/budget/service.go`:

```go
// ProcessDueBudgetRollovers advances each active series up to the current period.
func (s *Service) ProcessDueBudgetRollovers() error {
	now := time.Now().UTC()

	// Latest active, non-deleted instance per series (the "tail").
	var tails []Budget
	err := s.db.Raw(`
		SELECT b.* FROM "budget" b
		WHERE b."isActive" = true AND b."deletedAt" IS NULL
		  AND NOT EXISTS (
			SELECT 1 FROM "budget" b2
			WHERE b2."seriesId" = b."seriesId"
			  AND b2."deletedAt" IS NULL
			  AND b2."periodStart" > b."periodStart"
		  )
	`).Scan(&tails).Error
	if err != nil {
		return err
	}

	for _, tail := range tails {
		if err := s.rollSeriesForward(tail, now); err != nil {
			log.Printf("budget: failed to roll series %s forward: %v", tail.SeriesId, err)
		}
	}
	return nil
}

func (s *Service) rollSeriesForward(tail Budget, now time.Time) error {
	targetStart, _ := periodBounds(tail.PeriodType, now)
	cur := tail

	for cur.PeriodStart.Before(targetStart) {
		prevSpent, err := s.computeSpent(cur.UserId, cur.CategoryId, cur.PeriodStart, cur.PeriodEnd, cur.Currency)
		if err != nil {
			return err
		}

		// rollover computed in the tail's currency
		carried := 0.0
		if cur.Rollover {
			carried = math.Max(0, (cur.Amount+cur.CarriedOver)-prevSpent)
		}

		amount := cur.Amount
		currency := cur.Currency

		// If base currency changed, restate the whole instance in the new currency.
		base, _ := s.settings.GetBaseCurrency(cur.UserId)
		if base != "" && base != cur.Currency && s.conv != nil {
			convAmount, aErr := s.conv.Convert(amount, cur.Currency, base)
			convCarried := 0.0
			cErr := error(nil)
			if carried > 0 {
				convCarried, cErr = s.conv.Convert(carried, cur.Currency, base)
			}
			if aErr == nil && cErr == nil {
				amount = convAmount
				carried = convCarried
				currency = base
			} else {
				log.Printf("budget: series %s keeping currency %s (rate to %s unavailable)", cur.SeriesId, cur.Currency, base)
			}
		}

		nextStart := nextPeriodStart(cur.PeriodType, cur.PeriodStart)
		_, nextEnd := periodBounds(cur.PeriodType, nextStart)

		next := Budget{
			SeriesId:    cur.SeriesId,
			CategoryId:  cur.CategoryId,
			UserId:      cur.UserId,
			PeriodType:  cur.PeriodType,
			PeriodStart: nextStart,
			PeriodEnd:   nextEnd,
			Amount:      amount,
			Currency:    currency,
			CarriedOver: carried,
			Rollover:    cur.Rollover,
			IsActive:    true,
		}
		if err := s.db.Create(&next).Error; err != nil {
			if isUniqueViolation(err) {
				return nil // already created by a concurrent/previous run
			}
			return err
		}
		cur = next
	}
	return nil
}
```

- [ ] **Step 4: Create the scheduler**

Create `internal/budget/scheduler.go`:

```go
package budget

import (
	"log"

	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	c   *cron.Cron
	svc *Service
}

func NewScheduler(svc *Service) *Scheduler {
	return &Scheduler{c: cron.New(), svc: svc}
}

func (s *Scheduler) Start() {
	s.c.AddFunc("0 0 * * *", func() {
		log.Println("Running budget rollover scheduler...")
		if err := s.svc.ProcessDueBudgetRollovers(); err != nil {
			log.Printf("Budget rollover scheduler error: %v", err)
		}
	})
	s.c.Start()
	log.Println("Budget rollover scheduler started (daily at midnight)")
}

func (s *Scheduler) Stop() {
	s.c.Stop()
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/budget/ -run TestRealDB_Budget_Rollover -v`
Expected: PASS (or SKIP).

- [ ] **Step 6: Commit**

```bash
git add internal/budget/service.go internal/budget/scheduler.go internal/budget/service_test.go
git commit -m "feat(budget): cron roll-forward with rollover and catch-up"
```

---

## Task 7: HTTP handler, routes, and Swagger

**Files:**
- Create: `internal/budget/handler.go`

**Interfaces:**
- Consumes: `Service` (all methods), `CreateBudgetDto`, `UpdateBudgetDto`, `common`, `middleware`.
- Produces: `func NewHandler(svc *Service) *Handler`, `func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, auth gin.HandlerFunc)`.

The handler computes the list's target month/year from optional `period` (`YYYY-MM`) and `year` (`YYYY`) query params, defaulting to the current month/year.

- [ ] **Step 1: Create the handler**

Create `internal/budget/handler.go`:

```go
package budget

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"ger_fin_api/internal/common"
	"ger_fin_api/internal/middleware"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, auth gin.HandlerFunc) {
	g := rg.Group("/budgets", auth)
	g.POST("", h.create)
	g.GET("", h.findAll)
	g.GET("/:id", h.findOne)
	g.PATCH("/:id", h.update)
	g.DELETE("/:id", h.remove)
}

// @Summary		Create budget
// @Description	Create a monthly or yearly spending budget for an expense category (or an overall cap when categoryId is omitted)
// @Tags		budgets
// @Accept		json
// @Produce		json
// @Param		body body CreateBudgetDto true "Budget details"
// @Success		201 {object} Budget
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Failure		403 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Failure		409 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/budgets [post]
func (h *Handler) create(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	var dto CreateBudgetDto
	if !common.BindJSON(c, &dto) {
		return
	}

	b, err := h.svc.Create(dto, claims.Sub)
	if err != nil {
		common.RespondAppError(c, err)
		return
	}
	c.JSON(201, b)
}

// @Summary		List budgets
// @Description	List active budgets for the current period. Use ?period=YYYY-MM to view a past/future month and ?year=YYYY for yearly budgets.
// @Tags		budgets
// @Produce		json
// @Param		period query string false "Target month (YYYY-MM), defaults to current month"
// @Param		year   query string false "Target year (YYYY), defaults to current year"
// @Success		200 {array} Budget
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/budgets [get]
func (h *Handler) findAll(c *gin.Context) {
	claims := middleware.CurrentUser(c)
	now := time.Now().UTC()

	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	if p := c.Query("period"); p != "" {
		t, err := time.Parse("2006-01", p)
		if err != nil {
			common.RespondError(c, common.NewBadRequest("invalid period: use YYYY-MM"))
			return
		}
		monthStart = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	}

	yearStart := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	if y := c.Query("year"); y != "" {
		yi, err := strconv.Atoi(y)
		if err != nil {
			common.RespondError(c, common.NewBadRequest("invalid year: use YYYY"))
			return
		}
		yearStart = time.Date(yi, 1, 1, 0, 0, 0, 0, time.UTC)
	}

	budgets, err := h.svc.FindAll(claims.Sub, monthStart, yearStart)
	if err != nil {
		common.RespondInternalError(c)
		return
	}
	c.JSON(200, budgets)
}

// @Summary		Get budget with progress
// @Description	Get a single budget instance with computed spend, remaining, percent used, and over-budget flag
// @Tags		budgets
// @Produce		json
// @Param		id path string true "Budget ID"
// @Success		200 {object} BudgetDetailResponse
// @Failure		401 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/budgets/{id} [get]
func (h *Handler) findOne(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	detail, err := h.svc.FindOne(c.Param("id"), claims.Sub)
	if err != nil {
		common.RespondAppError(c, err)
		return
	}
	c.JSON(200, detail)
}

// @Summary		Update budget
// @Description	Update a budget instance's cap, rollover, or active state. Changes propagate to future periods via the roll-forward job.
// @Tags		budgets
// @Accept		json
// @Produce		json
// @Param		id   path string          true "Budget ID"
// @Param		body body UpdateBudgetDto  true "Fields to update"
// @Success		200 {object} Budget
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/budgets/{id} [patch]
func (h *Handler) update(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	var dto UpdateBudgetDto
	if !common.BindJSON(c, &dto) {
		return
	}

	b, err := h.svc.Update(c.Param("id"), dto, claims.Sub)
	if err != nil {
		common.RespondAppError(c, err)
		return
	}
	c.JSON(200, b)
}

// @Summary		Delete budget
// @Description	Soft-delete a budget instance. This also stops the series from rolling forward.
// @Tags		budgets
// @Produce		json
// @Param		id path string true "Budget ID"
// @Success		200 {object} common.MessageResponse
// @Failure		401 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/budgets/{id} [delete]
func (h *Handler) remove(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	if err := h.svc.Remove(c.Param("id"), claims.Sub); err != nil {
		common.RespondAppError(c, err)
		return
	}
	c.JSON(200, gin.H{"message": "Budget deleted successfully"})
}
```

- [ ] **Step 2: Verify the package compiles**

Run: `go build ./internal/budget/`
Expected: no output (success).

- [ ] **Step 3: Commit**

```bash
git add internal/budget/handler.go
git commit -m "feat(budget): HTTP handler, routes, and Swagger annotations"
```

---

## Task 8: Wire into main.go, regenerate docs, full build/test

**Files:**
- Modify: `cmd/main.go`

**Interfaces:**
- Consumes: `budget.NewService`, `budget.NewScheduler`, `budget.NewHandler`; existing `currencySvc`, `userSvc`.

- [ ] **Step 1: Add the import**

In `cmd/main.go`, add to the import block (keep alphabetical grouping with the other `ger_fin_api/internal/*` imports):

```go
	"ger_fin_api/internal/budget"
```

- [ ] **Step 2: Construct the service**

In `cmd/main.go`, after the `reportSvc := report.NewService(db, currencySvc)` line, add:

```go
	budgetSvc := budget.NewService(db, currencySvc, userSvc)
```

- [ ] **Step 3: Start the scheduler**

In the `// Schedulers` section, after the `currencyScheduler` block, add:

```go
	budgetScheduler := budget.NewScheduler(budgetSvc)
	budgetScheduler.Start()
	defer budgetScheduler.Stop()
```

- [ ] **Step 4: Construct and register the handler**

In the `// Handlers` section, after `currencyHandler := currency.NewHandler(currencySvc)`, add:

```go
	budgetHandler := budget.NewHandler(budgetSvc)
```

And in the route-registration block, after `currencyHandler.RegisterRoutes(api, jwtAuth)`, add:

```go
	budgetHandler.RegisterRoutes(api, jwtAuth)
```

- [ ] **Step 5: Regenerate Swagger docs**

Run: `swag init -g cmd/main.go`
Expected: `docs/docs.go`, `docs/swagger.json`, `docs/swagger.yaml` updated with the `budgets` tag and paths.

> If `swag` is not installed: `go install github.com/swaggo/swag/cmd/swag@latest` and ensure `$(go env GOPATH)/bin` is on `PATH`.

- [ ] **Step 6: Build the whole project**

Run: `go build ./...`
Expected: no output (success).

- [ ] **Step 7: Vet and run the full test suite**

Run: `go vet ./... && go test ./...`
Expected: all packages pass; `internal/budget` tests pass (or SKIP if Postgres is unavailable — start it with `make docker-up` to fully exercise them).

- [ ] **Step 8: Update the roadmap**

In `docs/ROADMAP.md`, check off the Phase 3 items (lines 68-72) and update the status table row 3 to `✅ Done`.

- [ ] **Step 9: Commit**

```bash
git add cmd/main.go docs/
git commit -m "feat(budget): wire budget domain into app and regenerate docs"
```

---

## Self-Review Notes

- **Spec coverage:** Budget model (T1), CRUD (T3/T5/T7), progress + over-budget flag (T4/T7), rollover (T6), monthly+yearly (T1/T2), parent rollup (T4), overall cap (T3/T4), base-currency snapshot + conversion (T3/T4/T6), cron catch-up (T6), seriesId + isActive (T1/T6). All covered.
- **Currency on roll-forward:** the decision captured only `carriedOver` conversion; this plan also converts `amount` so a re-based instance stays coherent (Task 6 rule). Flagged for the user.
- **Type consistency:** `Service` constructor `NewService(db, conv, settings)` is used identically in tests (`newSvc`) and `main.go`. `BudgetDetailResponse` embeds `Budget` + `Progress BudgetProgress` consistently across T2/T4/T7. `periodBounds`/`nextPeriodStart` signatures match all call sites.
- **AppError field:** the HTTP status is `appErr.Code` (verified in `internal/common/errors.go`); all test asserts use it.
```

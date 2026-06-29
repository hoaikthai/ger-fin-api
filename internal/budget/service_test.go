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

func TestRealDB_Budget_Create_RejectsNonExistentCategory(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	svc := newSvc(db)
	ghost := uuid.New().String()

	_, err := svc.Create(CreateBudgetDto{
		CategoryId: &ghost,
		PeriodType: common.BudgetPeriodMonthly,
		Amount:     100,
	}, userId)
	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, 404, appErr.Code)
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
	assert.InDelta(t, 112.0, detail.Progress.PercentUsed, 0.01) // 560/500
}

func TestRealDB_Budget_Progress_ExcludesUnavailableRate(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	eurAcct := seedAccount(t, db, userId, "EUR")
	cat := seedExpenseCategory(t, db, userId, "Food")
	svc := newSvc(db)

	// Budget snapshots base currency USD; account is EUR; no EUR/USD rate is seeded.
	seedExpense(t, db, userId, eurAcct, cat, 100, time.Now().UTC())
	b, err := svc.Create(CreateBudgetDto{CategoryId: &cat, PeriodType: common.BudgetPeriodMonthly, Amount: 500}, userId)
	require.NoError(t, err)

	detail, err := svc.FindOne(b.ID, userId)
	require.NoError(t, err)
	assert.InDelta(t, 0.0, detail.Progress.Spent, 0.001) // EUR->USD rate unavailable -> row excluded from spend
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

	// Another user's budget in the same period must NOT appear in this user's list.
	intruder := seedUser(t, db)
	intruderCat := seedExpenseCategory(t, db, intruder, "Other")
	_, err = svc.Create(CreateBudgetDto{CategoryId: &intruderCat, PeriodType: common.BudgetPeriodYearly, Amount: 9999}, intruder)
	require.NoError(t, err)

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
	assert.False(t, updated.Rollover) // unset field must remain default, not clobbered
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

func TestRealDB_Budget_Rollover_MultiPeriodCatchUp(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	cat := seedExpenseCategory(t, db, userId, "Food")
	svc := newSvc(db)

	twoMonthsAgo := time.Now().UTC().AddDate(0, -2, 0)
	start, end := periodBounds(string(common.BudgetPeriodMonthly), twoMonthsAgo)
	tail := Budget{
		SeriesId:    uuid.New().String(),
		CategoryId:  &cat,
		UserId:      userId,
		PeriodType:  string(common.BudgetPeriodMonthly),
		PeriodStart: start,
		PeriodEnd:   end,
		Amount:      500,
		Currency:    "USD",
		Rollover:    false,
		IsActive:    true,
	}
	require.NoError(t, db.Create(&tail).Error)

	require.NoError(t, svc.ProcessDueBudgetRollovers())

	var count int64
	require.NoError(t, db.Model(&Budget{}).Where(`"seriesId" = ?`, tail.SeriesId).Count(&count).Error)
	assert.Equal(t, int64(3), count) // two-months-ago + last month + current

	curStart, _ := periodBounds(string(common.BudgetPeriodMonthly), time.Now().UTC())
	var current Budget
	require.NoError(t, db.Where(`"seriesId" = ? AND "periodStart" = ?`, tail.SeriesId, curStart).First(&current).Error)
	assert.Equal(t, 500.0, current.Amount)
}

func TestRealDB_Budget_Rollover_CurrencyRestatement(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	cat := seedExpenseCategory(t, db, userId, "Food")
	svc := newSvc(db)

	// Set user base currency to EUR
	eurStr := "EUR"
	_, err := user.NewService(db).UpdateSettings(userId, user.UpdateSettingsDto{BaseCurrency: &eurStr})
	require.NoError(t, err)

	// Seed USD→EUR exchange rate (rate=0.92, so 500 USD → 460 EUR)
	rate := 0.92
	require.NoError(t, db.Create(&currency.ExchangeRate{
		BaseCurrency:   "USD",
		TargetCurrency: "EUR",
		Rate:           rate,
		FetchedAt:      time.Now().UTC(),
	}).Error)

	// Create active tail in last month denominated in USD
	tail := makeTailInLastMonth(t, db, userId, &cat, 500, 0, false)

	require.NoError(t, svc.ProcessDueBudgetRollovers())

	curStart, _ := periodBounds(string(common.BudgetPeriodMonthly), time.Now().UTC())
	var current Budget
	require.NoError(t, db.Where(`"seriesId" = ? AND "periodStart" = ?`, tail.SeriesId, curStart).First(&current).Error)
	assert.Equal(t, "EUR", current.Currency)
	assert.InDelta(t, 500*rate, current.Amount, 0.01)
}

func TestRealDB_Budget_Rollover_InactiveFutureDoesNotShadowTail(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	cat := seedExpenseCategory(t, db, userId, "Food")
	svc := newSvc(db)

	tail := makeTailInLastMonth(t, db, userId, &cat, 500, 0, false) // active, last month
	// An inactive future instance in the same series must not block rolling the active tail.
	nextStart := nextPeriodStart(string(common.BudgetPeriodMonthly), tail.PeriodStart)
	futureStart := nextPeriodStart(string(common.BudgetPeriodMonthly), nextStart) // two ahead of tail
	fs, fe := periodBounds(string(common.BudgetPeriodMonthly), futureStart)
	inactiveFuture := Budget{
		SeriesId:    tail.SeriesId,
		CategoryId:  &cat,
		UserId:      userId,
		PeriodType:  string(common.BudgetPeriodMonthly),
		PeriodStart: fs,
		PeriodEnd:   fe,
		Amount:      500,
		Currency:    "USD",
		Rollover:    false,
		IsActive:    true, // will be explicitly set to false below (GORM skips zero-value bool on Create)
	}
	require.NoError(t, db.Create(&inactiveFuture).Error)
	// GORM omits false (zero value for bool) when the column has default:true; use Update to force it.
	require.NoError(t, db.Model(&Budget{}).Where(`"id" = ?`, inactiveFuture.ID).Update("isActive", false).Error)

	require.NoError(t, svc.ProcessDueBudgetRollovers())

	curStart, _ := periodBounds(string(common.BudgetPeriodMonthly), time.Now().UTC())
	var current Budget
	err := db.Where(`"seriesId" = ? AND "periodStart" = ?`, tail.SeriesId, curStart).First(&current).Error
	require.NoError(t, err) // active tail rolled forward despite the inactive future row
	assert.True(t, current.IsActive)
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
	assert.Equal(t, "2026-06-01", got.PeriodStart.Format("2006-01-02"))
	assert.Equal(t, "2026-06-30", got.PeriodEnd.Format("2006-01-02"))
	assert.Equal(t, string(common.BudgetPeriodMonthly), got.PeriodType)
	assert.Equal(t, b.SeriesId, got.SeriesId)
	assert.Equal(t, b.CategoryId, got.CategoryId)
	assert.True(t, got.IsActive)
	assert.Equal(t, 0.0, got.CarriedOver)
}

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

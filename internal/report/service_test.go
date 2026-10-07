package report

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"ger_fin_api/internal/account"
	"ger_fin_api/internal/category"
	"ger_fin_api/internal/common"
	recurring_transaction "ger_fin_api/internal/recurring_transaction"
	"ger_fin_api/internal/testutil"
	"ger_fin_api/internal/transaction"
	"ger_fin_api/internal/user"
)

func seedUser(t *testing.T, db *gorm.DB) string {
	t.Helper()
	svc := user.NewService(db)
	u := &user.User{Email: "test-" + uuid.New().String() + "@example.com", Password: "hashed"}
	created, err := svc.Create(u)
	require.NoError(t, err)
	return created.ID
}

func seedAccount(t *testing.T, db *gorm.DB, userId string, balance float64) *account.Account {
	t.Helper()
	svc := account.NewService(db)
	a, err := svc.Create(userId, account.CreateAccountDto{
		Name:     "Account " + uuid.New().String(),
		Currency: "USD",
		Balance:  balance,
	})
	require.NoError(t, err)
	return a
}

func seedCategory(t *testing.T, db *gorm.DB, userId string, typ common.TransactionType) *category.Category {
	t.Helper()
	svc := category.NewService(db)
	cat, err := svc.Create(category.CreateCategoryDto{Name: "Cat " + uuid.New().String(), Type: typ}, userId)
	require.NoError(t, err)
	return cat
}

func seedDefaultCategories(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, category.NewService(db).SeedDefaultCategories())
}

func seedTx(t *testing.T, db *gorm.DB, userId, catId, accId string, typ common.TransactionType, amount float64, date time.Time) {
	t.Helper()
	svc := transaction.NewService(db)
	_, err := svc.Create(transaction.CreateTransactionDto{
		Type:            typ,
		Amount:          amount,
		Description:     "test",
		CategoryId:      catId,
		AccountId:       accId,
		TransactionDate: &date,
	}, userId)
	require.NoError(t, err)
}

func TestRealDB_Report_CategoryBreakdown(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db, nil)

	userId := seedUser(t, db)
	acc := seedAccount(t, db, userId, 1000)
	incomeCat := seedCategory(t, db, userId, common.TransactionTypeIncome)
	expenseCat := seedCategory(t, db, userId, common.TransactionTypeExpense)

	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := now

	seedTx(t, db, userId, incomeCat.ID, acc.ID, common.TransactionTypeIncome, 1000, now)
	seedTx(t, db, userId, expenseCat.ID, acc.ID, common.TransactionTypeExpense, -300, now)
	seedTx(t, db, userId, expenseCat.ID, acc.ID, common.TransactionTypeExpense, -200, now)

	resp, err := svc.CategoryBreakdown(userId, from, to, "USD")
	require.NoError(t, err)
	require.NotNil(t, resp)

	require.Len(t, resp.Income, 1)
	assert.Equal(t, 1000.0, resp.Income[0].Total)
	assert.Equal(t, 100.0, resp.Income[0].Percentage)

	require.Len(t, resp.Expense, 1)
	assert.Equal(t, -500.0, resp.Expense[0].Total)
	assert.Equal(t, 100.0, resp.Expense[0].Percentage)
}

func TestRealDB_Report_CategoryBreakdown_IsolatedByUser(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db, nil)

	userA := seedUser(t, db)
	userB := seedUser(t, db)
	accA := seedAccount(t, db, userA, 1000)
	accB := seedAccount(t, db, userB, 1000)
	catA := seedCategory(t, db, userA, common.TransactionTypeIncome)
	catB := seedCategory(t, db, userB, common.TransactionTypeIncome)

	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := now

	seedTx(t, db, userA, catA.ID, accA.ID, common.TransactionTypeIncome, 500, now)
	seedTx(t, db, userB, catB.ID, accB.ID, common.TransactionTypeIncome, 9999, now)

	resp, err := svc.CategoryBreakdown(userA, from, to, "USD")
	require.NoError(t, err)
	require.Len(t, resp.Income, 1)
	assert.Equal(t, 500.0, resp.Income[0].Total)
}

func TestRealDB_Report_CategoryBreakdown_ExcludesTransfers(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db, nil)
	seedDefaultCategories(t, db)
	txSvc := transaction.NewService(db)

	userId := seedUser(t, db)
	src := seedAccount(t, db, userId, 1000)
	dst := seedAccount(t, db, userId, 0)
	expenseCat := seedCategory(t, db, userId, common.TransactionTypeExpense)

	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := now

	seedTx(t, db, userId, expenseCat.ID, src.ID, common.TransactionTypeExpense, -100, now)

	_, err := txSvc.Transfer(transaction.CreateTransferDto{
		SourceAccountId:      src.ID,
		DestinationAccountId: dst.ID,
		Amount:               200,
		Description:          "Transfer",
	}, userId)
	require.NoError(t, err)

	resp, err := svc.CategoryBreakdown(userId, from, to, "USD")
	require.NoError(t, err)

	for _, row := range resp.Expense {
		assert.NotEqual(t, "Outgoing transfer", row.CategoryName)
	}
	for _, row := range resp.Income {
		assert.NotEqual(t, "Incoming transfer", row.CategoryName)
	}
}

func TestRealDB_Report_MonthlyTrend(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db, nil)

	userId := seedUser(t, db)
	acc := seedAccount(t, db, userId, 0)
	incomeCat := seedCategory(t, db, userId, common.TransactionTypeIncome)
	expenseCat := seedCategory(t, db, userId, common.TransactionTypeExpense)

	year := time.Now().Year()
	jan := time.Date(year, 1, 15, 0, 0, 0, 0, time.UTC)
	feb := time.Date(year, 2, 15, 0, 0, 0, 0, time.UTC)

	seedTx(t, db, userId, incomeCat.ID, acc.ID, common.TransactionTypeIncome, 1000, jan)
	seedTx(t, db, userId, expenseCat.ID, acc.ID, common.TransactionTypeExpense, -400, feb)

	from := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(year, 2, 28, 23, 59, 59, 0, time.UTC)

	result, err := svc.MonthlyTrend(userId, from, to, "USD")
	require.NoError(t, err)
	require.Len(t, result.Months, 2)

	assert.Equal(t, fmt.Sprintf("%d-01", year), result.Months[0].Month)
	assert.Equal(t, 1000.0, result.Months[0].Income)
	assert.Equal(t, 0.0, result.Months[0].Expense)
	assert.Equal(t, 1000.0, result.Months[0].Net)

	assert.Equal(t, fmt.Sprintf("%d-02", year), result.Months[1].Month)
	assert.Equal(t, 0.0, result.Months[1].Income)
	assert.Equal(t, -400.0, result.Months[1].Expense)
	assert.Equal(t, -400.0, result.Months[1].Net)
}

func TestRealDB_Report_MonthlyTrend_FillsEmptyMonths(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db, nil)

	userId := seedUser(t, db)
	acc := seedAccount(t, db, userId, 0)
	incomeCat := seedCategory(t, db, userId, common.TransactionTypeIncome)

	year := time.Now().Year()
	jan := time.Date(year, 1, 15, 0, 0, 0, 0, time.UTC)
	seedTx(t, db, userId, incomeCat.ID, acc.ID, common.TransactionTypeIncome, 500, jan)

	from := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(year, 3, 31, 23, 59, 59, 0, time.UTC)

	result, err := svc.MonthlyTrend(userId, from, to, "USD")
	require.NoError(t, err)
	require.Len(t, result.Months, 3)

	assert.Equal(t, 500.0, result.Months[0].Income)
	assert.Equal(t, 0.0, result.Months[1].Income)
	assert.Equal(t, 0.0, result.Months[1].Expense)
	assert.Equal(t, 0.0, result.Months[2].Income)
}

func TestRealDB_Report_NetWorth(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db, nil)

	userId := seedUser(t, db)
	seedAccount(t, db, userId, 5000)
	seedAccount(t, db, userId, 2000)

	resp, err := svc.NetWorth(userId, "USD")
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 7000.0, resp.Total)
	assert.Len(t, resp.Accounts, 2)
}

func TestRealDB_Report_NetWorth_IsolatedByUser(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db, nil)

	userA := seedUser(t, db)
	userB := seedUser(t, db)
	seedAccount(t, db, userA, 3000)
	seedAccount(t, db, userB, 9999)

	resp, err := svc.NetWorth(userA, "USD")
	require.NoError(t, err)
	assert.Equal(t, 3000.0, resp.Total)
	assert.Len(t, resp.Accounts, 1)
}

func TestRealDB_Report_TopCategories(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db, nil)

	userId := seedUser(t, db)
	acc := seedAccount(t, db, userId, 0)
	cat1 := seedCategory(t, db, userId, common.TransactionTypeExpense)
	cat2 := seedCategory(t, db, userId, common.TransactionTypeExpense)
	cat3 := seedCategory(t, db, userId, common.TransactionTypeExpense)

	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := now

	seedTx(t, db, userId, cat1.ID, acc.ID, common.TransactionTypeExpense, -100, now)
	seedTx(t, db, userId, cat2.ID, acc.ID, common.TransactionTypeExpense, -300, now)
	seedTx(t, db, userId, cat3.ID, acc.ID, common.TransactionTypeExpense, -200, now)

	results, err := svc.TopCategories(userId, from, to, 2, "USD")
	require.NoError(t, err)
	require.Len(t, results, 2)

	assert.Equal(t, -300.0, results[0].Total)
	assert.Equal(t, -200.0, results[1].Total)
}

func TestRealDB_Report_TopCategories_PercentageRelativeToAll(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db, nil)

	userId := seedUser(t, db)
	acc := seedAccount(t, db, userId, 0)
	cat1 := seedCategory(t, db, userId, common.TransactionTypeExpense)
	cat2 := seedCategory(t, db, userId, common.TransactionTypeExpense)

	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := now

	seedTx(t, db, userId, cat1.ID, acc.ID, common.TransactionTypeExpense, -600, now)
	seedTx(t, db, userId, cat2.ID, acc.ID, common.TransactionTypeExpense, -400, now)

	// limit=1 but percentage should be relative to all 1000 total expense
	results, err := svc.TopCategories(userId, from, to, 1, "USD")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, 60.0, results[0].Percentage)
}

func seedRecurringTx(t *testing.T, db *gorm.DB, userId, catId, accId string, typ common.TransactionType, amount float64, freq common.RecurrenceFrequency, start time.Time) *recurring_transaction.RecurringTransaction {
	t.Helper()
	txSvc := transaction.NewService(db)
	svc := recurring_transaction.NewService(db, txSvc)
	rt, err := svc.Create(recurring_transaction.CreateRecurringTransactionDto{
		Type:        typ,
		Amount:      amount,
		Description: "Recurring test",
		CategoryId:  catId,
		AccountId:   accId,
		Frequency:   freq,
		StartDate:   start,
	}, userId)
	require.NoError(t, err)
	return rt
}

func TestRealDB_CashFlow_Actual(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db, nil)

	userId := seedUser(t, db)
	acc := seedAccount(t, db, userId, 0)
	incomeCat := seedCategory(t, db, userId, common.TransactionTypeIncome)
	expenseCat := seedCategory(t, db, userId, common.TransactionTypeExpense)

	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := now

	seedTx(t, db, userId, incomeCat.ID, acc.ID, common.TransactionTypeIncome, 2000, now)
	seedTx(t, db, userId, expenseCat.ID, acc.ID, common.TransactionTypeExpense, -800, now)

	resp, err := svc.CashFlow(userId, from, to, 3, "USD")
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, 2000.0, resp.Actual.Income)
	assert.Equal(t, -800.0, resp.Actual.Expense)
	assert.Equal(t, 1200.0, resp.Actual.Net)
}

func TestRealDB_CashFlow_ProjectedRollingAverage(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db, nil)

	userId := seedUser(t, db)
	acc := seedAccount(t, db, userId, 0)
	incomeCat := seedCategory(t, db, userId, common.TransactionTypeIncome)

	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := now

	// Seed 3000 income in the 3-month lookback window (no recurring txs)
	m1 := from.AddDate(0, -2, 0)
	m2 := from.AddDate(0, -1, 0)
	seedTx(t, db, userId, incomeCat.ID, acc.ID, common.TransactionTypeIncome, 1500, m1)
	seedTx(t, db, userId, incomeCat.ID, acc.ID, common.TransactionTypeIncome, 1500, m2)

	resp, err := svc.CashFlow(userId, from, to, 3, "USD")
	require.NoError(t, err)

	// Rolling avg = 3000 / 3 = 1000/month; period ≈ current partial month
	assert.Greater(t, resp.Projected.Income, 0.0)
	assert.Equal(t, 0.0, resp.Recurring.Income)
}

func TestRealDB_CashFlow_RecurringComponent(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db, nil)

	userId := seedUser(t, db)
	acc := seedAccount(t, db, userId, 1000)
	incomeCat := seedCategory(t, db, userId, common.TransactionTypeIncome)

	// Recurring monthly income starting at the beginning of this month
	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(now.Year(), now.Month()+1, 0, 23, 59, 59, 0, time.UTC) // end of month

	seedRecurringTx(t, db, userId, incomeCat.ID, acc.ID, common.TransactionTypeIncome, 500, common.RecurrenceFrequencyMonthly, from)

	resp, err := svc.CashFlow(userId, from, to, 3, "USD")
	require.NoError(t, err)

	assert.Equal(t, 500.0, resp.Recurring.Income)
	assert.Greater(t, resp.Projected.Income, 0.0)
}

func TestRealDB_CashFlow_ExcludesTransfers(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db, nil)
	seedDefaultCategories(t, db)
	txSvc := transaction.NewService(db)

	userId := seedUser(t, db)
	src := seedAccount(t, db, userId, 1000)
	dst := seedAccount(t, db, userId, 0)
	expenseCat := seedCategory(t, db, userId, common.TransactionTypeExpense)

	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := now

	seedTx(t, db, userId, expenseCat.ID, src.ID, common.TransactionTypeExpense, -100, now)
	_, err := txSvc.Transfer(transaction.CreateTransferDto{
		SourceAccountId:      src.ID,
		DestinationAccountId: dst.ID,
		Amount:               200,
		Description:          "Transfer",
	}, userId)
	require.NoError(t, err)

	resp, err := svc.CashFlow(userId, from, to, 3, "USD")
	require.NoError(t, err)

	// Only the -100 expense should appear; transfer legs excluded
	assert.Equal(t, -100.0, resp.Actual.Expense)
	assert.Equal(t, 0.0, resp.Actual.Income)
}

func TestRealDB_CashFlow_IsolatedByUser(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db, nil)

	userA := seedUser(t, db)
	userB := seedUser(t, db)
	accA := seedAccount(t, db, userA, 0)
	accB := seedAccount(t, db, userB, 0)
	catA := seedCategory(t, db, userA, common.TransactionTypeIncome)
	catB := seedCategory(t, db, userB, common.TransactionTypeIncome)

	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := now

	seedTx(t, db, userA, catA.ID, accA.ID, common.TransactionTypeIncome, 500, now)
	seedTx(t, db, userB, catB.ID, accB.ID, common.TransactionTypeIncome, 9999, now)

	resp, err := svc.CashFlow(userA, from, to, 3, "USD")
	require.NoError(t, err)
	assert.Equal(t, 500.0, resp.Actual.Income)
}

func TestRealDB_YearOverYear(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db, nil)

	userId := seedUser(t, db)
	acc := seedAccount(t, db, userId, 0)
	incomeCat := seedCategory(t, db, userId, common.TransactionTypeIncome)
	expenseCat := seedCategory(t, db, userId, common.TransactionTypeExpense)

	year := time.Now().Year()
	jan := func(y int) time.Time { return time.Date(y, 1, 15, 0, 0, 0, 0, time.UTC) }

	seedTx(t, db, userId, incomeCat.ID, acc.ID, common.TransactionTypeIncome, 1000, jan(year-1))
	seedTx(t, db, userId, expenseCat.ID, acc.ID, common.TransactionTypeExpense, -400, jan(year-1))
	seedTx(t, db, userId, incomeCat.ID, acc.ID, common.TransactionTypeIncome, 1200, jan(year))

	from := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(year, 3, 31, 23, 59, 59, 0, time.UTC)

	resp, err := svc.YearOverYear(userId, from, to, 2, "USD")
	require.NoError(t, err)
	require.NotNil(t, resp)

	require.Len(t, resp.Years, 2)
	assert.Equal(t, year-1, resp.Years[0].Year)
	assert.Equal(t, year, resp.Years[1].Year)

	// January of year-1
	require.NotEmpty(t, resp.Years[0].Data)
	jan0 := resp.Years[0].Data[0]
	assert.Equal(t, fmt.Sprintf("%d-01", year-1), jan0.Month)
	assert.Equal(t, 1000.0, jan0.Income)
	assert.Equal(t, -400.0, jan0.Expense)

	// January of current year
	jan1 := resp.Years[1].Data[0]
	assert.Equal(t, fmt.Sprintf("%d-01", year), jan1.Month)
	assert.Equal(t, 1200.0, jan1.Income)
}

func TestRealDB_YearOverYear_FillsEmptyMonths(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db, nil)

	userId := seedUser(t, db)
	acc := seedAccount(t, db, userId, 0)
	incomeCat := seedCategory(t, db, userId, common.TransactionTypeIncome)

	year := time.Now().Year()
	seedTx(t, db, userId, incomeCat.ID, acc.ID, common.TransactionTypeIncome, 500, time.Date(year, 1, 15, 0, 0, 0, 0, time.UTC))

	from := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(year, 3, 31, 23, 59, 59, 0, time.UTC)

	resp, err := svc.YearOverYear(userId, from, to, 1, "USD")
	require.NoError(t, err)

	require.Len(t, resp.Years, 1)
	require.Len(t, resp.Years[0].Data, 3) // Jan, Feb, Mar

	assert.Equal(t, 500.0, resp.Years[0].Data[0].Income)
	assert.Equal(t, 0.0, resp.Years[0].Data[1].Income)
	assert.Equal(t, 0.0, resp.Years[0].Data[2].Income)
}

func TestRealDB_YearOverYear_MonthsAxisAnchored(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db, nil)

	userId := seedUser(t, db)

	year := time.Now().Year()
	from := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(year, 3, 31, 23, 59, 59, 0, time.UTC)

	resp, err := svc.YearOverYear(userId, from, to, 2, "USD")
	require.NoError(t, err)

	require.Len(t, resp.Months, 3)
	assert.Equal(t, fmt.Sprintf("%d-01", year), resp.Months[0])
	assert.Equal(t, fmt.Sprintf("%d-02", year), resp.Months[1])
	assert.Equal(t, fmt.Sprintf("%d-03", year), resp.Months[2])
}

func TestRealDB_YearOverYear_IsolatedByUser(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db, nil)

	userA := seedUser(t, db)
	userB := seedUser(t, db)
	accA := seedAccount(t, db, userA, 0)
	accB := seedAccount(t, db, userB, 0)
	catA := seedCategory(t, db, userA, common.TransactionTypeIncome)
	catB := seedCategory(t, db, userB, common.TransactionTypeIncome)

	year := time.Now().Year()
	seedTx(t, db, userA, catA.ID, accA.ID, common.TransactionTypeIncome, 300, time.Date(year, 1, 15, 0, 0, 0, 0, time.UTC))
	seedTx(t, db, userB, catB.ID, accB.ID, common.TransactionTypeIncome, 9999, time.Date(year, 1, 15, 0, 0, 0, 0, time.UTC))

	from := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(year, 1, 31, 23, 59, 59, 0, time.UTC)

	resp, err := svc.YearOverYear(userA, from, to, 1, "USD")
	require.NoError(t, err)

	require.Len(t, resp.Years, 1)
	assert.Equal(t, 300.0, resp.Years[0].Data[0].Income)
}

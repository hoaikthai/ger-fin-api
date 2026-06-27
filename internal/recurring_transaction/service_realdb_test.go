package recurring_transaction

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"ger_fin_api/internal/account"
	"ger_fin_api/internal/category"
	"ger_fin_api/internal/common"
	"ger_fin_api/internal/testutil"
	"ger_fin_api/internal/transaction"
	"ger_fin_api/internal/user"
	"gorm.io/gorm"
)

func seedUser(t *testing.T, db *gorm.DB) string {
	t.Helper()
	userSvc := user.NewService(db)
	u := &user.User{Email: "test-" + uuid.New().String() + "@example.com", Password: "hashed"}
	created, err := userSvc.Create(u)
	require.NoError(t, err)
	return created.ID
}

func seedAccount(t *testing.T, db *gorm.DB, userId string) string {
	t.Helper()
	svc := account.NewService(db)
	a, err := svc.Create(userId, account.CreateAccountDto{Name: "Test", Currency: "USD", Balance: 1000})
	require.NoError(t, err)
	return a.ID
}

func seedIncomeCategory(t *testing.T, db *gorm.DB, userId string) string {
	t.Helper()
	svc := category.NewService(db)
	cat, err := svc.Create(category.CreateCategoryDto{Name: "Salary", Type: common.TransactionTypeIncome}, userId)
	require.NoError(t, err)
	return cat.ID
}

func seedExpenseCategory(t *testing.T, db *gorm.DB, userId string) string {
	t.Helper()
	svc := category.NewService(db)
	cat, err := svc.Create(category.CreateCategoryDto{Name: "Rent", Type: common.TransactionTypeExpense}, userId)
	require.NoError(t, err)
	return cat.ID
}

func newRTSvc(db *gorm.DB) *Service {
	return NewService(db, transaction.NewService(db))
}

func TestRealDB_RecurringTransaction_Create(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	accountId := seedAccount(t, db, userId)
	categoryId := seedIncomeCategory(t, db, userId)

	svc := newRTSvc(db)
	startDate := time.Now().Truncate(time.Second).UTC()

	rt, err := svc.Create(CreateRecurringTransactionDto{
		Type:        common.TransactionTypeIncome,
		Amount:      200.0,
		Description: "Monthly salary",
		CategoryId:  categoryId,
		AccountId:   accountId,
		Frequency:   common.RecurrenceFrequencyMonthly,
		StartDate:   startDate,
	}, userId)

	require.NoError(t, err)
	assert.NotEmpty(t, rt.ID)
	assert.Equal(t, "income", rt.Type)
	assert.Equal(t, 200.0, rt.Amount)
	assert.Equal(t, "Monthly salary", rt.Description)
	assert.Equal(t, categoryId, rt.CategoryId)
	assert.Equal(t, accountId, rt.AccountId)
	assert.Equal(t, userId, rt.UserId)
	assert.True(t, rt.IsActive)
	require.NotNil(t, rt.NextDueDate)
	assert.Equal(t, startDate.Unix(), rt.NextDueDate.Unix())

	var fromDB RecurringTransaction
	err = db.First(&fromDB, "id = ?", rt.ID).Error
	require.NoError(t, err)
	require.NotNil(t, fromDB.NextDueDate)
	assert.Equal(t, startDate.Unix(), fromDB.NextDueDate.Unix())
}

func TestRealDB_RecurringTransaction_FindAll(t *testing.T) {
	db := testutil.NewRealDB(t)

	userA := seedUser(t, db)
	accA := seedAccount(t, db, userA)
	catA := seedIncomeCategory(t, db, userA)

	userB := seedUser(t, db)
	accB := seedAccount(t, db, userB)
	catB := seedIncomeCategory(t, db, userB)

	svc := newRTSvc(db)
	startDate := time.Now()

	for range 2 {
		_, err := svc.Create(CreateRecurringTransactionDto{
			Type:        common.TransactionTypeIncome,
			Amount:      100.0,
			Description: "RT for A",
			CategoryId:  catA,
			AccountId:   accA,
			Frequency:   common.RecurrenceFrequencyMonthly,
			StartDate:   startDate,
		}, userA)
		require.NoError(t, err)
	}

	_, err := svc.Create(CreateRecurringTransactionDto{
		Type:        common.TransactionTypeIncome,
		Amount:      100.0,
		Description: "RT for B",
		CategoryId:  catB,
		AccountId:   accB,
		Frequency:   common.RecurrenceFrequencyMonthly,
		StartDate:   startDate,
	}, userB)
	require.NoError(t, err)

	results, err := svc.FindAll(userA)
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestRealDB_RecurringTransaction_FindOne_Success(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	accountId := seedAccount(t, db, userId)
	categoryId := seedIncomeCategory(t, db, userId)

	svc := newRTSvc(db)
	rt, err := svc.Create(CreateRecurringTransactionDto{
		Type:        common.TransactionTypeIncome,
		Amount:      50.0,
		Description: "Test RT",
		CategoryId:  categoryId,
		AccountId:   accountId,
		Frequency:   common.RecurrenceFrequencyWeekly,
		StartDate:   time.Now(),
	}, userId)
	require.NoError(t, err)

	found, err := svc.FindOne(rt.ID, userId)
	require.NoError(t, err)
	assert.Equal(t, rt.ID, found.ID)
	assert.Equal(t, "Test RT", found.Description)
}

func TestRealDB_RecurringTransaction_FindOne_NotFound(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)

	svc := newRTSvc(db)
	_, err := svc.FindOne(uuid.New().String(), userId)
	require.Error(t, err)

	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 404, appErr.Code)
}

func TestRealDB_RecurringTransaction_FindOne_WrongUser(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	accountId := seedAccount(t, db, userId)
	categoryId := seedIncomeCategory(t, db, userId)

	otherUserId := seedUser(t, db)

	svc := newRTSvc(db)
	rt, err := svc.Create(CreateRecurringTransactionDto{
		Type:        common.TransactionTypeIncome,
		Amount:      75.0,
		Description: "Private RT",
		CategoryId:  categoryId,
		AccountId:   accountId,
		Frequency:   common.RecurrenceFrequencyDaily,
		StartDate:   time.Now(),
	}, userId)
	require.NoError(t, err)

	_, err = svc.FindOne(rt.ID, otherUserId)
	require.Error(t, err)

	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 404, appErr.Code)
}

func TestRealDB_RecurringTransaction_Update(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	accountId := seedAccount(t, db, userId)
	categoryId := seedIncomeCategory(t, db, userId)

	svc := newRTSvc(db)
	rt, err := svc.Create(CreateRecurringTransactionDto{
		Type:        common.TransactionTypeIncome,
		Amount:      100.0,
		Description: "Original description",
		CategoryId:  categoryId,
		AccountId:   accountId,
		Frequency:   common.RecurrenceFrequencyMonthly,
		StartDate:   time.Now(),
	}, userId)
	require.NoError(t, err)

	newDesc := "Updated description"
	newIsActive := false
	updated, err := svc.Update(rt.ID, UpdateRecurringTransactionDto{
		Description: &newDesc,
		IsActive:    &newIsActive,
	}, userId)
	require.NoError(t, err)
	assert.Equal(t, "Updated description", updated.Description)
	assert.False(t, updated.IsActive)

	var fromDB RecurringTransaction
	err = db.First(&fromDB, "id = ?", rt.ID).Error
	require.NoError(t, err)
	assert.Equal(t, "Updated description", fromDB.Description)
	assert.False(t, fromDB.IsActive)
}

func TestRealDB_RecurringTransaction_Remove(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	accountId := seedAccount(t, db, userId)
	categoryId := seedIncomeCategory(t, db, userId)

	svc := newRTSvc(db)
	rt, err := svc.Create(CreateRecurringTransactionDto{
		Type:        common.TransactionTypeIncome,
		Amount:      60.0,
		Description: "To be removed",
		CategoryId:  categoryId,
		AccountId:   accountId,
		Frequency:   common.RecurrenceFrequencyYearly,
		StartDate:   time.Now(),
	}, userId)
	require.NoError(t, err)

	err = svc.Remove(rt.ID, userId)
	require.NoError(t, err)

	_, err = svc.FindOne(rt.ID, userId)
	require.Error(t, err)
	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 404, appErr.Code)

	var fromDB RecurringTransaction
	err = db.Unscoped().First(&fromDB, "id = ?", rt.ID).Error
	require.NoError(t, err)
	assert.True(t, fromDB.DeletedAt.Valid)
}

func TestRealDB_RecurringTransaction_ProcessDue_Income(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	accountId := seedAccount(t, db, userId)
	categoryId := seedIncomeCategory(t, db, userId)

	svc := newRTSvc(db)
	pastDue := time.Now().Add(-1 * time.Hour)

	rt, err := svc.Create(CreateRecurringTransactionDto{
		Type:        common.TransactionTypeIncome,
		Amount:      300.0,
		Description: "Monthly income",
		CategoryId:  categoryId,
		AccountId:   accountId,
		Frequency:   common.RecurrenceFrequencyMonthly,
		StartDate:   pastDue,
	}, userId)
	require.NoError(t, err)
	require.NotNil(t, rt.NextDueDate)

	previousNextDue := *rt.NextDueDate

	err = svc.ProcessDueRecurringTransactions()
	require.NoError(t, err)

	var txs []transaction.Transaction
	err = db.Where("\"userId\" = ?", userId).Find(&txs).Error
	require.NoError(t, err)
	assert.Len(t, txs, 1)
	assert.Equal(t, 300.0, txs[0].Amount)
	assert.Equal(t, "income", txs[0].Type)

	var updatedRT RecurringTransaction
	err = db.First(&updatedRT, "id = ?", rt.ID).Error
	require.NoError(t, err)
	require.NotNil(t, updatedRT.NextDueDate)
	expectedNext := previousNextDue.AddDate(0, 1, 0)
	assert.Equal(t, expectedNext.Unix(), updatedRT.NextDueDate.Unix())
}

func TestRealDB_RecurringTransaction_ProcessDue_Expense(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	accountId := seedAccount(t, db, userId)
	categoryId := seedExpenseCategory(t, db, userId)

	svc := newRTSvc(db)
	pastDue := time.Now().Add(-1 * time.Hour)

	rt, err := svc.Create(CreateRecurringTransactionDto{
		Type:        common.TransactionTypeExpense,
		Amount:      50.0,
		Description: "Monthly rent",
		CategoryId:  categoryId,
		AccountId:   accountId,
		Frequency:   common.RecurrenceFrequencyMonthly,
		StartDate:   pastDue,
	}, userId)
	require.NoError(t, err)
	require.NotNil(t, rt.NextDueDate)

	previousNextDue := *rt.NextDueDate

	err = svc.ProcessDueRecurringTransactions()
	require.NoError(t, err)

	var txs []transaction.Transaction
	err = db.Where("\"userId\" = ?", userId).Find(&txs).Error
	require.NoError(t, err)
	require.Len(t, txs, 1)
	assert.Equal(t, -50.0, txs[0].Amount)
	assert.Equal(t, "expense", txs[0].Type)

	var updatedRT RecurringTransaction
	err = db.First(&updatedRT, "id = ?", rt.ID).Error
	require.NoError(t, err)
	require.NotNil(t, updatedRT.NextDueDate)
	expectedNext := previousNextDue.AddDate(0, 1, 0)
	assert.Equal(t, expectedNext.Unix(), updatedRT.NextDueDate.Unix())
}

func TestRealDB_RecurringTransaction_ProcessDue_Expired(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	accountId := seedAccount(t, db, userId)
	categoryId := seedIncomeCategory(t, db, userId)

	svc := newRTSvc(db)
	pastDue := time.Now().Add(-1 * time.Hour)
	endDate := time.Now().Add(-24 * time.Hour)

	rt, err := svc.Create(CreateRecurringTransactionDto{
		Type:        common.TransactionTypeIncome,
		Amount:      100.0,
		Description: "Expired RT",
		CategoryId:  categoryId,
		AccountId:   accountId,
		Frequency:   common.RecurrenceFrequencyMonthly,
		StartDate:   pastDue,
		EndDate:     &endDate,
	}, userId)
	require.NoError(t, err)

	err = svc.ProcessDueRecurringTransactions()
	require.NoError(t, err)

	var txs []transaction.Transaction
	err = db.Where("\"userId\" = ?", userId).Find(&txs).Error
	require.NoError(t, err)
	assert.Len(t, txs, 0)

	var updatedRT RecurringTransaction
	err = db.First(&updatedRT, "id = ?", rt.ID).Error
	require.NoError(t, err)
	assert.False(t, updatedRT.IsActive)
}

func TestRealDB_RecurringTransaction_ProcessDue_NotYetDue(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	accountId := seedAccount(t, db, userId)
	categoryId := seedIncomeCategory(t, db, userId)

	svc := newRTSvc(db)
	futureDue := time.Now().Add(24 * time.Hour)

	_, err := svc.Create(CreateRecurringTransactionDto{
		Type:        common.TransactionTypeIncome,
		Amount:      100.0,
		Description: "Not yet due RT",
		CategoryId:  categoryId,
		AccountId:   accountId,
		Frequency:   common.RecurrenceFrequencyMonthly,
		StartDate:   futureDue,
	}, userId)
	require.NoError(t, err)

	err = svc.ProcessDueRecurringTransactions()
	require.NoError(t, err)

	var txs []transaction.Transaction
	err = db.Where("\"userId\" = ?", userId).Find(&txs).Error
	require.NoError(t, err)
	assert.Len(t, txs, 0)
}

package recurring_transaction

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/common"
	"ger_fin_api/internal/testutil"
	"ger_fin_api/internal/transaction"
)

// Regression: another user's account could be attached and then read back
// through the preloaded Account on list/get.
func TestRealDB_RecurringTransaction_Create_RejectsForeignAccount(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	otherId := seedUser(t, db)
	foreignAccount := seedAccount(t, db, otherId)
	categoryId := seedIncomeCategory(t, db, userId)
	svc := newRTSvc(db)

	_, err := svc.Create(CreateRecurringTransactionDto{
		Type: common.TransactionTypeIncome, Amount: 10, Description: "x", CategoryId: categoryId,
		AccountId: foreignAccount, Frequency: common.RecurrenceFrequencyMonthly, StartDate: time.Now(),
	}, userId)
	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, 404, appErr.Code)
}

func TestRealDB_RecurringTransaction_Update_RejectsForeignCategory(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	otherId := seedUser(t, db)
	accountId := seedAccount(t, db, userId)
	categoryId := seedIncomeCategory(t, db, userId)
	foreignCategory := seedIncomeCategory(t, db, otherId)
	svc := newRTSvc(db)

	rt, err := svc.Create(CreateRecurringTransactionDto{
		Type: common.TransactionTypeIncome, Amount: 10, Description: "x", CategoryId: categoryId,
		AccountId: accountId, Frequency: common.RecurrenceFrequencyMonthly, StartDate: time.Now(),
	}, userId)
	require.NoError(t, err)

	_, err = svc.Update(rt.ID, UpdateRecurringTransactionDto{CategoryId: &foreignCategory}, userId)
	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, 404, appErr.Code)
}

// Missed runs are caught up, each occurrence dated on its due date.
func TestRealDB_RecurringTransaction_ProcessDue_CatchesUp(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	accountId := seedAccount(t, db, userId)
	categoryId := seedIncomeCategory(t, db, userId)
	svc := newRTSvc(db)

	start := time.Now().AddDate(0, 0, -2).Add(-time.Hour) // due 3 times: start, +1d, +2d
	_, err := svc.Create(CreateRecurringTransactionDto{
		Type: common.TransactionTypeIncome, Amount: 1, Description: "daily", CategoryId: categoryId,
		AccountId: accountId, Frequency: common.RecurrenceFrequencyDaily, StartDate: start,
	}, userId)
	require.NoError(t, err)

	require.NoError(t, svc.ProcessDueRecurringTransactions())

	var txs []transaction.Transaction
	require.NoError(t, db.Where(`"userId" = ?`, userId).Order(`"transactionDate" ASC`).Find(&txs).Error)
	require.Len(t, txs, 3)
	assert.Equal(t, 24*time.Hour, txs[1].TransactionDate.Sub(txs[0].TransactionDate))
	assert.Equal(t, 24*time.Hour, txs[2].TransactionDate.Sub(txs[1].TransactionDate))

	// A second run generates nothing new.
	require.NoError(t, svc.ProcessDueRecurringTransactions())
	var count int64
	require.NoError(t, db.Model(&transaction.Transaction{}).Where(`"userId" = ?`, userId).Count(&count).Error)
	assert.EqualValues(t, 3, count)
}

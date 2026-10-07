package recurring_transaction

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/common"
	"ger_fin_api/internal/testutil"
	"ger_fin_api/internal/transaction"
)

func appCode(t *testing.T, err error) int {
	t.Helper()
	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	return appErr.Code
}

func TestRealDB_Sync_RecurringTransaction(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db, transaction.NewService(db))
	userId := testutil.SeedUser(t, db)
	accId := testutil.SeedAccount(t, db, userId, "USD")
	catId := testutil.SeedCategory(t, db, userId, "expense")
	id := uuid.NewString()
	dto := CreateRecurringTransactionDto{
		CreateMeta: common.CreateMeta{ID: &id},
		Type:       common.TransactionTypeExpense, Amount: 9, Description: "Netflix",
		CategoryId: catId, AccountId: accId, Frequency: common.RecurrenceFrequencyMonthly,
		StartDate: time.Now().AddDate(0, 1, 0),
	}

	first, err := svc.Create(dto, userId)
	require.NoError(t, err)
	second, err := svc.Create(dto, userId)
	require.NoError(t, err)
	assert.Equal(t, id, first.ID)
	assert.Equal(t, id, second.ID)

	t2 := time.Now().Add(-time.Minute)
	t1 := t2.Add(-time.Minute)
	newer, older := "Netflix HD", "Old"
	_, err = svc.Update(id, UpdateRecurringTransactionDto{UpdateMeta: common.UpdateMeta{ClientModifiedAt: &t2}, Description: &newer}, userId)
	require.NoError(t, err)
	got, err := svc.Update(id, UpdateRecurringTransactionDto{UpdateMeta: common.UpdateMeta{ClientModifiedAt: &t1}, Description: &older}, userId)
	require.NoError(t, err)
	assert.Equal(t, "Netflix HD", got.Description)

	require.NoError(t, svc.Remove(id, userId))
	require.NoError(t, svc.Remove(id, userId))
	_, err = svc.Update(id, UpdateRecurringTransactionDto{Description: &newer}, userId)
	assert.Equal(t, 410, appCode(t, err))
}

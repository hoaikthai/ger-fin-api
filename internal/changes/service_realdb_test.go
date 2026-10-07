package changes

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/account"
	"ger_fin_api/internal/category"
	"ger_fin_api/internal/common"
	recurringTx "ger_fin_api/internal/recurring_transaction"
	"ger_fin_api/internal/testutil"
	"ger_fin_api/internal/transaction"
)

func idsOf[T any](rows []T, id func(T) string) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = id(r)
	}
	return out
}

func accountIds(r *Response) []string {
	return idsOf(r.Changes.Accounts, func(a account.Account) string { return a.ID })
}
func categoryIds(r *Response) []string {
	return idsOf(r.Changes.Categories, func(c category.Category) string { return c.ID })
}
func transactionIds(r *Response) []string {
	return idsOf(r.Changes.Transactions, func(t transaction.Transaction) string { return t.ID })
}

func TestRealDB_Changes_FirstPullIsScopedToUserPlusDefaults(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userId := testutil.SeedUser(t, db)
	other := testutil.SeedUser(t, db)
	accId := testutil.SeedAccount(t, db, userId, "USD")
	catId := testutil.SeedCategory(t, db, userId, "expense")
	otherAcc := testutil.SeedAccount(t, db, other, "USD")
	var defaultCat string
	require.NoError(t, db.Raw(`INSERT INTO "category" ("name","type","isDefault") VALUES ('Default Y','expense',true) RETURNING "id"`).Scan(&defaultCat).Error)

	res, err := svc.Changes(userId, 0, 1000)
	require.NoError(t, err)
	assert.Contains(t, accountIds(res), accId)
	assert.NotContains(t, accountIds(res), otherAcc)
	assert.Contains(t, categoryIds(res), catId)
	assert.Contains(t, categoryIds(res), defaultCat)
	assert.False(t, res.HasMore)
	assert.Positive(t, res.Cursor)
	for _, a := range res.Changes.Accounts {
		assert.Positive(t, a.Seq)
	}
}

func TestRealDB_Changes_CursorReturnsOnlyNewerRows(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userId := testutil.SeedUser(t, db)
	a1 := testutil.SeedAccount(t, db, userId, "USD")
	a2 := testutil.SeedAccount(t, db, userId, "USD")

	first, err := svc.Changes(userId, 0, 1000)
	require.NoError(t, err)
	require.NoError(t, db.Exec(`UPDATE "account" SET "name" = 'Changed' WHERE "id" = ?`, a2).Error)

	next, err := svc.Changes(userId, first.Cursor, 1000)
	require.NoError(t, err)
	assert.Equal(t, []string{a2}, accountIds(next))
	assert.NotContains(t, accountIds(next), a1)
	assert.Greater(t, next.Cursor, first.Cursor)

	empty, err := svc.Changes(userId, next.Cursor, 1000)
	require.NoError(t, err)
	assert.Empty(t, accountIds(empty))
	assert.Equal(t, next.Cursor, empty.Cursor, "cursor unchanged when nothing is new")
}

func TestRealDB_Changes_PagingDeliversEverythingOnce(t *testing.T) {
	db := testutil.NewRealDB(t)
	require.NoError(t, category.NewService(db).SeedDefaultCategories())
	svc := NewService(db)
	txSvc := transaction.NewService(db)
	userId := testutil.SeedUser(t, db)
	src := testutil.SeedAccount(t, db, userId, "USD")
	dst := testutil.SeedAccount(t, db, userId, "USD")
	tr, err := txSvc.Transfer(transaction.CreateTransferDto{SourceAccountId: src, DestinationAccountId: dst, Amount: 5, Description: "move"}, userId)
	require.NoError(t, err)

	seen := map[string]int{}
	related := map[string]string{}
	var cursor int64
	for page := 0; page < 1000; page++ {
		res, err := svc.Changes(userId, cursor, 2)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(accountIds(res))+len(categoryIds(res))+len(transactionIds(res))+len(res.Deleted), 2)
		for _, tx := range res.Changes.Transactions {
			seen[tx.ID]++
			if tx.RelatedTransactionId != nil {
				related[tx.ID] = *tx.RelatedTransactionId
			}
		}
		if res.HasMore {
			assert.Greater(t, res.Cursor, cursor)
		}
		cursor = res.Cursor
		if !res.HasMore {
			break
		}
	}
	srcId, dstId := tr.SourceTransaction.ID, tr.DestinationTransaction.ID
	assert.Equal(t, 1, seen[srcId])
	assert.Equal(t, 1, seen[dstId])
	assert.Equal(t, dstId, related[srcId])
	assert.Equal(t, srcId, related[dstId])
}

func TestRealDB_Changes_DeletedAccountKeepsTransactions(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userId := testutil.SeedUser(t, db)
	accId := testutil.SeedAccount(t, db, userId, "USD")
	catId := testutil.SeedCategory(t, db, userId, "expense")
	tx, err := transaction.NewService(db).Create(transaction.CreateTransactionDto{
		Type: common.TransactionTypeExpense, Amount: -3, Description: "x", CategoryId: catId, AccountId: accId,
	}, userId)
	require.NoError(t, err)
	require.NoError(t, account.NewService(db).Remove(accId, userId))

	res, err := svc.Changes(userId, 0, 1000)
	require.NoError(t, err)
	assert.Contains(t, res.Deleted, DeletedRef{Entity: "account", ID: accId})
	assert.NotContains(t, accountIds(res), accId)
	assert.Contains(t, transactionIds(res), tx.ID)
}

func TestRealDB_Changes_IncludesSchedulerCreatedTransactions(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	txSvc := transaction.NewService(db)
	userId := testutil.SeedUser(t, db)
	accId := testutil.SeedAccount(t, db, userId, "USD")
	catId := testutil.SeedCategory(t, db, userId, "expense")
	rtSvc := recurringTx.NewService(db, txSvc)
	_, err := rtSvc.Create(recurringTx.CreateRecurringTransactionDto{
		Type: common.TransactionTypeExpense, Amount: 4, Description: "Gym",
		CategoryId: catId, AccountId: accId, Frequency: common.RecurrenceFrequencyMonthly,
		StartDate: time.Now().Add(-time.Hour),
	}, userId)
	require.NoError(t, err)
	before, err := svc.Changes(userId, 0, 1000)
	require.NoError(t, err)

	require.NoError(t, rtSvc.ProcessDueRecurringTransactions())

	after, err := svc.Changes(userId, before.Cursor, 1000)
	require.NoError(t, err)
	require.NotEmpty(t, after.Changes.Transactions)
	assert.Equal(t, "Gym (Auto-generated)", after.Changes.Transactions[0].Description)
}

func TestRealDB_Changes_CursorAheadIsConflict(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userId := testutil.SeedUser(t, db)

	_, err := svc.Changes(userId, 1<<62, 10)
	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, 409, appErr.Code)
}

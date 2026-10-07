package transaction

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"ger_fin_api/internal/account"
	"ger_fin_api/internal/common"
	"ger_fin_api/internal/testutil"
)

func TestRealDB_Transfer_LinksAndDeletesBothSides(t *testing.T) {
	db := testutil.NewRealDB(t)
	seedDefaultCategories(t, db)
	userId := seedUser(t, db)
	src := seedAccountNamed(t, db, userId, "Src")
	dst := seedAccountNamed(t, db, userId, "Dst")
	svc := NewService(db)

	res, err := svc.Transfer(CreateTransferDto{
		SourceAccountId: src.ID, DestinationAccountId: dst.ID, Amount: 25, Description: "move",
	}, userId)
	require.NoError(t, err)

	out, err := svc.FindOne(res.SourceTransaction.ID, userId)
	require.NoError(t, err)
	require.NotNil(t, out.RelatedTransactionId)
	assert.Equal(t, res.DestinationTransaction.ID, *out.RelatedTransactionId)

	require.NoError(t, svc.Remove(res.SourceTransaction.ID, userId))
	_, err = svc.FindOne(res.DestinationTransaction.ID, userId)
	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, 404, appErr.Code)
}

func TestRealDB_Update_MovesTransactionToAnotherAccount(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	a1 := seedAccountNamed(t, db, userId, "A1")
	a2 := seedAccountNamed(t, db, userId, "A2")
	cat := seedCategory(t, db, userId, common.TransactionTypeExpense)
	svc := NewService(db)

	tx, err := svc.Create(CreateTransactionDto{
		Type: common.TransactionTypeExpense, Amount: -5, Description: "x", CategoryId: cat.ID, AccountId: a1.ID,
	}, userId)
	require.NoError(t, err)

	updated, err := svc.Update(tx.ID, UpdateTransactionDto{AccountId: &a2.ID}, userId)
	require.NoError(t, err)
	assert.Equal(t, a2.ID, updated.AccountId)
	require.NotNil(t, updated.Account)
	assert.Equal(t, a2.ID, updated.Account.ID)

	reloaded, err := svc.FindOne(tx.ID, userId)
	require.NoError(t, err)
	assert.Equal(t, a2.ID, reloaded.AccountId)
}

// categoryMapping must not resolve to another user's private category.
func TestRealDB_ValidateCsv_IgnoresForeignCategoryMapping(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	otherId := seedUser(t, db)
	seedAccountNamed(t, db, userId, "Cash")
	foreign := seedCategory(t, db, otherId, common.TransactionTypeExpense)
	svc := NewService(db)

	csv := "Date,Category,Amount,Currency,Wallet\n2026-01-02,Mystery,-5,USD,Cash\n"
	res, err := svc.ValidateCsv([]byte(csv), userId, map[string]string{"Mystery": foreign.ID})
	require.NoError(t, err)
	assert.False(t, res.Valid)
	assert.Equal(t, []string{"Mystery"}, res.UnknownCategories)
}

func accountBalance(t *testing.T, db *gorm.DB, id string) float64 {
	t.Helper()
	var a account.Account
	require.NoError(t, db.First(&a, "id = ?", id).Error)
	return a.Balance
}

func TestRealDB_Balance_TracksCreateUpdateRemove(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	a1 := seedAccountNamed(t, db, userId, "A1") // balance 1000
	a2 := seedAccountNamed(t, db, userId, "A2") // balance 1000
	expense := seedCategory(t, db, userId, common.TransactionTypeExpense)
	income := seedCategory(t, db, userId, common.TransactionTypeIncome)
	svc := NewService(db)

	tx, err := svc.Create(CreateTransactionDto{
		Type: common.TransactionTypeExpense, Amount: -40, Description: "x", CategoryId: expense.ID, AccountId: a1.ID,
	}, userId)
	require.NoError(t, err)
	assert.Equal(t, 960.0, accountBalance(t, db, a1.ID))
	assert.Equal(t, 960.0, tx.Account.Balance)

	_, err = svc.Create(CreateTransactionDto{
		Type: common.TransactionTypeIncome, Amount: 15, Description: "y", CategoryId: income.ID, AccountId: a1.ID,
	}, userId)
	require.NoError(t, err)
	assert.Equal(t, 975.0, accountBalance(t, db, a1.ID))

	// Change amount and move to another account: old effect reversed, new applied.
	newAmount := -100.0
	_, err = svc.Update(tx.ID, UpdateTransactionDto{Amount: &newAmount, AccountId: &a2.ID}, userId)
	require.NoError(t, err)
	assert.Equal(t, 1015.0, accountBalance(t, db, a1.ID))
	assert.Equal(t, 900.0, accountBalance(t, db, a2.ID))

	require.NoError(t, svc.Remove(tx.ID, userId))
	assert.Equal(t, 1000.0, accountBalance(t, db, a2.ID))
}

func TestRealDB_Balance_TransferMovesBothAccounts(t *testing.T) {
	db := testutil.NewRealDB(t)
	seedDefaultCategories(t, db)
	userId := seedUser(t, db)
	src := seedAccountNamed(t, db, userId, "Src")
	dst := seedAccountNamed(t, db, userId, "Dst")
	svc := NewService(db)

	res, err := svc.Transfer(CreateTransferDto{
		SourceAccountId: src.ID, DestinationAccountId: dst.ID, Amount: 250, Description: "move",
	}, userId)
	require.NoError(t, err)
	assert.Equal(t, 750.0, accountBalance(t, db, src.ID))
	assert.Equal(t, 1250.0, accountBalance(t, db, dst.ID))

	require.NoError(t, svc.Remove(res.DestinationTransaction.ID, userId))
	assert.Equal(t, 1000.0, accountBalance(t, db, src.ID))
	assert.Equal(t, 1000.0, accountBalance(t, db, dst.ID))
}

// Transactions from before balance tracking never moved the balance, so
// editing or deleting them must not move it either.
func TestRealDB_Balance_LegacyTransactionLeavesBalanceAlone(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := seedUser(t, db)
	acc := seedAccountNamed(t, db, userId, "Old")
	cat := seedCategory(t, db, userId, common.TransactionTypeExpense)
	svc := NewService(db)

	tx, err := svc.Create(CreateTransactionDto{
		Type: common.TransactionTypeExpense, Amount: -30, Description: "legacy", CategoryId: cat.ID, AccountId: acc.ID,
	}, userId)
	require.NoError(t, err)
	// Simulate a pre-migration row: no balance effect recorded.
	require.NoError(t, db.Exec(`UPDATE "transaction" SET "balanceApplied" = false WHERE id = ?`, tx.ID).Error)
	require.NoError(t, db.Exec(`UPDATE "account" SET balance = 1000 WHERE id = ?`, acc.ID).Error)

	amount := -50.0
	_, err = svc.Update(tx.ID, UpdateTransactionDto{Amount: &amount}, userId)
	require.NoError(t, err)
	require.NoError(t, svc.Remove(tx.ID, userId))
	assert.Equal(t, 1000.0, accountBalance(t, db, acc.ID))
}

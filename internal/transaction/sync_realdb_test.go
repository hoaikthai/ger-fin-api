package transaction

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"ger_fin_api/internal/common"
	"ger_fin_api/internal/testutil"
)

func balanceOf(t *testing.T, db *gorm.DB, accountId string) float64 {
	t.Helper()
	var b float64
	require.NoError(t, db.Raw(`SELECT "balance" FROM "account" WHERE "id" = ?`, accountId).Scan(&b).Error)
	return b
}

func appCode(t *testing.T, err error) int {
	t.Helper()
	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	return appErr.Code
}

func expenseDto(id *string, accountId, categoryId string) CreateTransactionDto {
	return CreateTransactionDto{
		CreateMeta: common.CreateMeta{ID: id},
		Type:       common.TransactionTypeExpense, Amount: -10, Description: "Lunch",
		CategoryId: categoryId, AccountId: accountId,
	}
}

func TestRealDB_Sync_CreateTwiceWithSameIdAppliesOnce(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userId := seedUser(t, db)
	acc := seedAccount(t, db, userId)
	cat := seedCategory(t, db, userId, common.TransactionTypeExpense)
	id := uuid.NewString()

	first, err := svc.Create(expenseDto(&id, acc.ID, cat.ID), userId)
	require.NoError(t, err)
	second, err := svc.Create(expenseDto(&id, acc.ID, cat.ID), userId)
	require.NoError(t, err)

	assert.Equal(t, id, first.ID)
	assert.Equal(t, id, second.ID)
	assert.InDelta(t, 990.0, balanceOf(t, db, acc.ID), 0.001, "balance moved once")
}

func TestRealDB_Sync_CreateWithForeignOrDeletedId(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	owner := seedUser(t, db)
	other := seedUser(t, db)
	acc := seedAccount(t, db, owner)
	cat := seedCategory(t, db, owner, common.TransactionTypeExpense)
	otherAcc := seedAccount(t, db, other)
	otherCat := seedCategory(t, db, other, common.TransactionTypeExpense)
	id := uuid.NewString()

	_, err := svc.Create(expenseDto(&id, acc.ID, cat.ID), owner)
	require.NoError(t, err)

	_, err = svc.Create(expenseDto(&id, otherAcc.ID, otherCat.ID), other)
	assert.Equal(t, 409, appCode(t, err))

	require.NoError(t, svc.Remove(id, owner))
	_, err = svc.Create(expenseDto(&id, acc.ID, cat.ID), owner)
	assert.Equal(t, 410, appCode(t, err))
}

func TestRealDB_Sync_UpdateLastWriteWins(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userId := seedUser(t, db)
	acc := seedAccount(t, db, userId)
	cat := seedCategory(t, db, userId, common.TransactionTypeExpense)
	tx, err := svc.Create(expenseDto(nil, acc.ID, cat.ID), userId)
	require.NoError(t, err)

	t2 := time.Now().Add(-time.Minute)
	newer := "newer"
	_, err = svc.Update(tx.ID, UpdateTransactionDto{UpdateMeta: common.UpdateMeta{ClientModifiedAt: &t2}, Description: &newer}, userId)
	require.NoError(t, err)

	t1 := t2.Add(-time.Minute)
	older := "older"
	got, err := svc.Update(tx.ID, UpdateTransactionDto{UpdateMeta: common.UpdateMeta{ClientModifiedAt: &t1}, Description: &older}, userId)
	require.NoError(t, err)
	assert.Equal(t, "newer", got.Description, "stale edit ignored, current row returned")
	require.NotNil(t, got.ClientModifiedAt)
	assert.WithinDuration(t, t2, *got.ClientModifiedAt, time.Millisecond)
}

func TestRealDB_Sync_Update_FutureClockIsClamped(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userId := seedUser(t, db)
	acc := seedAccount(t, db, userId)
	cat := seedCategory(t, db, userId, common.TransactionTypeExpense)
	tx, err := svc.Create(expenseDto(nil, acc.ID, cat.ID), userId)
	require.NoError(t, err)

	future := time.Now().Add(48 * time.Hour)
	ahead := "clock ahead"
	_, err = svc.Update(tx.ID, UpdateTransactionDto{UpdateMeta: common.UpdateMeta{ClientModifiedAt: &future}, Description: &ahead}, userId)
	require.NoError(t, err)

	now := time.Now()
	later := "later edit"
	got, err := svc.Update(tx.ID, UpdateTransactionDto{UpdateMeta: common.UpdateMeta{ClientModifiedAt: &now}, Description: &later}, userId)
	require.NoError(t, err)
	assert.Equal(t, "later edit", got.Description)
}

func TestRealDB_Sync_UpdateDeletedIsGoneAndDeleteIsIdempotent(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userId := seedUser(t, db)
	acc := seedAccount(t, db, userId)
	cat := seedCategory(t, db, userId, common.TransactionTypeExpense)
	tx, err := svc.Create(expenseDto(nil, acc.ID, cat.ID), userId)
	require.NoError(t, err)

	require.NoError(t, svc.Remove(tx.ID, userId))
	require.NoError(t, svc.Remove(tx.ID, userId), "second delete succeeds")
	assert.InDelta(t, 1000.0, balanceOf(t, db, acc.ID), 0.001, "balance reversed once")

	desc := "edit"
	_, err = svc.Update(tx.ID, UpdateTransactionDto{Description: &desc}, userId)
	assert.Equal(t, 410, appCode(t, err))

	_, err = svc.Update(uuid.NewString(), UpdateTransactionDto{Description: &desc}, userId)
	assert.Equal(t, 404, appCode(t, err), "unknown id stays 404")
}

func TestRealDB_Sync_TransferReplayAppliesOnce(t *testing.T) {
	db := testutil.NewRealDB(t)
	seedDefaultCategories(t, db)
	svc := NewService(db)
	userId := seedUser(t, db)
	src := seedAccountNamed(t, db, userId, "Src")
	dst := seedAccountNamed(t, db, userId, "Dst")
	id, relatedId := uuid.NewString(), uuid.NewString()
	dto := CreateTransferDto{
		ID: &id, RelatedID: &relatedId,
		SourceAccountId: src.ID, DestinationAccountId: dst.ID, Amount: 25, Description: "move",
	}

	first, err := svc.Transfer(dto, userId)
	require.NoError(t, err)
	second, err := svc.Transfer(dto, userId)
	require.NoError(t, err)

	assert.Equal(t, id, first.SourceTransaction.ID)
	assert.Equal(t, relatedId, first.DestinationTransaction.ID)
	assert.Equal(t, id, second.SourceTransaction.ID)
	assert.Equal(t, relatedId, second.DestinationTransaction.ID)
	assert.InDelta(t, 975.0, balanceOf(t, db, src.ID), 0.001)
	assert.InDelta(t, 1025.0, balanceOf(t, db, dst.ID), 0.001)
}

func TestRealDB_Sync_TransferIdsMustComeTogether(t *testing.T) {
	db := testutil.NewRealDB(t)
	seedDefaultCategories(t, db)
	svc := NewService(db)
	userId := seedUser(t, db)
	src := seedAccountNamed(t, db, userId, "Src")
	dst := seedAccountNamed(t, db, userId, "Dst")
	id := uuid.NewString()
	base := CreateTransferDto{SourceAccountId: src.ID, DestinationAccountId: dst.ID, Amount: 5, Description: "x"}

	onlyId := base
	onlyId.ID = &id
	_, err := svc.Transfer(onlyId, userId)
	assert.Equal(t, 400, appCode(t, err))

	same := base
	same.ID, same.RelatedID = &id, &id
	_, err = svc.Transfer(same, userId)
	assert.Equal(t, 400, appCode(t, err))
}

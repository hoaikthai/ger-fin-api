package common_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/common"
	"ger_fin_api/internal/testutil"
)

func code(t *testing.T, err error) int {
	t.Helper()
	appErr, ok := err.(*common.AppError)
	require.True(t, ok, "expected *AppError, got %v", err)
	return appErr.Code
}

func TestRealDB_CreateReplay(t *testing.T) {
	db := testutil.NewRealDB(t)
	owner := testutil.SeedUser(t, db)
	other := testutil.SeedUser(t, db)
	accId := testutil.SeedAccount(t, db, owner, "USD")

	replay, err := common.CreateReplay(db, "account", nil, owner)
	require.NoError(t, err)
	assert.False(t, replay, "no client id → normal create")

	fresh := uuid.NewString()
	replay, err = common.CreateReplay(db, "account", &fresh, owner)
	require.NoError(t, err)
	assert.False(t, replay, "unknown id → normal create")

	replay, err = common.CreateReplay(db, "account", &accId, owner)
	require.NoError(t, err)
	assert.True(t, replay, "own live row → return it")

	_, err = common.CreateReplay(db, "account", &accId, other)
	assert.Equal(t, 409, code(t, err), "someone else's id")

	require.NoError(t, db.Exec(`UPDATE "account" SET "deletedAt" = now() WHERE "id" = ?`, accId).Error)
	_, err = common.CreateReplay(db, "account", &accId, owner)
	assert.Equal(t, 410, code(t, err), "own deleted row")
}

func TestRealDB_GoneAndIgnoreIfDeleted(t *testing.T) {
	db := testutil.NewRealDB(t)
	owner := testutil.SeedUser(t, db)
	txId := uuid.NewString()
	accId := testutil.SeedAccount(t, db, owner, "USD")
	catId := testutil.SeedCategory(t, db, owner, "expense")
	require.NoError(t, db.Exec(`INSERT INTO "transaction" ("id","type","amount","description","categoryId","userId","accountId","deletedAt")
		VALUES (?, 'expense', -1, 'x', ?, ?, ?, now())`, txId, catId, owner, accId).Error)

	notFound := common.NewNotFound("Transaction not found")
	assert.Equal(t, 410, code(t, common.GoneIfDeleted(db, "transaction", txId, owner, notFound)))
	assert.NoError(t, common.IgnoreIfDeleted(db, "transaction", txId, owner, notFound))

	missing := uuid.NewString()
	assert.Equal(t, 404, code(t, common.GoneIfDeleted(db, "transaction", missing, owner, notFound)))
	assert.Equal(t, 404, code(t, common.IgnoreIfDeleted(db, "transaction", missing, owner, notFound)))

	forbidden := common.NewForbidden("nope")
	assert.Equal(t, 403, code(t, common.GoneIfDeleted(db, "transaction", txId, owner, forbidden)), "non-404 errors pass through")
}

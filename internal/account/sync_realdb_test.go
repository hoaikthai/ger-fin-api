package account

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/common"
	"ger_fin_api/internal/testutil"
)

func appCode(t *testing.T, err error) int {
	t.Helper()
	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	return appErr.Code
}

func TestRealDB_Sync_Account(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userId := testutil.SeedUser(t, db)
	id := uuid.NewString()
	dto := CreateAccountDto{CreateMeta: common.CreateMeta{ID: &id}, Name: "Cash", Currency: "VND"}

	first, err := svc.Create(userId, dto)
	require.NoError(t, err)
	second, err := svc.Create(userId, dto)
	require.NoError(t, err)
	assert.Equal(t, id, first.ID)
	assert.Equal(t, id, second.ID)

	t2 := time.Now().Add(-time.Minute)
	t1 := t2.Add(-time.Minute)
	newer, older := "Wallet", "Old name"
	_, err = svc.Update(id, userId, UpdateAccountDto{UpdateMeta: common.UpdateMeta{ClientModifiedAt: &t2}, Name: &newer})
	require.NoError(t, err)
	got, err := svc.Update(id, userId, UpdateAccountDto{UpdateMeta: common.UpdateMeta{ClientModifiedAt: &t1}, Name: &older})
	require.NoError(t, err)
	assert.Equal(t, "Wallet", got.Name)

	require.NoError(t, svc.Remove(id, userId))
	require.NoError(t, svc.Remove(id, userId))
	_, err = svc.Update(id, userId, UpdateAccountDto{Name: &newer})
	assert.Equal(t, 410, appCode(t, err))
	_, err = svc.Create(userId, dto)
	assert.Equal(t, 410, appCode(t, err))
}

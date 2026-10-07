package category

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

func TestRealDB_Sync_Category(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userId := testutil.SeedUser(t, db)
	id := uuid.NewString()
	dto := CreateCategoryDto{CreateMeta: common.CreateMeta{ID: &id}, Name: "Coffee", Type: common.TransactionTypeExpense}

	first, err := svc.Create(dto, userId)
	require.NoError(t, err)
	second, err := svc.Create(dto, userId)
	require.NoError(t, err)
	assert.Equal(t, id, first.ID)
	assert.Equal(t, id, second.ID)

	t2 := time.Now().Add(-time.Minute)
	t1 := t2.Add(-time.Minute)
	newer, older := "Cafe", "Old"
	_, err = svc.Update(id, UpdateCategoryDto{UpdateMeta: common.UpdateMeta{ClientModifiedAt: &t2}, Name: &newer}, userId)
	require.NoError(t, err)
	got, err := svc.Update(id, UpdateCategoryDto{UpdateMeta: common.UpdateMeta{ClientModifiedAt: &t1}, Name: &older}, userId)
	require.NoError(t, err)
	assert.Equal(t, "Cafe", got.Name)

	require.NoError(t, svc.Remove(id, userId))
	require.NoError(t, svc.Remove(id, userId))
	_, err = svc.Update(id, UpdateCategoryDto{Name: &newer}, userId)
	assert.Equal(t, 410, appCode(t, err))
}

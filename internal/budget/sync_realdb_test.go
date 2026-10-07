package budget

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

func TestRealDB_Sync_Budget(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := newSvc(db)
	userId := testutil.SeedUser(t, db)
	id := uuid.NewString()
	dto := CreateBudgetDto{CreateMeta: common.CreateMeta{ID: &id}, PeriodType: common.BudgetPeriodMonthly, Amount: 500}

	first, err := svc.Create(dto, userId)
	require.NoError(t, err)
	second, err := svc.Create(dto, userId)
	require.NoError(t, err, "replay is not a category/period conflict")
	assert.Equal(t, id, first.ID)
	assert.Equal(t, id, second.ID)

	t2 := time.Now().Add(-time.Minute)
	t1 := t2.Add(-time.Minute)
	newer, older := 700.0, 300.0
	_, err = svc.Update(id, UpdateBudgetDto{UpdateMeta: common.UpdateMeta{ClientModifiedAt: &t2}, Amount: &newer}, userId)
	require.NoError(t, err)
	got, err := svc.Update(id, UpdateBudgetDto{UpdateMeta: common.UpdateMeta{ClientModifiedAt: &t1}, Amount: &older}, userId)
	require.NoError(t, err)
	assert.InDelta(t, 700.0, got.Amount, 0.001)

	require.NoError(t, svc.Remove(id, userId))
	require.NoError(t, svc.Remove(id, userId))
	_, err = svc.Update(id, UpdateBudgetDto{Amount: &newer}, userId)
	assert.Equal(t, 410, appCode(t, err))
}

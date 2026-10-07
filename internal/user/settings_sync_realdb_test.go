package user

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/common"
	"ger_fin_api/internal/testutil"
)

func TestRealDB_Sync_SettingsLastWriteWins(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userId := testutil.SeedUser(t, db)

	t2 := time.Now().Add(-time.Minute)
	t1 := t2.Add(-time.Minute)
	eur, jpy := "EUR", "JPY"
	_, err := svc.UpdateSettings(userId, UpdateSettingsDto{UpdateMeta: common.UpdateMeta{ClientModifiedAt: &t2}, BaseCurrency: &eur})
	require.NoError(t, err)
	got, err := svc.UpdateSettings(userId, UpdateSettingsDto{UpdateMeta: common.UpdateMeta{ClientModifiedAt: &t1}, BaseCurrency: &jpy})
	require.NoError(t, err)
	assert.Equal(t, "EUR", got.BaseCurrency)
}

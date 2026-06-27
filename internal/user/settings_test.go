package user

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/testutil"
)

func TestRealDB_Settings_GetDefaults(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	email := "test-" + uuid.New().String() + "@example.com"
	u, err := svc.Create(newTestUser(email))
	require.NoError(t, err)

	settings, err := svc.GetSettings(u.ID)

	require.NoError(t, err)
	require.NotNil(t, settings)
	assert.Equal(t, u.ID, settings.UserId)
	assert.Equal(t, 3, settings.CashFlowMonths)
	assert.Equal(t, 2, settings.YearOverYearYears)
	assert.Equal(t, "USD", settings.BaseCurrency)
	assert.Nil(t, settings.DefaultAccountId)
}

func TestRealDB_Settings_GetIdempotent(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	email := "test-" + uuid.New().String() + "@example.com"
	u, err := svc.Create(newTestUser(email))
	require.NoError(t, err)

	s1, err := svc.GetSettings(u.ID)
	require.NoError(t, err)

	s2, err := svc.GetSettings(u.ID)
	require.NoError(t, err)

	assert.Equal(t, s1.ID, s2.ID)
}

func TestRealDB_Settings_Update(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	email := "test-" + uuid.New().String() + "@example.com"
	u, err := svc.Create(newTestUser(email))
	require.NoError(t, err)

	months := 6
	years := 3
	currency := "EUR"
	updated, err := svc.UpdateSettings(u.ID, UpdateSettingsDto{
		CashFlowMonths:    &months,
		YearOverYearYears: &years,
		BaseCurrency:      &currency,
	})

	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, 6, updated.CashFlowMonths)
	assert.Equal(t, 3, updated.YearOverYearYears)
	assert.Equal(t, "EUR", updated.BaseCurrency)
}

func TestRealDB_Settings_UpdatePartial(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	email := "test-" + uuid.New().String() + "@example.com"
	u, err := svc.Create(newTestUser(email))
	require.NoError(t, err)

	months := 6
	_, err = svc.UpdateSettings(u.ID, UpdateSettingsDto{CashFlowMonths: &months})
	require.NoError(t, err)

	settings, err := svc.GetSettings(u.ID)
	require.NoError(t, err)
	assert.Equal(t, 6, settings.CashFlowMonths)
	assert.Equal(t, 2, settings.YearOverYearYears) // unchanged
	assert.Equal(t, "USD", settings.BaseCurrency)   // unchanged
}

func TestRealDB_Settings_GetReportDefaults(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	email := "test-" + uuid.New().String() + "@example.com"
	u, err := svc.Create(newTestUser(email))
	require.NoError(t, err)

	cfm, yoy, err := svc.GetReportDefaults(u.ID)

	require.NoError(t, err)
	assert.Equal(t, 3, cfm)
	assert.Equal(t, 2, yoy)
}

func TestRealDB_Settings_ClearDefaultAccount(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	email := "test-" + uuid.New().String() + "@example.com"
	u, err := svc.Create(newTestUser(email))
	require.NoError(t, err)

	// Set empty string to clear
	empty := ""
	updated, err := svc.UpdateSettings(u.ID, UpdateSettingsDto{DefaultAccountId: &empty})
	require.NoError(t, err)
	assert.Nil(t, updated.DefaultAccountId)
}

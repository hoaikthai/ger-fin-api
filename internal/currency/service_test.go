package currency

import (
	"testing"

	"ger_fin_api/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRealDB_Currency_Convert_SameCurrency(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db, "")

	result, err := svc.Convert(100.0, "USD", "USD")
	require.NoError(t, err)
	assert.Equal(t, 100.0, result)
}

func TestRealDB_Currency_Convert_NoRateInDB(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db, "")

	// No rates seeded — must return ErrRateNotFound
	_, err := svc.Convert(100.0, "EUR", "USD")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRateNotFound)
}

func TestRealDB_Currency_Convert_WithSeededRate(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db, "")

	// Seed EUR rate: 1 USD = 0.92 EUR
	rate := ExchangeRate{
		BaseCurrency: "USD", TargetCurrency: "EUR", Rate: 0.92,
	}
	rate.FetchedAt = rate.CreatedAt
	require.NoError(t, db.Create(&rate).Error)
	// Manually set fetchedAt since it must be non-zero
	require.NoError(t, db.Exec(`UPDATE "exchangeRate" SET "fetchedAt" = NOW() WHERE id = ?`, rate.ID).Error)

	// 100 EUR → USD: 100 / 0.92 ≈ 108.70
	result, err := svc.Convert(100.0, "EUR", "USD")
	require.NoError(t, err)
	assert.InDelta(t, 108.70, result, 0.01)
}

func TestRealDB_Currency_GetLatestRates_Empty(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db, "")

	rates, err := svc.GetLatestRates()
	require.NoError(t, err)
	assert.Empty(t, rates)
}

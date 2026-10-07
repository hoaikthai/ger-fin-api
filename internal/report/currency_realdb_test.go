package report

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/account"
	"ger_fin_api/internal/common"
	"ger_fin_api/internal/testutil"
)

// eurToUsd converts EUR→USD at 2.0; USD passes through.
type eurToUsd struct{}

func (eurToUsd) Convert(amount float64, from, to string) (float64, error) {
	if from == to {
		return amount, nil
	}
	return amount * 2, nil
}

// Regression: TopCategories, CashFlow and YearOverYear summed amounts across
// account currencies without converting them.
func TestRealDB_Report_ConvertsMixedCurrencies(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db, eurToUsd{})
	userId := seedUser(t, db)
	usd := seedAccount(t, db, userId, 0)
	eur, err := account.NewService(db).Create(userId, account.CreateAccountDto{Name: "EUR", Currency: "EUR"})
	require.NoError(t, err)
	cat := seedCategory(t, db, userId, common.TransactionTypeExpense)

	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	seedTx(t, db, userId, cat.ID, usd.ID, common.TransactionTypeExpense, -10, now)
	seedTx(t, db, userId, cat.ID, eur.ID, common.TransactionTypeExpense, -10, now) // = -20 USD

	top, err := svc.TopCategories(userId, from, now, 5, "USD")
	require.NoError(t, err)
	require.Len(t, top, 1)
	assert.Equal(t, -30.0, top[0].Total)
	assert.Equal(t, 100.0, top[0].Percentage)

	cf, err := svc.CashFlow(userId, from, now, 3, "USD")
	require.NoError(t, err)
	assert.Equal(t, "USD", cf.BaseCurrency)
	assert.Equal(t, -30.0, cf.Actual.Expense)

	yoy, err := svc.YearOverYear(userId, from, now, 1, "USD")
	require.NoError(t, err)
	assert.Equal(t, "USD", yoy.BaseCurrency)
	require.Len(t, yoy.Years, 1)
	assert.Equal(t, -30.0, yoy.Years[0].Data[0].Expense)
}

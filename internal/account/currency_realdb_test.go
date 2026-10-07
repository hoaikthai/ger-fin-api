package account

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/common"
	"ger_fin_api/internal/testutil"
)

func TestRealDB_Account_Update_CurrencyLockedOnceUsed(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userId := seedUser(t, db)

	a, err := svc.Create(userId, CreateAccountDto{Name: "Wallet", Currency: "USD"})
	require.NoError(t, err)

	// No transactions yet: currency can change.
	eur := "EUR"
	_, err = svc.Update(a.ID, userId, UpdateAccountDto{Currency: &eur})
	require.NoError(t, err)

	var catId string
	require.NoError(t, db.Raw(`INSERT INTO "category" ("name", "type", "userId") VALUES ('C', 'expense', ?) RETURNING "id"`, userId).Scan(&catId).Error)
	require.NoError(t, db.Exec(`INSERT INTO "transaction" ("type", "amount", "description", "categoryId", "userId", "accountId") VALUES ('expense', -1, 'x', ?, ?, ?)`, catId, userId, a.ID).Error)

	usd := "USD"
	_, err = svc.Update(a.ID, userId, UpdateAccountDto{Currency: &usd})
	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, 400, appErr.Code)

	// Re-sending the current currency is still fine.
	_, err = svc.Update(a.ID, userId, UpdateAccountDto{Currency: &eur})
	require.NoError(t, err)
}

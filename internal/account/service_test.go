package account

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"ger_fin_api/internal/common"
	"ger_fin_api/internal/testutil"
	"ger_fin_api/internal/user"
)

func seedUser(t *testing.T, db *gorm.DB) string {
	t.Helper()
	svc := user.NewService(db)
	u := &user.User{Email: "test-" + uuid.New().String() + "@example.com", Password: "hashed"}
	created, err := svc.Create(u)
	require.NoError(t, err)
	return created.ID
}

func TestRealDB_Account_Create(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userID := seedUser(t, db)

	result, err := svc.Create(userID, CreateAccountDto{
		Name:     "Checking",
		Currency: "USD",
		Balance:  100.0,
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.ID)
	assert.Equal(t, "Checking", result.Name)
	assert.Equal(t, "USD", result.Currency)
	assert.Equal(t, userID, result.UserId)

	var fetched Account
	require.NoError(t, db.First(&fetched, "id = ?", result.ID).Error)
	assert.Equal(t, "Checking", fetched.Name)
	assert.Equal(t, "USD", fetched.Currency)
	assert.Equal(t, userID, fetched.UserId)
}

func TestRealDB_Account_FindAllByUser(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userA := seedUser(t, db)
	userB := seedUser(t, db)

	_, err := svc.Create(userA, CreateAccountDto{Name: "A1", Currency: "USD"})
	require.NoError(t, err)
	_, err = svc.Create(userA, CreateAccountDto{Name: "A2", Currency: "EUR"})
	require.NoError(t, err)
	_, err = svc.Create(userB, CreateAccountDto{Name: "B1", Currency: "GBP"})
	require.NoError(t, err)

	accountsA, err := svc.FindAllByUser(userA)
	require.NoError(t, err)
	assert.Len(t, accountsA, 2)

	accountsB, err := svc.FindAllByUser(userB)
	require.NoError(t, err)
	assert.Len(t, accountsB, 1)
}

func TestRealDB_Account_FindOne_Success(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userID := seedUser(t, db)

	created, err := svc.Create(userID, CreateAccountDto{Name: "Savings", Currency: "USD"})
	require.NoError(t, err)

	found, err := svc.FindOne(created.ID, userID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, found.ID)
	assert.Equal(t, "Savings", found.Name)
}

func TestRealDB_Account_FindOne_NotFound(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	_, err := svc.FindOne(uuid.New().String(), uuid.New().String())
	require.Error(t, err)
	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 404, appErr.Code)
}

func TestRealDB_Account_FindOne_WrongUser(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userID := seedUser(t, db)
	otherUser := seedUser(t, db)

	created, err := svc.Create(userID, CreateAccountDto{Name: "Private", Currency: "USD"})
	require.NoError(t, err)

	_, err = svc.FindOne(created.ID, otherUser)
	require.Error(t, err)
	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 404, appErr.Code)
}

func TestRealDB_Account_Update(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userID := seedUser(t, db)

	created, err := svc.Create(userID, CreateAccountDto{Name: "Old Name", Currency: "USD"})
	require.NoError(t, err)

	newName := "New Name"
	newCurrency := "EUR"
	updated, err := svc.Update(created.ID, userID, UpdateAccountDto{
		Name:     &newName,
		Currency: &newCurrency,
	})
	require.NoError(t, err)
	assert.Equal(t, "New Name", updated.Name)
	assert.Equal(t, "EUR", updated.Currency)

	var fetched Account
	require.NoError(t, db.First(&fetched, "id = ?", created.ID).Error)
	assert.Equal(t, "New Name", fetched.Name)
	assert.Equal(t, "EUR", fetched.Currency)
}

func TestRealDB_Account_Remove(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userID := seedUser(t, db)

	created, err := svc.Create(userID, CreateAccountDto{Name: "ToDelete", Currency: "USD"})
	require.NoError(t, err)

	require.NoError(t, svc.Remove(created.ID, userID))

	_, err = svc.FindOne(created.ID, userID)
	require.Error(t, err)
	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 404, appErr.Code)

	var softDeleted Account
	require.NoError(t, db.Unscoped().First(&softDeleted, "id = ?", created.ID).Error)
	assert.True(t, softDeleted.DeletedAt.Valid)
}

func TestRealDB_Account_UpdateBalance(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userID := seedUser(t, db)

	created, err := svc.Create(userID, CreateAccountDto{Name: "Wallet", Currency: "USD", Balance: 50.0})
	require.NoError(t, err)

	updated, err := svc.UpdateBalance(created.ID, userID, 250.0)
	require.NoError(t, err)
	assert.Equal(t, 250.0, updated.Balance)

	var fetched Account
	require.NoError(t, db.First(&fetched, "id = ?", created.ID).Error)
	assert.Equal(t, 250.0, fetched.Balance)
}

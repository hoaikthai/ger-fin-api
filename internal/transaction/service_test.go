package transaction

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"ger_fin_api/internal/account"
	"ger_fin_api/internal/category"
	"ger_fin_api/internal/common"
	"ger_fin_api/internal/testutil"
	"ger_fin_api/internal/user"
)

func seedUser(t *testing.T, db *gorm.DB) string {
	t.Helper()
	userSvc := user.NewService(db)
	u := &user.User{Email: "test-" + uuid.New().String() + "@example.com", Password: "hashed"}
	created, err := userSvc.Create(u)
	require.NoError(t, err)
	return created.ID
}

func seedAccount(t *testing.T, db *gorm.DB, userId string) *account.Account {
	t.Helper()
	svc := account.NewService(db)
	a, err := svc.Create(userId, account.CreateAccountDto{Name: "Test", Currency: "USD", Balance: 1000})
	require.NoError(t, err)
	return a
}

func seedAccountNamed(t *testing.T, db *gorm.DB, userId, name string) *account.Account {
	t.Helper()
	svc := account.NewService(db)
	a, err := svc.Create(userId, account.CreateAccountDto{Name: name, Currency: "USD", Balance: 1000})
	require.NoError(t, err)
	return a
}

func seedAccountWithCurrency(t *testing.T, db *gorm.DB, userId, currency string) *account.Account {
	t.Helper()
	svc := account.NewService(db)
	a, err := svc.Create(userId, account.CreateAccountDto{Name: "Account-" + currency, Currency: currency, Balance: 1000})
	require.NoError(t, err)
	return a
}

func seedCategory(t *testing.T, db *gorm.DB, userId string, typ common.TransactionType) *category.Category {
	t.Helper()
	svc := category.NewService(db)
	cat, err := svc.Create(category.CreateCategoryDto{Name: "Test Cat " + uuid.New().String(), Type: typ}, userId)
	require.NoError(t, err)
	return cat
}

func seedDefaultCategories(t *testing.T, db *gorm.DB) {
	t.Helper()
	svc := category.NewService(db)
	err := svc.SeedDefaultCategories()
	require.NoError(t, err)
}

func TestRealDB_Transaction_Create_Income(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	userId := seedUser(t, db)
	acc := seedAccount(t, db, userId)
	cat := seedCategory(t, db, userId, common.TransactionTypeIncome)

	result, err := svc.Create(CreateTransactionDto{
		Type:        common.TransactionTypeIncome,
		Amount:      100,
		Description: "Salary payment",
		CategoryId:  cat.ID,
		AccountId:   acc.ID,
	}, userId)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.ID)
	assert.Equal(t, 100.0, result.Amount)
	assert.Equal(t, string(common.TransactionTypeIncome), result.Type)

	var stored Transaction
	err = db.First(&stored, "id = ?", result.ID).Error
	require.NoError(t, err)
	assert.Equal(t, 100.0, stored.Amount)
	assert.Equal(t, string(common.TransactionTypeIncome), stored.Type)
}

func TestRealDB_Transaction_Create_Expense(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	userId := seedUser(t, db)
	acc := seedAccount(t, db, userId)
	cat := seedCategory(t, db, userId, common.TransactionTypeExpense)

	result, err := svc.Create(CreateTransactionDto{
		Type:        common.TransactionTypeExpense,
		Amount:      -50,
		Description: "Groceries",
		CategoryId:  cat.ID,
		AccountId:   acc.ID,
	}, userId)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.ID)
	assert.Equal(t, -50.0, result.Amount)
	assert.Equal(t, string(common.TransactionTypeExpense), result.Type)

	var stored Transaction
	err = db.First(&stored, "id = ?", result.ID).Error
	require.NoError(t, err)
	assert.Equal(t, -50.0, stored.Amount)
}

func TestRealDB_Transaction_Create_WrongAmountSign(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	userId := seedUser(t, db)
	acc := seedAccount(t, db, userId)
	incomeCat := seedCategory(t, db, userId, common.TransactionTypeIncome)
	expenseCat := seedCategory(t, db, userId, common.TransactionTypeExpense)

	_, err := svc.Create(CreateTransactionDto{
		Type:        common.TransactionTypeIncome,
		Amount:      -100,
		Description: "Bad income",
		CategoryId:  incomeCat.ID,
		AccountId:   acc.ID,
	}, userId)
	require.Error(t, err)
	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 400, appErr.Code)

	_, err = svc.Create(CreateTransactionDto{
		Type:        common.TransactionTypeExpense,
		Amount:      50,
		Description: "Bad expense",
		CategoryId:  expenseCat.ID,
		AccountId:   acc.ID,
	}, userId)
	require.Error(t, err)
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 400, appErr.Code)
}

func TestRealDB_Transaction_Create_CategoryTypeMismatch(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	userId := seedUser(t, db)
	acc := seedAccount(t, db, userId)
	expenseCat := seedCategory(t, db, userId, common.TransactionTypeExpense)

	_, err := svc.Create(CreateTransactionDto{
		Type:        common.TransactionTypeIncome,
		Amount:      100,
		Description: "Income with expense category",
		CategoryId:  expenseCat.ID,
		AccountId:   acc.ID,
	}, userId)

	require.Error(t, err)
	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 400, appErr.Code)
}

func TestRealDB_Transaction_FindAll(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	userA := seedUser(t, db)
	userB := seedUser(t, db)
	accA := seedAccount(t, db, userA)
	accB := seedAccount(t, db, userB)
	catA := seedCategory(t, db, userA, common.TransactionTypeIncome)
	catB := seedCategory(t, db, userB, common.TransactionTypeIncome)

	now := time.Now()
	for i := 0; i < 2; i++ {
		_, err := svc.Create(CreateTransactionDto{
			Type:            common.TransactionTypeIncome,
			Amount:          100,
			Description:     "Income",
			CategoryId:      catA.ID,
			AccountId:       accA.ID,
			TransactionDate: &now,
		}, userA)
		require.NoError(t, err)
	}

	_, err := svc.Create(CreateTransactionDto{
		Type:            common.TransactionTypeIncome,
		Amount:          200,
		Description:     "Other user income",
		CategoryId:      catB.ID,
		AccountId:       accB.ID,
		TransactionDate: &now,
	}, userB)
	require.NoError(t, err)

	results, err := svc.FindAll(userA, common.TimePeriodMonth, 0)
	require.NoError(t, err)
	assert.Len(t, results, 2)
	for _, tx := range results {
		assert.Equal(t, userA, tx.UserId)
	}
}

func TestRealDB_Transaction_FindOne_Success(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	userId := seedUser(t, db)
	acc := seedAccount(t, db, userId)
	cat := seedCategory(t, db, userId, common.TransactionTypeIncome)

	created, err := svc.Create(CreateTransactionDto{
		Type:        common.TransactionTypeIncome,
		Amount:      75,
		Description: "Test income",
		CategoryId:  cat.ID,
		AccountId:   acc.ID,
	}, userId)
	require.NoError(t, err)

	found, err := svc.FindOne(created.ID, userId)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, created.ID, found.ID)
	assert.Equal(t, 75.0, found.Amount)
}

func TestRealDB_Transaction_FindOne_NotFound(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	userId := seedUser(t, db)

	_, err := svc.FindOne(uuid.New().String(), userId)
	require.Error(t, err)
	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 404, appErr.Code)
}

func TestRealDB_Transaction_FindOne_WrongUser(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	userId := seedUser(t, db)
	otherUser := seedUser(t, db)
	acc := seedAccount(t, db, userId)
	cat := seedCategory(t, db, userId, common.TransactionTypeIncome)

	created, err := svc.Create(CreateTransactionDto{
		Type:        common.TransactionTypeIncome,
		Amount:      50,
		Description: "Private income",
		CategoryId:  cat.ID,
		AccountId:   acc.ID,
	}, userId)
	require.NoError(t, err)

	_, err = svc.FindOne(created.ID, otherUser)
	require.Error(t, err)
	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 404, appErr.Code)
}

func TestRealDB_Transaction_Update(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	userId := seedUser(t, db)
	acc := seedAccount(t, db, userId)
	cat := seedCategory(t, db, userId, common.TransactionTypeIncome)

	created, err := svc.Create(CreateTransactionDto{
		Type:        common.TransactionTypeIncome,
		Amount:      100,
		Description: "Original",
		CategoryId:  cat.ID,
		AccountId:   acc.ID,
	}, userId)
	require.NoError(t, err)

	newDesc := "Updated description"
	newAmount := 250.0
	updated, err := svc.Update(created.ID, UpdateTransactionDto{
		Description: &newDesc,
		Amount:      &newAmount,
	}, userId)
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, newDesc, updated.Description)
	assert.Equal(t, newAmount, updated.Amount)

	var stored Transaction
	err = db.First(&stored, "id = ?", created.ID).Error
	require.NoError(t, err)
	assert.Equal(t, newDesc, stored.Description)
	assert.Equal(t, newAmount, stored.Amount)
}

func TestRealDB_Transaction_Remove(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	userId := seedUser(t, db)
	acc := seedAccount(t, db, userId)
	cat := seedCategory(t, db, userId, common.TransactionTypeExpense)

	created, err := svc.Create(CreateTransactionDto{
		Type:        common.TransactionTypeExpense,
		Amount:      -30,
		Description: "To be deleted",
		CategoryId:  cat.ID,
		AccountId:   acc.ID,
	}, userId)
	require.NoError(t, err)

	err = svc.Remove(created.ID, userId)
	require.NoError(t, err)

	_, err = svc.FindOne(created.ID, userId)
	require.Error(t, err)
	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 404, appErr.Code)

	var softDeleted Transaction
	err = db.Unscoped().First(&softDeleted, "id = ?", created.ID).Error
	require.NoError(t, err)
	assert.True(t, softDeleted.DeletedAt.Valid)
}

func TestRealDB_Transaction_FindByAccount(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	seedDefaultCategories(t, db)

	userId := seedUser(t, db)
	accA := seedAccount(t, db, userId)
	accB := seedAccount(t, db, userId)
	cat := seedCategory(t, db, userId, common.TransactionTypeIncome)

	now := time.Now()
	for i := 0; i < 2; i++ {
		_, err := svc.Create(CreateTransactionDto{
			Type:            common.TransactionTypeIncome,
			Amount:          100,
			Description:     "Income for account A",
			CategoryId:      cat.ID,
			AccountId:       accA.ID,
			TransactionDate: &now,
		}, userId)
		require.NoError(t, err)
	}

	_, err := svc.Create(CreateTransactionDto{
		Type:            common.TransactionTypeIncome,
		Amount:          200,
		Description:     "Income for account B",
		CategoryId:      cat.ID,
		AccountId:       accB.ID,
		TransactionDate: &now,
	}, userId)
	require.NoError(t, err)

	results, err := svc.FindByAccount(accA.ID, userId, common.TimePeriodMonth, 0)
	require.NoError(t, err)
	assert.Len(t, results, 2)
	for _, tx := range results {
		assert.Equal(t, accA.ID, tx.AccountId)
	}
}

func TestRealDB_Transaction_Transfer(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	seedDefaultCategories(t, db)

	userId := seedUser(t, db)
	srcAcc := seedAccount(t, db, userId)
	dstAcc := seedAccount(t, db, userId)

	result, err := svc.Transfer(CreateTransferDto{
		SourceAccountId:      srcAcc.ID,
		DestinationAccountId: dstAcc.ID,
		Amount:               200,
		Description:          "Transfer between accounts",
	}, userId)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.SourceTransaction)
	require.NotNil(t, result.DestinationTransaction)

	assert.Less(t, result.SourceTransaction.Amount, 0.0)
	assert.Greater(t, result.DestinationTransaction.Amount, 0.0)
	assert.Equal(t, string(common.TransactionTypeExpense), result.SourceTransaction.Type)
	assert.Equal(t, string(common.TransactionTypeIncome), result.DestinationTransaction.Type)
}

func TestRealDB_Transaction_Transfer_SameAccount(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	userId := seedUser(t, db)
	acc := seedAccount(t, db, userId)

	_, err := svc.Transfer(CreateTransferDto{
		SourceAccountId:      acc.ID,
		DestinationAccountId: acc.ID,
		Amount:               100,
		Description:          "Same account transfer",
	}, userId)

	require.Error(t, err)
	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 400, appErr.Code)
}

func TestRealDB_Transaction_ImportFromCsv(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	seedDefaultCategories(t, db)

	userId := seedUser(t, db)
	svc2 := account.NewService(db)
	_, err := svc2.Create(userId, account.CreateAccountDto{Name: "Wallet", Currency: "USD", Balance: 500})
	require.NoError(t, err)

	csvData := []byte("Date,Category,Amount,Currency,Wallet,Note\n2024-01-15,Food & Beverage,-15.50,USD,Wallet,Lunch\n")

	importResult, err := svc.ImportFromCsv(csvData, userId, nil)
	require.NoError(t, err)
	require.NotNil(t, importResult)
	assert.Equal(t, 1, importResult.Imported)
	assert.Equal(t, 0, importResult.Skipped)
	assert.Empty(t, importResult.Errors)
	assert.Empty(t, importResult.UnknownCategories)

	var txs []Transaction
	err = db.Where("\"userId\" = ?", userId).Find(&txs).Error
	require.NoError(t, err)
	assert.Len(t, txs, 1)
}

func TestRealDB_Transaction_ImportFromCsv_UnknownCategory_NoMapping(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	userId := seedUser(t, db)
	svc2 := account.NewService(db)
	_, err := svc2.Create(userId, account.CreateAccountDto{Name: "Wallet", Currency: "USD", Balance: 500})
	require.NoError(t, err)

	csvData := []byte("Date,Category,Amount,Currency,Wallet\n2024-01-15,Unknown Cat,-10.00,USD,Wallet\n")

	result, err := svc.ImportFromCsv(csvData, userId, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 0, result.Imported)
	assert.Equal(t, []string{"Unknown Cat"}, result.UnknownCategories)

	var txs []Transaction
	db.Where("\"userId\" = ?", userId).Find(&txs)
	assert.Empty(t, txs)
}

func TestRealDB_Transaction_ImportFromCsv_UnknownCategory_WithMapping(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	userId := seedUser(t, db)
	svc2 := account.NewService(db)
	_, err := svc2.Create(userId, account.CreateAccountDto{Name: "Wallet", Currency: "USD", Balance: 500})
	require.NoError(t, err)

	cat := seedCategory(t, db, userId, common.TransactionTypeExpense)

	csvData := []byte("Date,Category,Amount,Currency,Wallet\n2024-01-15,My Old Category,-20.00,USD,Wallet\n")
	mapping := map[string]string{"My Old Category": cat.ID}

	result, err := svc.ImportFromCsv(csvData, userId, mapping)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, result.Imported)
	assert.Empty(t, result.UnknownCategories)

	var txs []Transaction
	db.Where("\"userId\" = ?", userId).Find(&txs)
	assert.Len(t, txs, 1)
	assert.Equal(t, cat.ID, txs[0].CategoryId)
}

func TestRealDB_Transaction_ImportFromCsv_Duplicate_SkipsAll(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	seedDefaultCategories(t, db)

	userId := seedUser(t, db)
	svc2 := account.NewService(db)
	_, err := svc2.Create(userId, account.CreateAccountDto{Name: "Wallet", Currency: "USD", Balance: 500})
	require.NoError(t, err)

	csvData := []byte("Date,Category,Amount,Currency,Wallet,Note\n2024-03-10,Food & Beverage,-15.00,USD,Wallet,Lunch\n")

	// First import
	r1, err := svc.ImportFromCsv(csvData, userId, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, r1.Imported)
	assert.Equal(t, 0, r1.Skipped)

	// Re-import identical CSV — should skip the duplicate
	r2, err := svc.ImportFromCsv(csvData, userId, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, r2.Imported)
	assert.Equal(t, 1, r2.Skipped)

	var txs []Transaction
	db.Where("\"userId\" = ?", userId).Find(&txs)
	assert.Len(t, txs, 1)
}

func TestRealDB_Transaction_ImportFromCsv_Duplicate_TwoIdenticalRows(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	seedDefaultCategories(t, db)

	userId := seedUser(t, db)
	svc2 := account.NewService(db)
	_, err := svc2.Create(userId, account.CreateAccountDto{Name: "Wallet", Currency: "USD", Balance: 500})
	require.NoError(t, err)

	// CSV has two identical rows — both are genuinely distinct transactions.
	csvTwoRows := []byte("Date,Category,Amount,Currency,Wallet,Note\n2024-03-10,Food & Beverage,-25.00,USD,Wallet,Coffee\n2024-03-10,Food & Beverage,-25.00,USD,Wallet,Coffee\n")

	r1, err := svc.ImportFromCsv(csvTwoRows, userId, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, r1.Imported)
	assert.Equal(t, 0, r1.Skipped)

	// Re-import: DB now has 2; CSV has 2 → all duplicates, skip both.
	r2, err := svc.ImportFromCsv(csvTwoRows, userId, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, r2.Imported)
	assert.Equal(t, 2, r2.Skipped)

	var txs []Transaction
	db.Where("\"userId\" = ?", userId).Find(&txs)
	assert.Len(t, txs, 2)
}

func TestRealDB_Transaction_ValidateCsv_AllGood(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	seedDefaultCategories(t, db)

	userId := seedUser(t, db)
	account.NewService(db).Create(userId, account.CreateAccountDto{Name: "Wallet", Currency: "USD", Balance: 500})

	csvData := []byte("Date,Category,Amount,Currency,Wallet\n2024-05-01,Food & Beverage,-10.00,USD,Wallet\n")
	result, err := svc.ValidateCsv(csvData, userId, nil)
	require.NoError(t, err)
	assert.True(t, result.Valid)
	assert.Empty(t, result.UnknownAccounts)
	assert.Empty(t, result.UnknownCategories)
	assert.Empty(t, result.RowErrors)
}

func TestRealDB_Transaction_ValidateCsv_CollectsAllIssues(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	userId := seedUser(t, db)
	// No accounts, no categories seeded — every name in the CSV will be unknown.

	csvData := []byte("Date,Category,Amount,Currency,Wallet\n2024-05-01,Groceries,-10.00,USD,Cash\n")
	result, err := svc.ValidateCsv(csvData, userId, nil)
	require.NoError(t, err)
	assert.False(t, result.Valid)
	assert.Equal(t, []string{"Cash"}, result.UnknownAccounts)
	assert.Equal(t, []string{"Groceries"}, result.UnknownCategories)
	// Row errors are skipped when account/category are unknown.
	assert.Empty(t, result.RowErrors)
}

func TestRealDB_Transaction_ValidateCsv_RowErrors(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	seedDefaultCategories(t, db)

	userId := seedUser(t, db)
	account.NewService(db).Create(userId, account.CreateAccountDto{Name: "Wallet", Currency: "USD", Balance: 500})

	// Income category but negative amount → row error.
	csvData := []byte("Date,Category,Amount,Currency,Wallet\n2024-05-01,Salary,-50.00,USD,Wallet\n")
	result, err := svc.ValidateCsv(csvData, userId, nil)
	require.NoError(t, err)
	assert.False(t, result.Valid)
	assert.Empty(t, result.UnknownAccounts)
	assert.Empty(t, result.UnknownCategories)
	assert.Len(t, result.RowErrors, 1)
}

func TestRealDB_Transaction_ValidateCsv_RowErrorCap(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	seedDefaultCategories(t, db)

	userId := seedUser(t, db)
	account.NewService(db).Create(userId, account.CreateAccountDto{Name: "Wallet", Currency: "USD", Balance: 500})

	// Build a CSV with maxValidateRowErrors+10 bad rows (income category with negative amount).
	var sb strings.Builder
	sb.WriteString("Date,Category,Amount,Currency,Wallet\n")
	for i := 0; i < maxValidateRowErrors+10; i++ {
		sb.WriteString("2024-05-01,Salary,-50.00,USD,Wallet\n")
	}
	result, err := svc.ValidateCsv([]byte(sb.String()), userId, nil)
	require.NoError(t, err)
	assert.False(t, result.Valid)
	assert.Len(t, result.RowErrors, maxValidateRowErrors)
}

func TestRealDB_Transaction_ValidateCsv_WithMapping(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	userId := seedUser(t, db)
	account.NewService(db).Create(userId, account.CreateAccountDto{Name: "Wallet", Currency: "USD", Balance: 500})
	cat := seedCategory(t, db, userId, common.TransactionTypeExpense)

	csvData := []byte("Date,Category,Amount,Currency,Wallet\n2024-05-01,Old Name,-20.00,USD,Wallet\n")
	result, err := svc.ValidateCsv(csvData, userId, map[string]string{"Old Name": cat.ID})
	require.NoError(t, err)
	assert.True(t, result.Valid)
	assert.Empty(t, result.UnknownCategories)
}

func TestRealDB_Transaction_Transfer_CrossCurrency_RequiresDestinationAmount(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	seedDefaultCategories(t, db)

	userId := seedUser(t, db)
	srcAcc := seedAccountWithCurrency(t, db, userId, "USD")
	dstAcc := seedAccountWithCurrency(t, db, userId, "EUR")

	_, err := svc.Transfer(CreateTransferDto{
		SourceAccountId:      srcAcc.ID,
		DestinationAccountId: dstAcc.ID,
		Amount:               100,
		Description:          "USD to EUR without destinationAmount",
	}, userId)

	require.Error(t, err)
	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 400, appErr.Code)
	assert.Contains(t, appErr.Error(), "destinationAmount")
}

func TestRealDB_Transaction_Transfer_CrossCurrency_WithDestinationAmount(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	seedDefaultCategories(t, db)

	userId := seedUser(t, db)
	srcAcc := seedAccountWithCurrency(t, db, userId, "USD")
	dstAcc := seedAccountWithCurrency(t, db, userId, "EUR")

	destAmount := 92.50
	result, err := svc.Transfer(CreateTransferDto{
		SourceAccountId:      srcAcc.ID,
		DestinationAccountId: dstAcc.ID,
		Amount:               100,
		DestinationAmount:    &destAmount,
		Description:          "USD to EUR",
	}, userId)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.SourceTransaction)
	require.NotNil(t, result.DestinationTransaction)

	assert.InDelta(t, -100.0, result.SourceTransaction.Amount, 0.001)
	assert.InDelta(t, 92.50, result.DestinationTransaction.Amount, 0.001)
	assert.Equal(t, string(common.TransactionTypeExpense), result.SourceTransaction.Type)
	assert.Equal(t, string(common.TransactionTypeIncome), result.DestinationTransaction.Type)
}

func TestRealDB_Transaction_Transfer_SameCurrency_DestinationAmountOptional(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	seedDefaultCategories(t, db)

	userId := seedUser(t, db)
	srcAcc := seedAccountWithCurrency(t, db, userId, "USD")
	dstAcc := seedAccountWithCurrency(t, db, userId, "USD")

	// Same currency: destinationAmount is optional — without it, mirrors Amount.
	result, err := svc.Transfer(CreateTransferDto{
		SourceAccountId:      srcAcc.ID,
		DestinationAccountId: dstAcc.ID,
		Amount:               200,
		Description:          "Same-currency transfer no destinationAmount",
	}, userId)

	require.NoError(t, err)
	assert.InDelta(t, -200.0, result.SourceTransaction.Amount, 0.001)
	assert.InDelta(t, 200.0, result.DestinationTransaction.Amount, 0.001)
}

func TestRealDB_Transaction_Transfer_SameCurrency_DestinationAmountOverride(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	seedDefaultCategories(t, db)

	userId := seedUser(t, db)
	srcAcc := seedAccountWithCurrency(t, db, userId, "USD")
	dstAcc := seedAccountWithCurrency(t, db, userId, "USD")

	// Same currency but caller explicitly provides destinationAmount — it is respected.
	destAmount := 150.0
	result, err := svc.Transfer(CreateTransferDto{
		SourceAccountId:      srcAcc.ID,
		DestinationAccountId: dstAcc.ID,
		Amount:               200,
		DestinationAmount:    &destAmount,
		Description:          "Same-currency transfer with explicit destinationAmount",
	}, userId)

	require.NoError(t, err)
	assert.InDelta(t, -200.0, result.SourceTransaction.Amount, 0.001)
	assert.InDelta(t, 150.0, result.DestinationTransaction.Amount, 0.001)
}

func TestRealDB_Transaction_ImportFromCsv_Duplicate_PartialOverlap(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)

	seedDefaultCategories(t, db)

	userId := seedUser(t, db)
	svc2 := account.NewService(db)
	_, err := svc2.Create(userId, account.CreateAccountDto{Name: "Wallet", Currency: "USD", Balance: 500})
	require.NoError(t, err)

	// First import: 1 transaction.
	csvOne := []byte("Date,Category,Amount,Currency,Wallet\n2024-04-05,Food & Beverage,-30.00,USD,Wallet\n")
	r1, err := svc.ImportFromCsv(csvOne, userId, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, r1.Imported)

	// Second import: 2 rows with same key — DB has 1, so only 1 more should be imported.
	csvTwo := []byte("Date,Category,Amount,Currency,Wallet\n2024-04-05,Food & Beverage,-30.00,USD,Wallet\n2024-04-05,Food & Beverage,-30.00,USD,Wallet\n")
	r2, err := svc.ImportFromCsv(csvTwo, userId, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, r2.Imported)
	assert.Equal(t, 1, r2.Skipped)

	var txs []Transaction
	db.Where("\"userId\" = ?", userId).Find(&txs)
	assert.Len(t, txs, 2)
}

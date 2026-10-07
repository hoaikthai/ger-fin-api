package transaction

import (
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/account"
	"ger_fin_api/internal/category"
	"ger_fin_api/internal/common"
	"ger_fin_api/internal/testutil"
)

const (
	testUserId   = "11111111-1111-1111-1111-111111111111"
	testTxId     = "22222222-2222-2222-2222-222222222222"
	testOldAccId = "33333333-3333-3333-3333-333333333333"
	testNewAccId = "44444444-4444-4444-4444-444444444444"
	testCatId    = "55555555-5555-5555-5555-555555555555"
)

func txCols() []string {
	return []string{"id", "createdAt", "updatedAt", "deletedAt", "type", "amount", "description",
		"categoryId", "userId", "accountId", "transactionDate", "relatedTransactionId",
		"recurringTransactionId", "originalCurrency", "originalAmount", "balanceApplied"}
}

// equalsArg matches a single driver argument against an expected value.
type equalsArg struct{ want string }

func (e equalsArg) Match(v driver.Value) bool { return v == e.want }

func expectFindOne(mock sqlmock.Sqlmock, accountId string) {
	now := time.Now()
	mock.ExpectQuery(`SELECT \* FROM "transaction"`).WillReturnRows(sqlmock.NewRows(txCols()).
		AddRow(testTxId, now, now, nil, "expense", -10.0, "Lunch", testCatId, testUserId, accountId, now, nil, nil, nil, nil, true))
	mock.ExpectQuery(`SELECT \* FROM "account"`).WillReturnRows(sqlmock.NewRows(testutil.AccountCols()).
		AddRow(accountId, now, now, nil, "Wallet", "USD", 100.0, nil, true, testUserId))
	mock.ExpectQuery(`SELECT \* FROM "category"`).WillReturnRows(sqlmock.NewRows(testutil.CategoryCols()).
		AddRow(testCatId, now, now, nil, "Food", "expense", true, nil, nil))
}

// Regression: Save() with a stale preloaded Account used to copy the old
// account's ID back onto accountId, silently ignoring the update.
func TestUpdate_ChangesAccountId(t *testing.T) {
	db, mock, err := testutil.NewMockDB()
	require.NoError(t, err)
	svc := NewService(db)

	expectFindOne(mock, testOldAccId)
	now := time.Now()
	mock.ExpectQuery(`SELECT \* FROM "account"`).WillReturnRows(sqlmock.NewRows(testutil.AccountCols()).
		AddRow(testNewAccId, now, now, nil, "Bank", "USD", 0.0, nil, true, testUserId))
	mock.ExpectBegin()
	args := make([]driver.Value, 16)
	for i := range args {
		args[i] = sqlmock.AnyArg()
	}
	args[8] = equalsArg{testNewAccId} // "accountId" in the SET list
	mock.ExpectExec(`UPDATE "transaction"`).WithArgs(args...).WillReturnResult(sqlmock.NewResult(0, 1))
	// Balance moves from the old account to the new one.
	mock.ExpectExec(`UPDATE "account" SET "balance"=balance \+ \$1`).WithArgs(10.0, sqlmock.AnyArg(), testOldAccId).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "account" SET "balance"=balance \+ \$1`).WithArgs(-10.0, sqlmock.AnyArg(), testNewAccId).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	expectFindOne(mock, testNewAccId)

	newAcc := testNewAccId
	tx, err := svc.Update(testTxId, UpdateTransactionDto{AccountId: &newAcc}, testUserId)
	require.NoError(t, err)
	assert.Equal(t, testNewAccId, tx.AccountId)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdate_RejectsTypeThatMismatchesCategory(t *testing.T) {
	db, mock, err := testutil.NewMockDB()
	require.NoError(t, err)
	svc := NewService(db)

	expectFindOne(mock, testOldAccId) // expense transaction in an expense category

	income := common.TransactionTypeIncome
	amount := 10.0
	_, err = svc.Update(testTxId, UpdateTransactionDto{Type: &income, Amount: &amount}, testUserId)
	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, 400, appErr.Code)
}

func TestValidateRows_RejectsInvalidDate(t *testing.T) {
	ctx := &csvContext{
		colIdx:    map[string]int{"Date": 0, "Category": 1, "Amount": 2, "Currency": 3, "Wallet": 4},
		rows:      [][]string{{"31/12/2025", "Food", "-5", "USD", "Cash"}, {"2025-12-31", "Food", "-5", "USD", "Cash"}},
		accByName: map[string]*account.Account{"Cash": {Currency: "USD"}},
		catByName: map[string]*category.Category{"Food": {Type: "expense"}},
	}
	errs := validateRows(ctx)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "Row 2: invalid date")
}

func newJobRouter(h *Handler, userId string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/transactions/import/jobs/:jobId", func(c *gin.Context) {
		c.Set("user", &common.JWTClaims{Sub: userId})
		c.Next()
	}, h.getImportJob)
	return r
}

func TestGetImportJob_OnlyOwnerCanRead(t *testing.T) {
	h := NewHandler(nil, nil, nil)
	h.jobs.Store("job-1", &importJobState{userId: "owner", status: "done", result: &ImportResult{Imported: 3}})

	w := httptest.NewRecorder()
	newJobRouter(h, "someone-else").ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/transactions/import/jobs/job-1", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)

	w = httptest.NewRecorder()
	newJobRouter(h, "owner").ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/transactions/import/jobs/job-1", nil))
	require.Equal(t, http.StatusOK, w.Code)
	var status ImportJobStatus
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &status))
	assert.Equal(t, 3, status.Result.Imported)
}

func TestEvictExpiredJobs(t *testing.T) {
	h := NewHandler(nil, nil, nil)
	h.jobs.Store("old", &importJobState{status: "done", finishedAt: time.Now().Add(-2 * importJobTTL)})
	h.jobs.Store("recent", &importJobState{status: "done", finishedAt: time.Now()})
	h.jobs.Store("running", &importJobState{status: "running"})

	h.evictExpiredJobs()

	_, old := h.jobs.Load("old")
	_, recent := h.jobs.Load("recent")
	_, running := h.jobs.Load("running")
	assert.False(t, old)
	assert.True(t, recent)
	assert.True(t, running)
}

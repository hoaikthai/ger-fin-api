package account

import (
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/common"
	"ger_fin_api/internal/testutil"
)

func newAccountSvc(t *testing.T) (*Service, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := testutil.NewMockDB()
	require.NoError(t, err)
	return NewService(db), mock
}

func TestAccountCreate_Success(t *testing.T) {
	svc, mock := newAccountSvc(t)

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "account"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("acc-uuid"))
	mock.ExpectCommit()

	a, err := svc.Create("user-id", CreateAccountDto{
		Name:     "Checking",
		Currency: "USD",
		Balance:  1000,
	})

	require.NoError(t, err)
	require.NotNil(t, a)
	assert.Equal(t, "Checking", a.Name)
	assert.Equal(t, "user-id", a.UserId)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountFindOne_NotFound(t *testing.T) {
	svc, mock := newAccountSvc(t)

	mock.ExpectQuery(`SELECT .+ FROM "account" WHERE`).
		WillReturnRows(sqlmock.NewRows(testutil.AccountCols()))

	_, err := svc.FindOne("nonexistent-id", "user-id")

	require.Error(t, err)
	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 404, appErr.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountFindOne_Success(t *testing.T) {
	svc, mock := newAccountSvc(t)

	rows := sqlmock.NewRows(testutil.AccountCols()).
		AddRow("acc-id", time.Now(), time.Now(), nil, "Checking", "USD", 1000.0, nil, true, "user-id")
	mock.ExpectQuery(`SELECT .+ FROM "account" WHERE`).WillReturnRows(rows)

	a, err := svc.FindOne("acc-id", "user-id")

	require.NoError(t, err)
	require.NotNil(t, a)
	assert.Equal(t, "Checking", a.Name)
	assert.Equal(t, "USD", a.Currency)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountRemove_NotFound(t *testing.T) {
	svc, mock := newAccountSvc(t)

	mock.ExpectQuery(`SELECT .+ FROM "account" WHERE`).
		WillReturnRows(sqlmock.NewRows(testutil.AccountCols()))

	err := svc.Remove("nonexistent-id", "user-id")

	require.Error(t, err)
	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 404, appErr.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountFindAll_Empty(t *testing.T) {
	svc, mock := newAccountSvc(t)

	mock.ExpectQuery(`SELECT .+ FROM "account" WHERE`).
		WillReturnRows(sqlmock.NewRows(testutil.AccountCols()))

	accounts, err := svc.FindAllByUser("user-id")

	require.NoError(t, err)
	assert.Empty(t, accounts)
	assert.NoError(t, mock.ExpectationsWereMet())
}

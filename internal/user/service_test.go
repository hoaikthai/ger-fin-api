package user

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

func newUserSvc(t *testing.T) (*Service, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := testutil.NewMockDB()
	require.NoError(t, err)
	return NewService(db), mock
}

func TestFindByEmail_Found(t *testing.T) {
	svc, mock := newUserSvc(t)

	rows := sqlmock.NewRows(testutil.UserCols()).
		AddRow("user-id", time.Now(), time.Now(), nil, "found@example.com", "hash", nil, nil)
	mock.ExpectQuery(`SELECT .+ FROM "user" WHERE`).WillReturnRows(rows)

	u, err := svc.FindByEmail("found@example.com")

	require.NoError(t, err)
	require.NotNil(t, u)
	assert.Equal(t, "found@example.com", u.Email)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestFindByEmail_NotFound(t *testing.T) {
	svc, mock := newUserSvc(t)

	mock.ExpectQuery(`SELECT .+ FROM "user" WHERE`).
		WillReturnRows(sqlmock.NewRows(testutil.UserCols()))

	u, err := svc.FindByEmail("nobody@example.com")

	require.NoError(t, err)
	assert.Nil(t, u)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetProfile_NotFound(t *testing.T) {
	svc, mock := newUserSvc(t)

	mock.ExpectQuery(`SELECT .+ FROM "user" WHERE`).
		WillReturnRows(sqlmock.NewRows(testutil.UserCols()))

	_, err := svc.GetProfile("nonexistent-id")

	require.Error(t, err)
	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 404, appErr.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateProfile_NotFound(t *testing.T) {
	svc, mock := newUserSvc(t)

	mock.ExpectQuery(`SELECT .+ FROM "user" WHERE`).
		WillReturnRows(sqlmock.NewRows(testutil.UserCols()))

	name := "Alice"
	_, err := svc.UpdateProfile("nonexistent-id", UpdateProfileDto{FirstName: &name})

	require.Error(t, err)
	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 404, appErr.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateProfile_Success(t *testing.T) {
	svc, mock := newUserSvc(t)

	rows := sqlmock.NewRows(testutil.UserCols()).
		AddRow("user-id", time.Now(), time.Now(), nil, "user@example.com", "hash", nil, nil)
	mock.ExpectQuery(`SELECT .+ FROM "user" WHERE`).WillReturnRows(rows)

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "user"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	first := "Alice"
	u, err := svc.UpdateProfile("user-id", UpdateProfileDto{FirstName: &first})

	require.NoError(t, err)
	require.NotNil(t, u)
	assert.Equal(t, &first, u.FirstName)
	assert.NoError(t, mock.ExpectationsWereMet())
}

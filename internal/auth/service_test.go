package auth

import (
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"ger_fin_api/internal/common"
	"ger_fin_api/internal/testutil"
	"ger_fin_api/internal/user"
)

func newAuthSvc(t *testing.T) (*Service, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := testutil.NewMockDB()
	require.NoError(t, err)
	return NewService(user.NewService(db), "test-secret"), mock
}

func TestRegister_Success(t *testing.T) {
	svc, mock := newAuthSvc(t)

	// FindByEmail: user not found
	mock.ExpectQuery(`SELECT .+ FROM "user" WHERE`).
		WillReturnRows(sqlmock.NewRows(testutil.UserCols()))

	// Create user
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "user"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("new-user-uuid"))
	mock.ExpectCommit()

	u, err := svc.Register(RegisterDto{
		Email:    "new@example.com",
		Password: "password123",
	})

	require.NoError(t, err)
	require.NotNil(t, u)
	assert.Equal(t, "new@example.com", u.Email)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRegister_EmailConflict(t *testing.T) {
	svc, mock := newAuthSvc(t)

	// FindByEmail: user already exists
	rows := sqlmock.NewRows(testutil.UserCols()).
		AddRow("existing-id", time.Now(), time.Now(), nil, "taken@example.com", "hash", nil, nil)
	mock.ExpectQuery(`SELECT .+ FROM "user" WHERE`).
		WillReturnRows(rows)

	_, err := svc.Register(RegisterDto{Email: "taken@example.com", Password: "password123"})

	require.Error(t, err)
	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 409, appErr.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestLogin_Success(t *testing.T) {
	svc, mock := newAuthSvc(t)

	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)

	rows := sqlmock.NewRows(testutil.UserCols()).
		AddRow("user-id-1", time.Now(), time.Now(), nil, "test@example.com", string(hash), nil, nil)
	mock.ExpectQuery(`SELECT .+ FROM "user" WHERE`).
		WillReturnRows(rows)

	token, err := svc.Login("test@example.com", "password123")

	require.NoError(t, err)
	assert.NotEmpty(t, token)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestLogin_WrongPassword(t *testing.T) {
	svc, mock := newAuthSvc(t)

	hash, _ := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)

	rows := sqlmock.NewRows(testutil.UserCols()).
		AddRow("user-id-1", time.Now(), time.Now(), nil, "test@example.com", string(hash), nil, nil)
	mock.ExpectQuery(`SELECT .+ FROM "user" WHERE`).
		WillReturnRows(rows)

	_, err := svc.Login("test@example.com", "wrong-password")

	require.Error(t, err)
	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 401, appErr.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestLogin_UserNotFound(t *testing.T) {
	svc, mock := newAuthSvc(t)

	mock.ExpectQuery(`SELECT .+ FROM "user" WHERE`).
		WillReturnRows(sqlmock.NewRows(testutil.UserCols()))

	_, err := svc.Login("nobody@example.com", "password123")

	require.Error(t, err)
	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 401, appErr.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

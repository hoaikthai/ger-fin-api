package auth

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/common"
	"ger_fin_api/internal/testutil"
	"ger_fin_api/internal/user"
)

func newRealAuthSvc(t *testing.T) (*Service, *user.Service) {
	t.Helper()
	db := testutil.NewRealDB(t)
	userSvc := user.NewService(db)
	authSvc := NewService(userSvc, "test-secret", 24*time.Hour)
	return authSvc, userSvc
}

func uniqueEmail(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("test-%d@example.com", time.Now().UnixNano())
}

func TestRealDB_Auth_Register_Success(t *testing.T) {
	authSvc, userSvc := newRealAuthSvc(t)
	email := uniqueEmail(t)
	plain := "password123"

	result, err := authSvc.Register(RegisterDto{Email: email, Password: plain})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.ID)
	assert.Equal(t, email, result.Email)

	stored, err := userSvc.FindByEmail(email)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.NotEqual(t, plain, stored.Password)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(stored.Password), []byte(plain)))
}

func TestRealDB_Auth_Register_DuplicateEmail(t *testing.T) {
	authSvc, _ := newRealAuthSvc(t)
	email := uniqueEmail(t)

	_, err := authSvc.Register(RegisterDto{Email: email, Password: "password123"})
	require.NoError(t, err)

	_, err = authSvc.Register(RegisterDto{Email: email, Password: "other-password"})
	require.Error(t, err)

	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 409, appErr.Code)
}

func TestRealDB_Auth_Login_Success(t *testing.T) {
	authSvc, _ := newRealAuthSvc(t)
	email := uniqueEmail(t)

	_, err := authSvc.Register(RegisterDto{Email: email, Password: "password123"})
	require.NoError(t, err)

	token, err := authSvc.Login(email, "password123")

	require.NoError(t, err)
	assert.NotEmpty(t, token)
}

func TestRealDB_Auth_Login_WrongPassword(t *testing.T) {
	authSvc, _ := newRealAuthSvc(t)
	email := uniqueEmail(t)

	_, err := authSvc.Register(RegisterDto{Email: email, Password: "correct-password"})
	require.NoError(t, err)

	_, err = authSvc.Login(email, "wrong-password")
	require.Error(t, err)

	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 401, appErr.Code)
}

func TestRealDB_Auth_Login_UnknownEmail(t *testing.T) {
	authSvc, _ := newRealAuthSvc(t)

	_, err := authSvc.Login(uniqueEmail(t), "password123")
	require.Error(t, err)

	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 401, appErr.Code)
}

func TestRealDB_Auth_ValidateUser_Success(t *testing.T) {
	authSvc, _ := newRealAuthSvc(t)
	email := uniqueEmail(t)

	_, err := authSvc.Register(RegisterDto{Email: email, Password: "password123"})
	require.NoError(t, err)

	u, err := authSvc.ValidateUser(email, "password123")

	require.NoError(t, err)
	require.NotNil(t, u)
	assert.Equal(t, email, u.Email)
}

func TestRealDB_Auth_ValidateUser_WrongCredentials(t *testing.T) {
	authSvc, _ := newRealAuthSvc(t)
	email := uniqueEmail(t)

	_, err := authSvc.Register(RegisterDto{Email: email, Password: "correct-password"})
	require.NoError(t, err)

	_, err = authSvc.ValidateUser(email, "wrong-password")
	require.Error(t, err)

	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 401, appErr.Code)
}

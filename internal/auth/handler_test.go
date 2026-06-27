package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"ger_fin_api/internal/auth"
	"ger_fin_api/internal/testutil"
	"ger_fin_api/internal/user"
)

func setupAuthRouter(t *testing.T) (*gin.Engine, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := testutil.NewMockDB()
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	r := gin.New()

	userSvc := user.NewService(db)
	authSvc := auth.NewService(userSvc, "test-secret")
	auth.NewHandler(authSvc).RegisterRoutes(r.Group(""))

	return r, mock
}

func jsonBody(t *testing.T, v any) *strings.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return strings.NewReader(string(b))
}

func TestRegisterEndpoint_Success(t *testing.T) {
	r, mock := setupAuthRouter(t)

	mock.ExpectQuery(`SELECT .+ FROM "user" WHERE`).
		WillReturnRows(sqlmock.NewRows(testutil.UserCols()))
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "user"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("new-user-uuid"))
	mock.ExpectCommit()

	req := httptest.NewRequest(http.MethodPost, "/auth/register", jsonBody(t, map[string]any{
		"email":    "new@example.com",
		"password": "password123",
	}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var body map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "new@example.com", body["email"])
	assert.Nil(t, body["password"], "password must not be in response")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRegisterEndpoint_ValidationError(t *testing.T) {
	r, _ := setupAuthRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/auth/register", jsonBody(t, map[string]any{
		"email":    "not-an-email",
		"password": "12", // too short
	}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var body map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, float64(400), body["statusCode"])
}

func TestRegisterEndpoint_EmailConflict(t *testing.T) {
	r, mock := setupAuthRouter(t)

	existingRows := sqlmock.NewRows(testutil.UserCols()).
		AddRow("existing-id", time.Now(), time.Now(), nil, "taken@example.com", "hash", nil, nil)
	mock.ExpectQuery(`SELECT .+ FROM "user" WHERE`).
		WillReturnRows(existingRows)

	req := httptest.NewRequest(http.MethodPost, "/auth/register", jsonBody(t, map[string]any{
		"email":    "taken@example.com",
		"password": "password123",
	}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestLoginEndpoint_Success(t *testing.T) {
	r, mock := setupAuthRouter(t)

	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	rows := sqlmock.NewRows(testutil.UserCols()).
		AddRow("user-id-1", time.Now(), time.Now(), nil, "test@example.com", string(hash), nil, nil)
	mock.ExpectQuery(`SELECT .+ FROM "user" WHERE`).
		WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodPost, "/auth/login", jsonBody(t, map[string]any{
		"email":    "test@example.com",
		"password": "password123",
	}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	token, ok := body["access_token"].(string)
	assert.True(t, ok)
	assert.NotEmpty(t, token)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestLoginEndpoint_InvalidCredentials(t *testing.T) {
	r, mock := setupAuthRouter(t)

	hash, _ := bcrypt.GenerateFromPassword([]byte("correct"), bcrypt.MinCost)
	rows := sqlmock.NewRows(testutil.UserCols()).
		AddRow("user-id-1", time.Now(), time.Now(), nil, "test@example.com", string(hash), nil, nil)
	mock.ExpectQuery(`SELECT .+ FROM "user" WHERE`).
		WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodPost, "/auth/login", jsonBody(t, map[string]any{
		"email":    "test@example.com",
		"password": "wrong",
	}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

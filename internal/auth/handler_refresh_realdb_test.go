package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/middleware"
	"ger_fin_api/internal/testutil"
	"ger_fin_api/internal/user"
)

const handlerTestSecret = "handler-test-secret-handler-test-secret"

func newAuthRouter(t *testing.T) *gin.Engine {
	t.Helper()
	db := testutil.NewRealDB(t)
	svc := NewService(user.NewService(db), handlerTestSecret, time.Hour)
	revoked := NewTokenStore(db)
	h := NewHandler(svc, revoked, NewRefreshStore(db, 90*24*time.Hour))
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h.RegisterRoutes(r.Group(""), middleware.JWTAuth(handlerTestSecret, revoked))
	return r
}

func send(t *testing.T, r *gin.Engine, path, bearer string, body any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestRealDB_AuthHandler_LoginRefreshLogout(t *testing.T) {
	r := newAuthRouter(t)
	email := uniqueEmail(t)
	code, _ := send(t, r, "/auth/register", "", map[string]string{"email": email, "password": "password123"})
	require.Equal(t, 201, code)

	code, login := send(t, r, "/auth/login", "", map[string]string{"email": email, "password": "password123"})
	require.Equal(t, 200, code)
	require.NotEmpty(t, login["access_token"])
	require.NotEmpty(t, login["refresh_token"])
	assert.Equal(t, float64(3600), login["expires_in"])

	code, refreshed := send(t, r, "/auth/refresh", "", map[string]any{"refresh_token": login["refresh_token"]})
	require.Equal(t, 200, code)
	require.NotEmpty(t, refreshed["access_token"])
	assert.NotEqual(t, login["refresh_token"], refreshed["refresh_token"])

	code, _ = send(t, r, "/auth/logout", refreshed["access_token"].(string), map[string]any{"refresh_token": refreshed["refresh_token"]})
	require.Equal(t, 200, code)

	code, _ = send(t, r, "/auth/refresh", "", map[string]any{"refresh_token": refreshed["refresh_token"]})
	assert.Equal(t, 401, code, "logout revoked the session")
}

func TestRealDB_AuthHandler_RefreshRejectsGarbage(t *testing.T) {
	r := newAuthRouter(t)
	code, _ := send(t, r, "/auth/refresh", "", map[string]any{"refresh_token": "nope"})
	assert.Equal(t, 401, code)
	code, _ = send(t, r, "/auth/refresh", "", map[string]any{})
	assert.Equal(t, 400, code)
}

func TestRealDB_AuthHandler_LogoutWithoutBodyStillWorks(t *testing.T) {
	r := newAuthRouter(t)
	email := uniqueEmail(t)
	send(t, r, "/auth/register", "", map[string]string{"email": email, "password": "password123"})
	_, login := send(t, r, "/auth/login", "", map[string]string{"email": email, "password": "password123"})

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+login["access_token"].(string))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)

	code, _ := send(t, r, "/auth/logout", login["access_token"].(string), map[string]any{})
	assert.Equal(t, 401, code, "access token is denylisted after logout")
}

func TestRealDB_AuthHandler_LogoutMalformedBodyRevokesNothing(t *testing.T) {
	r := newAuthRouter(t)
	email := uniqueEmail(t)
	send(t, r, "/auth/register", "", map[string]string{"email": email, "password": "password123"})
	_, login := send(t, r, "/auth/login", "", map[string]string{"email": email, "password": "password123"})
	access := login["access_token"].(string)

	for _, body := range []string{`{bad json`, `{"refresh_token": 123}`} {
		req := httptest.NewRequest(http.MethodPost, "/auth/logout", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+access)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, 400, w.Code, body)
	}

	code, _ := send(t, r, "/auth/refresh", "", map[string]any{"refresh_token": login["refresh_token"]})
	assert.Equal(t, 200, code, "refresh token untouched")

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+access)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code, "access token still accepted")
}

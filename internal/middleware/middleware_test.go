package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"

	"ger_fin_api/internal/common"
)

func TestRateLimit_BlocksAfterLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/login", RateLimit(2, time.Minute), func(c *gin.Context) { c.Status(http.StatusOK) })

	codes := make([]int, 3)
	for i := range codes {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/login", nil)
		req.RemoteAddr = "203.0.113.7:1234"
		r.ServeHTTP(w, req)
		codes[i] = w.Code
	}
	assert.Equal(t, []int{200, 200, 429}, codes)

	// A different client has its own budget.
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "203.0.113.8:1234"
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestValidateIDParams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("", ValidateIDParams())
	g.GET("/accounts/:id", func(c *gin.Context) { c.Status(http.StatusOK) })
	g.GET("/jobs/:jobId", func(c *gin.Context) { c.Status(http.StatusOK) })

	cases := map[string]int{
		"/accounts/6f1c2a52-3c1e-4d7e-9b1a-2b6a0f6f1b2c": http.StatusOK,
		"/accounts/not-a-uuid":                           http.StatusNotFound,
		"/jobs/123":                                      http.StatusNotFound,
	}
	for path, want := range cases {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		assert.Equal(t, want, w.Code, path)
	}
}

type fakeRevocations map[string]bool

func (f fakeRevocations) IsRevoked(jti string) (bool, error) { return f[jti], nil }

func signToken(t *testing.T, secret, jti string) string {
	t.Helper()
	claims := common.JWTClaims{Sub: "user-1", RegisteredClaims: jwt.RegisteredClaims{
		ID:        jti,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}}
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestJWTAuth_RevokedAndLegacyTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const secret = "0123456789abcdef0123456789abcdef"
	r := gin.New()
	r.GET("/me", JWTAuth(secret, fakeRevocations{"revoked-jti": true}), func(c *gin.Context) { c.Status(http.StatusOK) })

	cases := map[string]int{
		"good-jti":    http.StatusOK,
		"revoked-jti": http.StatusUnauthorized,
		"":            http.StatusUnauthorized, // pre-logout token without jti
	}
	for jti, want := range cases {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/me", nil)
		req.Header.Set("Authorization", "Bearer "+signToken(t, secret, jti))
		r.ServeHTTP(w, req)
		assert.Equal(t, want, w.Code, "jti=%q", jti)
	}
}

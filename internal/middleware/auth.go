package middleware

import (
	"errors"
	"strings"

	"ger_fin_api/internal/common"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const userKey = "user"

// RevocationChecker reports whether a token ID (jti) has been revoked by logout.
type RevocationChecker interface {
	IsRevoked(jti string) (bool, error)
}

func JWTAuth(secret string, revoked RevocationChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" || !strings.HasPrefix(header, "Bearer ") {
			common.RespondError(c, common.NewUnauthorized("Unauthorized"))
			c.Abort()
			return
		}

		tokenStr := strings.TrimPrefix(header, "Bearer ")

		claims := &common.JWTClaims{}
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, errors.New("unexpected signing method")
			}
			return []byte(secret), nil
		})

		// Tokens without a jti predate logout support and cannot be revoked.
		if err != nil || !token.Valid || claims.ID == "" {
			common.RespondError(c, common.NewUnauthorized("Unauthorized"))
			c.Abort()
			return
		}

		isRevoked, err := revoked.IsRevoked(claims.ID)
		if err != nil {
			common.RespondInternalError(c)
			c.Abort()
			return
		}
		if isRevoked {
			common.RespondError(c, common.NewUnauthorized("Unauthorized"))
			c.Abort()
			return
		}

		c.Set(userKey, claims)
		c.Next()
	}
}

func CurrentUser(c *gin.Context) *common.JWTClaims {
	return c.MustGet(userKey).(*common.JWTClaims)
}

package common

import "github.com/golang-jwt/jwt/v5"

type JWTClaims struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	jwt.RegisteredClaims
}

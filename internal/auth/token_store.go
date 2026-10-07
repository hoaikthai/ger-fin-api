package auth

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TokenStore is the logout denylist: revoked token IDs (jti) are kept until
// the token would have expired anyway.
type TokenStore struct {
	db *gorm.DB
}

func NewTokenStore(db *gorm.DB) *TokenStore {
	return &TokenStore{db: db}
}

// Revoke denylists jti until expiresAt and prunes entries that have expired.
func (s *TokenStore) Revoke(jti string, expiresAt time.Time) error {
	if err := s.db.Exec(`DELETE FROM "revoked_token" WHERE "expiresAt" < now()`).Error; err != nil {
		return err
	}
	return s.db.Table("revoked_token").
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(map[string]interface{}{"jti": jti, "expiresAt": expiresAt}).Error
}

// IsRevoked satisfies middleware.RevocationChecker.
func (s *TokenStore) IsRevoked(jti string) (bool, error) {
	var count int64
	err := s.db.Table("revoked_token").Where(`"jti" = ?`, jti).Count(&count).Error
	return count > 0, err
}

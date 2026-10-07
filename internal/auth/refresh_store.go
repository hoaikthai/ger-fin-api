package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"time"

	"ger_fin_api/internal/common"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// reuseGrace lets a client that lost the refresh response (flaky mobile
// network) retry with the token it still holds without being treated as a thief.
const reuseGrace = 60 * time.Second

var ErrInvalidRefreshToken = common.NewUnauthorized("Invalid refresh token")

// RefreshStore issues and rotates opaque refresh tokens. Each successful
// rotation extends the session by ttl (sliding expiry).
type RefreshStore struct {
	db  *gorm.DB
	ttl time.Duration
	now func() time.Time
}

func NewRefreshStore(db *gorm.DB, ttl time.Duration) *RefreshStore {
	return &RefreshStore{db: db, ttl: ttl, now: time.Now}
}

type refreshRow struct {
	ID        string     `gorm:"column:id"`
	UserId    string     `gorm:"column:userId"`
	FamilyId  string     `gorm:"column:familyId"`
	ExpiresAt time.Time  `gorm:"column:expiresAt"`
	UsedAt    *time.Time `gorm:"column:usedAt"`
	RevokedAt *time.Time `gorm:"column:revokedAt"`
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// Issue starts a new token family for a fresh login and prunes expired tokens.
func (s *RefreshStore) Issue(userId string) (string, error) {
	if err := s.db.Exec(`DELETE FROM "refresh_token" WHERE "expiresAt" < ?`, s.now()).Error; err != nil {
		return "", err
	}
	return s.insert(s.db, userId, uuid.NewString())
}

func (s *RefreshStore) insert(db *gorm.DB, userId, familyId string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	err := db.Exec(`INSERT INTO "refresh_token" ("userId", "familyId", "tokenHash", "expiresAt") VALUES (?, ?, ?, ?)`,
		userId, familyId, hashToken(token), s.now().Add(s.ttl)).Error
	if err != nil {
		return "", err
	}
	return token, nil
}

// Rotate exchanges a refresh token for its successor in the same family and
// returns the owner's id. Presenting a token that was already used (outside
// reuseGrace) revokes the whole family: one of the two holders is a thief.
func (s *RefreshStore) Rotate(token string) (string, string, error) {
	var userId, next string
	reused := false
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var familyIds []string
		if err := tx.Raw(`SELECT "familyId" FROM "refresh_token" WHERE "tokenHash" = ?`, hashToken(token)).Scan(&familyIds).Error; err != nil {
			return err
		}
		if len(familyIds) == 0 {
			return ErrInvalidRefreshToken
		}
		if err := lockFamily(tx, familyIds[0]); err != nil {
			return err
		}
		var rows []refreshRow
		if err := tx.Raw(`SELECT "id", "userId", "familyId", "expiresAt", "usedAt", "revokedAt"
			FROM "refresh_token" WHERE "tokenHash" = ? FOR UPDATE`, hashToken(token)).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return ErrInvalidRefreshToken
		}
		row, now := rows[0], s.now()
		if row.RevokedAt != nil || !row.ExpiresAt.After(now) {
			return ErrInvalidRefreshToken
		}
		if row.UsedAt != nil && now.Sub(*row.UsedAt) > reuseGrace {
			reused = true
			return tx.Exec(`UPDATE "refresh_token" SET "revokedAt" = ? WHERE "familyId" = ? AND "revokedAt" IS NULL`,
				now, row.FamilyId).Error
		}
		if row.UsedAt == nil {
			if err := tx.Exec(`UPDATE "refresh_token" SET "usedAt" = ? WHERE "id" = ?`, now, row.ID).Error; err != nil {
				return err
			}
		}
		issued, err := s.insert(tx, row.UserId, row.FamilyId)
		if err != nil {
			return err
		}
		userId, next = row.UserId, issued
		return nil
	})
	if err != nil {
		return "", "", err
	}
	if reused {
		// The revocation above must commit, so the error is returned only after it.
		return "", "", ErrInvalidRefreshToken
	}
	return userId, next, nil
}

// RevokeFamilyOf ends the session that token belongs to (logout). Tokens of
// other users are left alone.
func (s *RefreshStore) RevokeFamilyOf(token, userId string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var familyIds []string
		if err := tx.Raw(`SELECT "familyId" FROM "refresh_token" WHERE "tokenHash" = ? AND "userId" = ?`,
			hashToken(token), userId).Scan(&familyIds).Error; err != nil {
			return err
		}
		if len(familyIds) == 0 {
			return nil
		}
		if err := lockFamily(tx, familyIds[0]); err != nil {
			return err
		}
		return tx.Exec(`UPDATE "refresh_token" SET "revokedAt" = ? WHERE "familyId" = ? AND "revokedAt" IS NULL`,
			s.now(), familyIds[0]).Error
	})
}

// lockFamily serializes rotation and revocation of one token family, so a
// revocation always sees successors created by a concurrent rotation. It is
// always taken before any row lock, giving a single lock order.
func lockFamily(tx *gorm.DB, familyId string) error {
	return tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, familyId).Error
}

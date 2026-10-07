package testutil

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// SeedUser inserts a user with a unique email and returns its id.
func SeedUser(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var id string
	err := db.Raw(`INSERT INTO "user" ("email", "password") VALUES (?, 'hashed') RETURNING "id"`,
		"test-"+uuid.NewString()+"@example.com").Scan(&id).Error
	require.NoError(t, err)
	return id
}

// SeedAccount inserts an account with balance 1000 and returns its id.
func SeedAccount(t *testing.T, db *gorm.DB, userId, currency string) string {
	t.Helper()
	var id string
	err := db.Raw(`INSERT INTO "account" ("name", "currency", "balance", "userId") VALUES ('Test', ?, 1000, ?) RETURNING "id"`,
		currency, userId).Scan(&id).Error
	require.NoError(t, err)
	return id
}

// SeedCategory inserts a user category of type "income" or "expense" and returns its id.
func SeedCategory(t *testing.T, db *gorm.DB, userId, typ string) string {
	t.Helper()
	var id string
	err := db.Raw(`INSERT INTO "category" ("name", "type", "userId") VALUES (?, ?, ?) RETURNING "id"`,
		"Cat "+uuid.NewString(), typ, userId).Scan(&id).Error
	require.NoError(t, err)
	return id
}

// Seq returns the sync sequence number of a row, including soft-deleted rows.
func Seq(t *testing.T, db *gorm.DB, table, id string) int64 {
	t.Helper()
	var seq int64
	err := db.Raw(fmt.Sprintf(`SELECT "seq" FROM %q WHERE "id" = ?`, table), id).Scan(&seq).Error
	require.NoError(t, err)
	return seq
}

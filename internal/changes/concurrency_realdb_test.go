package changes

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/testutil"
)

// Writer A allocates a seq and keeps its transaction open; writer B (same user)
// must wait for A, so a pull taken in between cannot advance past A's row.
func TestRealDB_Changes_NeverSkipsALateCommit(t *testing.T) {
	db := testutil.NewRealDBShared(t)
	userId := testutil.SeedUser(t, db) // committed
	t.Cleanup(func() { db.Exec(`DELETE FROM "user" WHERE "id" = ?`, userId) })
	svc := NewService(db)

	start, err := svc.Changes(userId, 0, MaxLimit)
	require.NoError(t, err)

	txA := db.Begin()
	var accA string
	require.NoError(t, txA.Raw(`INSERT INTO "account" ("name","currency","userId") VALUES ('A','USD',?) RETURNING "id"`, userId).Scan(&accA).Error)

	bDone := make(chan string, 1)
	go func() {
		var accB string
		_ = db.Raw(`INSERT INTO "account" ("name","currency","userId") VALUES ('B','USD',?) RETURNING "id"`, userId).Scan(&accB).Error
		bDone <- accB
	}()

	select {
	case <-bDone:
		txA.Rollback()
		t.Fatal("writer B committed while writer A held the user's lock")
	case <-time.After(300 * time.Millisecond):
	}

	mid, err := svc.Changes(userId, start.Cursor, MaxLimit)
	require.NoError(t, err)
	assert.Empty(t, accountIds(mid), "neither uncommitted A nor blocked B is visible")

	require.NoError(t, txA.Commit().Error)
	accB := <-bDone
	require.NotEmpty(t, accB)

	after, err := svc.Changes(userId, mid.Cursor, MaxLimit)
	require.NoError(t, err)
	assert.Contains(t, accountIds(after), accA)
	assert.Contains(t, accountIds(after), accB)
}

package migrations_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/testutil"
)

// A row-level trigger that takes a lock runs after Postgres has locked the
// target row, so a transaction holding that lock and a statement holding the
// row could wait on each other. The statement-level lock is taken before any
// row is locked, so the second writer simply waits.
func TestRealDB_SyncLock_NoDeadlockBetweenRowAndUserWrites(t *testing.T) {
	db := testutil.NewRealDBShared(t)
	userId := testutil.SeedUser(t, db) // committed
	t.Cleanup(func() { db.Exec(`DELETE FROM "user" WHERE "id" = ?`, userId) })
	accX := testutil.SeedAccount(t, db, userId, "USD") // committed

	txA := db.Begin()
	t.Cleanup(func() { txA.Rollback() }) // registered last, so it runs before the user delete
	require.NoError(t, txA.Exec(`INSERT INTO "account" ("name","currency","userId") VALUES ('A','USD',?)`, userId).Error)

	bDone := make(chan error, 1)
	go func() {
		bDone <- db.Exec(`UPDATE "account" SET "name" = 'B' WHERE "id" = ?`, accX).Error
	}()
	time.Sleep(200 * time.Millisecond)

	require.NoError(t, txA.Exec(`UPDATE "account" SET "balance" = "balance" + 1 WHERE "id" = ?`, accX).Error)
	require.NoError(t, txA.Commit().Error)

	select {
	case err := <-bDone:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("writer B did not finish")
	}
}

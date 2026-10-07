package migrations_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/migrations"
	"ger_fin_api/internal/testutil"
)

var syncedTables = []string{"account", "category", "transaction", "budget", "recurring_transaction", "user_settings"}

func TestRealDB_SyncSeq_ColumnsExistAndAreFilled(t *testing.T) {
	db := testutil.NewRealDB(t)
	for _, table := range syncedTables {
		var nullable string
		require.NoError(t, db.Raw(`SELECT is_nullable FROM information_schema.columns
			WHERE table_name = ? AND column_name = 'seq'`, table).Scan(&nullable).Error)
		assert.Equal(t, "NO", nullable, table)

		var cma int64
		require.NoError(t, db.Raw(`SELECT COUNT(*) FROM information_schema.columns
			WHERE table_name = ? AND column_name = 'clientModifiedAt'`, table).Scan(&cma).Error)
		assert.Equal(t, int64(1), cma, table)
	}
}

func TestRealDB_SyncSeq_BumpsOnInsertUpdateAndSoftDelete(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := testutil.SeedUser(t, db)
	accId := testutil.SeedAccount(t, db, userId, "USD")

	inserted := testutil.Seq(t, db, "account", accId)
	require.Positive(t, inserted)

	require.NoError(t, db.Exec(`UPDATE "account" SET "name" = 'Renamed' WHERE "id" = ?`, accId).Error)
	updated := testutil.Seq(t, db, "account", accId)
	assert.Greater(t, updated, inserted)

	require.NoError(t, db.Exec(`UPDATE "account" SET "deletedAt" = now() WHERE "id" = ?`, accId).Error)
	assert.Greater(t, testutil.Seq(t, db, "account", accId), updated)
}

func TestRealDB_SyncSeq_DefaultCategoryWithoutUserGetsSeq(t *testing.T) {
	db := testutil.NewRealDB(t)
	var id string
	require.NoError(t, db.Raw(`INSERT INTO "category" ("name", "type", "isDefault") VALUES ('Default X', 'expense', true) RETURNING "id"`).Scan(&id).Error)
	assert.Positive(t, testutil.Seq(t, db, "category", id))
}

// The harness migrates an empty database, so this rebuilds the pre-012 shape of
// a real table inside the test transaction (no seq column, populated) and runs
// 012's per-table SQL against it, exercising the backfill UPDATE.
func TestRealDB_SyncSeq_BackfillsExistingRows(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := testutil.SeedUser(t, db)
	require.NoError(t, db.Exec(`
		DROP TRIGGER "TRG_account_sync_seq" ON "account";
		ALTER TABLE "account" DROP COLUMN "seq";
	`).Error)
	ids := []string{
		testutil.SeedAccount(t, db, userId, "USD"),
		testutil.SeedAccount(t, db, userId, "EUR"),
		testutil.SeedAccount(t, db, userId, "VND"),
	}

	require.NoError(t, db.Exec(migrations.SyncSeqTableSQL("account")).Error)

	seen := map[int64]bool{}
	for _, id := range ids {
		var seq *int64
		require.NoError(t, db.Raw(`SELECT "seq" FROM "account" WHERE "id" = ?`, id).Scan(&seq).Error)
		require.NotNil(t, seq, "backfilled")
		assert.False(t, seen[*seq], "distinct seq")
		seen[*seq] = true
	}
	var nulls int64
	require.NoError(t, db.Raw(`SELECT COUNT(*) FROM "account" WHERE "seq" IS NULL`).Scan(&nulls).Error)
	assert.Zero(t, nulls)

	newId := testutil.SeedAccount(t, db, userId, "USD")
	assert.Positive(t, testutil.Seq(t, db, "account", newId), "trigger reinstalled")
}

package migrations

import (
	"fmt"

	"gorm.io/gorm"
)

// syncedTables are the tables mobile clients mirror through GET /sync/changes.
var syncedTables = []string{"account", "category", "transaction", "budget", "recurring_transaction", "user_settings"}

// Every write to a synced table takes a new value from one global sequence, so a
// client can pull "everything after seq N". The trigger first takes a per-user
// advisory lock: one user's writes are serialized, their seq values become
// visible in commit order, and a pull can never skip a row that commits late.
// Default categories (userId NULL) only change in migrations/seeding and take no lock.
// Superseded by 014: the per-row lock deadlocked; a global statement-level lock replaces it.
func migration012() Migration {
	return Migration{
		Version: 12,
		Name:    "SyncSeqAndClientModifiedAt",
		Up: func(db *gorm.DB) error {
			return db.Transaction(func(tx *gorm.DB) error {
				if err := tx.Exec(`
					CREATE SEQUENCE IF NOT EXISTS sync_seq;
					CREATE OR REPLACE FUNCTION sync_bump_seq() RETURNS trigger AS $$
					BEGIN
						IF NEW."userId" IS NOT NULL THEN
							PERFORM pg_advisory_xact_lock(hashtext(NEW."userId"::text));
						END IF;
						NEW."seq" := nextval('sync_seq');
						RETURN NEW;
					END
					$$ LANGUAGE plpgsql;
				`).Error; err != nil {
					return err
				}
				for _, table := range syncedTables {
					if err := tx.Exec(syncSeqTableSQL(table)).Error; err != nil {
						return fmt.Errorf("%s: %w", table, err)
					}
				}
				return nil
			})
		},
	}
}

// syncSeqTableSQL adds the sync columns to one table, backfills seq for
// existing rows and installs the seq trigger.
func syncSeqTableSQL(table string) string {
	return fmt.Sprintf(`
		ALTER TABLE "%[1]s" ADD COLUMN IF NOT EXISTS "seq" bigint;
		ALTER TABLE "%[1]s" ADD COLUMN IF NOT EXISTS "clientModifiedAt" timestamptz;
		UPDATE "%[1]s" SET "seq" = nextval('sync_seq') WHERE "seq" IS NULL;
		ALTER TABLE "%[1]s" ALTER COLUMN "seq" SET NOT NULL;
		CREATE INDEX IF NOT EXISTS "IDX_%[1]s_user_seq" ON "%[1]s" ("userId", "seq");
		DROP TRIGGER IF EXISTS "TRG_%[1]s_sync_seq" ON "%[1]s";
		CREATE TRIGGER "TRG_%[1]s_sync_seq" BEFORE INSERT OR UPDATE ON "%[1]s"
			FOR EACH ROW EXECUTE FUNCTION sync_bump_seq();
	`, table)
}

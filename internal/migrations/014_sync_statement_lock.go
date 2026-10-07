package migrations

import (
	"fmt"

	"gorm.io/gorm"
)

// Replaces 012's per-user row-level lock, which deadlocked: Postgres locks the
// target row before row triggers run, so a statement holding row X could wait
// in the trigger for the user lock while the lock holder waited for row X.
//
// Now every INSERT or UPDATE statement on a synced table first takes one global
// advisory lock in a statement-level BEFORE trigger, which fires before any row
// is scanned or locked. A waiting writer holds no row lock, so it cannot be
// part of a deadlock, and seq values become visible in commit order across all
// users: a pull can never skip a row that commits late. The row trigger now
// only assigns seq.
func migration014() Migration {
	return Migration{
		Version: 14,
		Name:    "SyncStatementLock",
		Up: func(db *gorm.DB) error {
			return db.Transaction(func(tx *gorm.DB) error {
				if err := tx.Exec(`
					CREATE OR REPLACE FUNCTION sync_statement_lock() RETURNS trigger AS $$
					BEGIN
						PERFORM pg_advisory_xact_lock(1, 0);
						RETURN NULL;
					END
					$$ LANGUAGE plpgsql;
					CREATE OR REPLACE FUNCTION sync_bump_seq() RETURNS trigger AS $$
					BEGIN
						NEW."seq" := nextval('sync_seq');
						RETURN NEW;
					END
					$$ LANGUAGE plpgsql;
				`).Error; err != nil {
					return err
				}
				for _, table := range syncedTables {
					if err := tx.Exec(fmt.Sprintf(`
						DROP TRIGGER IF EXISTS "TRG_%[1]s_sync_lock" ON "%[1]s";
						CREATE TRIGGER "TRG_%[1]s_sync_lock" BEFORE INSERT OR UPDATE ON "%[1]s"
							FOR EACH STATEMENT EXECUTE FUNCTION sync_statement_lock();
					`, table)).Error; err != nil {
						return fmt.Errorf("%s: %w", table, err)
					}
				}
				return nil
			})
		},
	}
}

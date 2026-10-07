package migrations

import "gorm.io/gorm"

// Balances are now maintained from transaction amounts (numeric(15,2)), so the
// balance column needs the same precision to avoid overflow.
//
// Transactions created before balance tracking never moved the balance, so
// they are marked balanceApplied = false and later updates/deletes of them
// leave the balance alone (reversing an effect that never happened would
// corrupt it).
func migration009() Migration {
	return Migration{
		Version: 9,
		Name:    "AccountBalanceTracking",
		Up: func(db *gorm.DB) error {
			return db.Exec(`
				ALTER TABLE "account"
					ALTER COLUMN "balance" TYPE numeric(15, 2);

				ALTER TABLE "transaction"
					ADD COLUMN IF NOT EXISTS "balanceApplied" boolean NOT NULL DEFAULT true;

				UPDATE "transaction" SET "balanceApplied" = false;
			`).Error
		},
	}
}

package migrations

import "gorm.io/gorm"

func migration007() Migration {
	return Migration{
		Version: 7,
		Name:    "AddTransactionCurrencyFields",
		Up: func(db *gorm.DB) error {
			return db.Exec(`
				ALTER TABLE "transaction"
					ADD COLUMN IF NOT EXISTS "originalCurrency" VARCHAR(3),
					ADD COLUMN IF NOT EXISTS "originalAmount"   NUMERIC(15,2);
			`).Error
		},
	}
}

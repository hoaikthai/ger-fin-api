package migrations

import "gorm.io/gorm"

func migration004() Migration {
	return Migration{
		Version: 4,
		Name:    "IncreaseAmountPrecision",
		Up: func(db *gorm.DB) error {
			return db.Exec(`
				ALTER TABLE "recurring_transaction"
					ALTER COLUMN "amount" TYPE numeric(15, 2);

				ALTER TABLE "transaction"
					ALTER COLUMN "amount" TYPE numeric(15, 2);
			`).Error
		},
	}
}

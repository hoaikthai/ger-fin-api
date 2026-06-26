package migrations

import "gorm.io/gorm"

func migration002() Migration {
	return Migration{
		Version: 2,
		Name:    "AddSoftDeleteColumns",
		Up: func(db *gorm.DB) error {
			return db.Exec(`
				ALTER TABLE "account"     ADD COLUMN IF NOT EXISTS "deletedAt" timestamp;
				ALTER TABLE "user"        ADD COLUMN IF NOT EXISTS "deletedAt" timestamp;
				ALTER TABLE "category"    ADD COLUMN IF NOT EXISTS "deletedAt" timestamp;
				ALTER TABLE "transaction" ADD COLUMN IF NOT EXISTS "deletedAt" timestamp;
			`).Error
		},
	}
}

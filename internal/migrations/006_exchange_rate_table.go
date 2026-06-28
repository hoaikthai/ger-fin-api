package migrations

import "gorm.io/gorm"

func migration006() Migration {
	return Migration{
		Version: 6,
		Name:    "CreateExchangeRateTable",
		Up: func(db *gorm.DB) error {
			return db.Exec(`
				CREATE TABLE IF NOT EXISTS "exchangeRate" (
					"id"             UUID        NOT NULL DEFAULT uuid_generate_v4(),
					"baseCurrency"   VARCHAR(3)  NOT NULL,
					"targetCurrency" VARCHAR(3)  NOT NULL,
					"rate"           NUMERIC(20,8) NOT NULL,
					"fetchedAt"      TIMESTAMPTZ NOT NULL,
					"createdAt"      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
					"updatedAt"      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
					"deletedAt"      TIMESTAMPTZ,
					CONSTRAINT "PK_exchangeRate" PRIMARY KEY ("id")
				);
				CREATE UNIQUE INDEX IF NOT EXISTS "UQ_exchangeRate_pair"
					ON "exchangeRate" ("baseCurrency", "targetCurrency")
					WHERE "deletedAt" IS NULL;
			`).Error
		},
	}
}

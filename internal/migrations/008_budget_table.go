package migrations

import "gorm.io/gorm"

func migration008() Migration {
	return Migration{
		Version: 8,
		Name:    "CreateBudgetTable",
		Up: func(db *gorm.DB) error {
			return db.Exec(`
				CREATE TABLE IF NOT EXISTS "budget" (
					"id"          uuid        NOT NULL DEFAULT uuid_generate_v4(),
					"createdAt"   timestamptz NOT NULL DEFAULT now(),
					"updatedAt"   timestamptz NOT NULL DEFAULT now(),
					"deletedAt"   timestamptz,
					"seriesId"    uuid        NOT NULL,
					"categoryId"  uuid,
					"userId"      uuid        NOT NULL,
					"periodType"  varchar     NOT NULL,
					"periodStart" date        NOT NULL,
					"periodEnd"   date        NOT NULL,
					"amount"      numeric(15,2) NOT NULL,
					"currency"    varchar(3)  NOT NULL,
					"carriedOver" numeric(15,2) NOT NULL DEFAULT 0,
					"rollover"    boolean     NOT NULL DEFAULT false,
					"isActive"    boolean     NOT NULL DEFAULT true,
					CONSTRAINT "PK_budget" PRIMARY KEY ("id"),
					CONSTRAINT "FK_budget_category"
						FOREIGN KEY ("categoryId") REFERENCES "category"("id") ON DELETE CASCADE,
					CONSTRAINT "FK_budget_user"
						FOREIGN KEY ("userId") REFERENCES "user"("id") ON DELETE CASCADE
				);

				CREATE INDEX IF NOT EXISTS "IDX_budget_series"
					ON "budget" ("seriesId") WHERE "deletedAt" IS NULL;

				CREATE UNIQUE INDEX IF NOT EXISTS "UQ_budget_category_period"
					ON "budget" ("userId", "categoryId", "periodType", "periodStart")
					WHERE "deletedAt" IS NULL AND "categoryId" IS NOT NULL;

				CREATE UNIQUE INDEX IF NOT EXISTS "UQ_budget_overall_period"
					ON "budget" ("userId", "periodType", "periodStart")
					WHERE "deletedAt" IS NULL AND "categoryId" IS NULL;
			`).Error
		},
	}
}

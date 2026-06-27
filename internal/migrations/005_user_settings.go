package migrations

import "gorm.io/gorm"

func migration005() Migration {
	return Migration{
		Version: 5,
		Name:    "CreateUserSettings",
		Up: func(db *gorm.DB) error {
			return db.Exec(`
				CREATE TABLE IF NOT EXISTS "user_settings" (
					"id"                uuid        NOT NULL DEFAULT uuid_generate_v4(),
					"userId"            uuid        NOT NULL,
					"cashFlowMonths"    integer     NOT NULL DEFAULT 3,
					"yearOverYearYears" integer     NOT NULL DEFAULT 2,
					"baseCurrency"      varchar(3)  NOT NULL DEFAULT 'USD',
					"defaultAccountId"  uuid,
					"createdAt"         timestamp   NOT NULL DEFAULT now(),
					"updatedAt"         timestamp   NOT NULL DEFAULT now(),
					"deletedAt"         timestamp,
					CONSTRAINT "PK_user_settings"        PRIMARY KEY ("id"),
					CONSTRAINT "UQ_user_settings_userId" UNIQUE ("userId"),
					CONSTRAINT "FK_user_settings_user"
						FOREIGN KEY ("userId")           REFERENCES "user"("id")    ON DELETE CASCADE,
					CONSTRAINT "FK_user_settings_account"
						FOREIGN KEY ("defaultAccountId") REFERENCES "account"("id") ON DELETE SET NULL
				);
			`).Error
		},
	}
}

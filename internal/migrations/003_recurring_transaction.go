package migrations

import "gorm.io/gorm"

func migration003() Migration {
	return Migration{
		Version: 3,
		Name:    "CreateRecurringTransactionTable",
		Up: func(db *gorm.DB) error {
			return db.Exec(`
				CREATE TYPE IF NOT EXISTS "public"."recurring_transaction_type_enum"
					AS ENUM('income', 'expense');

				CREATE TYPE IF NOT EXISTS "public"."recurring_transaction_frequency_enum"
					AS ENUM('daily', 'weekly', 'monthly', 'yearly');

				CREATE TABLE IF NOT EXISTS "recurring_transaction" (
					"id"          uuid                                          NOT NULL DEFAULT uuid_generate_v4(),
					"createdAt"   timestamp                                     NOT NULL DEFAULT now(),
					"updatedAt"   timestamp                                     NOT NULL DEFAULT now(),
					"deletedAt"   timestamp,
					"type"        "public"."recurring_transaction_type_enum"    NOT NULL,
					"amount"      numeric(10, 2)                                NOT NULL,
					"description" varchar                                       NOT NULL,
					"categoryId"  uuid                                          NOT NULL,
					"userId"      uuid                                          NOT NULL,
					"accountId"   uuid                                          NOT NULL,
					"frequency"   "public"."recurring_transaction_frequency_enum" NOT NULL,
					"startDate"   timestamp                                     NOT NULL,
					"endDate"     timestamp,
					"nextDueDate" timestamp,
					"isActive"    boolean                                       NOT NULL DEFAULT true,
					CONSTRAINT "PK_recurring_transaction" PRIMARY KEY ("id"),
					CONSTRAINT "FK_recurring_transaction_category"
						FOREIGN KEY ("categoryId") REFERENCES "category"("id") ON DELETE CASCADE,
					CONSTRAINT "FK_recurring_transaction_user"
						FOREIGN KEY ("userId") REFERENCES "user"("id") ON DELETE CASCADE,
					CONSTRAINT "FK_recurring_transaction_account"
						FOREIGN KEY ("accountId") REFERENCES "account"("id") ON DELETE CASCADE
				);

				ALTER TABLE "transaction"
					ADD COLUMN IF NOT EXISTS "recurringTransactionId" uuid;

				ALTER TABLE "transaction"
					ADD CONSTRAINT "FK_transaction_recurring"
					FOREIGN KEY ("recurringTransactionId")
					REFERENCES "recurring_transaction"("id") ON DELETE CASCADE;
			`).Error
		},
	}
}

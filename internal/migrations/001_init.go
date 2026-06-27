package migrations

import "gorm.io/gorm"

func migration001() Migration {
	return Migration{
		Version: 1,
		Name:    "InitDB",
		Up: func(db *gorm.DB) error {
			return db.Exec(`
				CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

				CREATE TABLE IF NOT EXISTS "user" (
					"id"        uuid         NOT NULL DEFAULT uuid_generate_v4(),
					"email"     varchar      NOT NULL,
					"password"  varchar      NOT NULL,
					"firstName" varchar,
					"lastName"  varchar,
					"createdAt" timestamp    NOT NULL DEFAULT now(),
					"updatedAt" timestamp    NOT NULL DEFAULT now(),
					CONSTRAINT "UQ_user_email" UNIQUE ("email"),
					CONSTRAINT "PK_user"       PRIMARY KEY ("id")
				);

				CREATE TABLE IF NOT EXISTS "account" (
					"id"          uuid            NOT NULL DEFAULT uuid_generate_v4(),
					"name"        varchar         NOT NULL,
					"currency"    varchar(3)      NOT NULL,
					"balance"     numeric(10, 2)  NOT NULL DEFAULT 0,
					"description" varchar,
					"isActive"    boolean         NOT NULL DEFAULT true,
					"userId"      uuid            NOT NULL,
					"createdAt"   timestamp       NOT NULL DEFAULT now(),
					"updatedAt"   timestamp       NOT NULL DEFAULT now(),
					CONSTRAINT "PK_account" PRIMARY KEY ("id"),
					CONSTRAINT "FK_account_user"
						FOREIGN KEY ("userId") REFERENCES "user"("id") ON DELETE CASCADE
				);

				CREATE TABLE IF NOT EXISTS "category" (
					"id"        uuid      NOT NULL DEFAULT uuid_generate_v4(),
					"name"      varchar   NOT NULL,
					"type"      varchar   NOT NULL,
					"isDefault" boolean   NOT NULL DEFAULT false,
					"userId"    uuid,
					"parentId"  uuid,
					"createdAt" timestamp NOT NULL DEFAULT now(),
					"updatedAt" timestamp NOT NULL DEFAULT now(),
					CONSTRAINT "PK_category" PRIMARY KEY ("id"),
					CONSTRAINT "FK_category_user"
						FOREIGN KEY ("userId") REFERENCES "user"("id") ON DELETE CASCADE,
					CONSTRAINT "FK_category_parent"
						FOREIGN KEY ("parentId") REFERENCES "category"("id") ON DELETE CASCADE
				);

				CREATE TABLE IF NOT EXISTS "transaction" (
					"id"                   uuid      NOT NULL DEFAULT uuid_generate_v4(),
					"type"                 varchar   NOT NULL,
					"amount"               numeric(10, 2) NOT NULL,
					"description"          varchar   NOT NULL,
					"categoryId"           uuid      NOT NULL,
					"userId"               uuid      NOT NULL,
					"accountId"            uuid      NOT NULL,
					"transactionDate"      timestamp NOT NULL DEFAULT now(),
					"relatedTransactionId" uuid,
					"createdAt"            timestamp NOT NULL DEFAULT now(),
					"updatedAt"            timestamp NOT NULL DEFAULT now(),
					CONSTRAINT "PK_transaction" PRIMARY KEY ("id"),
					CONSTRAINT "FK_transaction_category"
						FOREIGN KEY ("categoryId") REFERENCES "category"("id") ON DELETE CASCADE,
					CONSTRAINT "FK_transaction_user"
						FOREIGN KEY ("userId") REFERENCES "user"("id") ON DELETE CASCADE,
					CONSTRAINT "FK_transaction_account"
						FOREIGN KEY ("accountId") REFERENCES "account"("id") ON DELETE CASCADE,
					CONSTRAINT "FK_transaction_related"
						FOREIGN KEY ("relatedTransactionId") REFERENCES "transaction"("id") ON DELETE CASCADE
				);
			`).Error
		},
	}
}

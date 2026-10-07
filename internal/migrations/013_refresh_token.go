package migrations

import "gorm.io/gorm"

// refresh_token stores rotating refresh tokens for mobile clients. Only a
// SHA-256 hash of each opaque token is stored. Tokens issued from one login
// share a familyId so reuse of an old token can revoke the whole chain.
func migration013() Migration {
	return Migration{
		Version: 13,
		Name:    "CreateRefreshTokenTable",
		Up: func(db *gorm.DB) error {
			return db.Exec(`
				CREATE TABLE IF NOT EXISTS "refresh_token" (
					"id"        uuid        NOT NULL DEFAULT uuid_generate_v4(),
					"userId"    uuid        NOT NULL,
					"familyId"  uuid        NOT NULL,
					"tokenHash" bytea       NOT NULL,
					"expiresAt" timestamptz NOT NULL,
					"usedAt"    timestamptz,
					"revokedAt" timestamptz,
					"createdAt" timestamptz NOT NULL DEFAULT now(),
					CONSTRAINT "PK_refresh_token"      PRIMARY KEY ("id"),
					CONSTRAINT "UQ_refresh_token_hash" UNIQUE ("tokenHash"),
					CONSTRAINT "FK_refresh_token_user"
						FOREIGN KEY ("userId") REFERENCES "user"("id") ON DELETE CASCADE
				);
				CREATE INDEX IF NOT EXISTS "IDX_refresh_token_family"    ON "refresh_token" ("familyId");
				CREATE INDEX IF NOT EXISTS "IDX_refresh_token_expiresAt" ON "refresh_token" ("expiresAt");
			`).Error
		},
	}
}

package migrations

import "gorm.io/gorm"

// revoked_token is the logout denylist: a token's jti is stored until the
// token would have expired anyway.
func migration011() Migration {
	return Migration{
		Version: 11,
		Name:    "CreateRevokedTokenTable",
		Up: func(db *gorm.DB) error {
			return db.Exec(`
				CREATE TABLE IF NOT EXISTS "revoked_token" (
					"jti"       uuid        NOT NULL,
					"expiresAt" timestamptz NOT NULL,
					CONSTRAINT "PK_revoked_token" PRIMARY KEY ("jti")
				);

				CREATE INDEX IF NOT EXISTS "IDX_revoked_token_expiresAt"
					ON "revoked_token" ("expiresAt");
			`).Error
		},
	}
}

package migrations

import "gorm.io/gorm"

// Transfers created before transfers were linked have relatedTransactionId NULL
// on both sides, so reports and budgets counted them as income/expense. Pair
// each unlinked "Outgoing transfer" with the "Incoming transfer" created right
// after it (same user and description, different account, created within 5s).
func migration010() Migration {
	return Migration{
		Version: 10,
		Name:    "LinkLegacyTransfers",
		Up: func(db *gorm.DB) error {
			return db.Exec(`
				WITH legs AS (
					SELECT t."id", t."userId", t."accountId", t."description", t."createdAt", c."name" AS kind,
					       ROW_NUMBER() OVER (
					           PARTITION BY t."userId", t."description", c."name", date_trunc('day', t."createdAt")
					           ORDER BY t."createdAt", t."id"
					       ) AS rn
					FROM "transaction" t
					JOIN "category" c ON c."id" = t."categoryId"
					WHERE c."isDefault" = true
					  AND c."name" IN ('Outgoing transfer', 'Incoming transfer')
					  AND t."relatedTransactionId" IS NULL
					  AND t."deletedAt" IS NULL
				),
				pairs AS (
					SELECT o."id" AS out_id, i."id" AS in_id
					FROM legs o
					JOIN legs i
					  ON i.kind = 'Incoming transfer'
					 AND i."userId" = o."userId"
					 AND i."description" = o."description"
					 AND date_trunc('day', i."createdAt") = date_trunc('day', o."createdAt")
					 AND i.rn = o.rn
					 AND i."accountId" <> o."accountId"
					 AND ABS(EXTRACT(EPOCH FROM (i."createdAt" - o."createdAt"))) < 5
					WHERE o.kind = 'Outgoing transfer'
				)
				UPDATE "transaction" t
				SET "relatedTransactionId" = CASE WHEN t."id" = p.out_id THEN p.in_id ELSE p.out_id END
				FROM pairs p
				WHERE t."id" IN (p.out_id, p.in_id);
			`).Error
		},
	}
}

package migrations_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/category"
	"ger_fin_api/internal/migrations"
	"ger_fin_api/internal/testutil"
)

func TestRealDB_LinkLegacyTransfers(t *testing.T) {
	db := testutil.NewRealDB(t)
	require.NoError(t, category.NewService(db).SeedDefaultCategories())

	var userId, src, dst, outCat, inCat, otherCat string
	require.NoError(t, db.Raw(`INSERT INTO "user" ("email", "password") VALUES (gen_random_uuid()::text || '@x.com', 'h') RETURNING "id"`).Scan(&userId).Error)
	require.NoError(t, db.Raw(`INSERT INTO "account" ("name", "currency", "userId") VALUES ('S', 'USD', ?) RETURNING "id"`, userId).Scan(&src).Error)
	require.NoError(t, db.Raw(`INSERT INTO "account" ("name", "currency", "userId") VALUES ('D', 'USD', ?) RETURNING "id"`, userId).Scan(&dst).Error)
	require.NoError(t, db.Raw(`SELECT "id" FROM "category" WHERE "isDefault" AND "name" = 'Outgoing transfer'`).Scan(&outCat).Error)
	require.NoError(t, db.Raw(`SELECT "id" FROM "category" WHERE "isDefault" AND "name" = 'Incoming transfer'`).Scan(&inCat).Error)
	require.NoError(t, db.Raw(`SELECT "id" FROM "category" WHERE "isDefault" AND "name" = 'Salary'`).Scan(&otherCat).Error)

	insert := func(cat, acc string, amount float64, createdAt time.Time) string {
		var id string
		require.NoError(t, db.Raw(`INSERT INTO "transaction" ("type", "amount", "description", "categoryId", "userId", "accountId", "createdAt")
			VALUES (CASE WHEN ? > 0 THEN 'income' ELSE 'expense' END, ?, 'move', ?, ?, ?, ?) RETURNING "id"`,
			amount, amount, cat, userId, acc, createdAt).Scan(&id).Error)
		return id
	}
	base := time.Now().Add(-48 * time.Hour)
	out1 := insert(outCat, src, -10, base)
	in1 := insert(inCat, dst, 10, base.Add(10*time.Millisecond))
	out2 := insert(outCat, src, -20, base.Add(time.Hour))
	in2 := insert(inCat, dst, 20, base.Add(time.Hour+10*time.Millisecond))
	salary := insert(otherCat, dst, 99, base)

	var m migrations.Migration
	for _, mig := range migrations.All() {
		if mig.Version == 10 {
			m = mig
		}
	}
	require.NoError(t, m.Up(db))

	related := func(id string) *string {
		var r *string
		require.NoError(t, db.Raw(`SELECT "relatedTransactionId" FROM "transaction" WHERE "id" = ?`, id).Scan(&r).Error)
		return r
	}
	for pair, want := range map[string]string{out1: in1, in1: out1, out2: in2, in2: out2} {
		got := related(pair)
		require.NotNil(t, got)
		assert.Equal(t, want, *got)
	}
	assert.Nil(t, related(salary))
}

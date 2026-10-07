# Sync Contract and Refresh Tokens Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the API safe for an offline-first mobile client: idempotent, last-write-wins writes on every synced entity, one cursor-based change feed, and rotating refresh tokens.

**Architecture:** A migration adds a trigger-maintained `seq` (one global Postgres sequence, per-user advisory lock) and `clientModifiedAt` to six tables. Small helpers in `internal/common/sync.go` give every service the same create-replay / deletes-win / stale-edit rules. A new `internal/changes` package serves `GET /sync/changes`. Refresh tokens live in a new `refresh_token` table behind `auth.RefreshStore`.

**Tech Stack:** Go 1.23, Gin, GORM (pgx), PostgreSQL, testify, swag.

**Spec:** `docs/superpowers/specs/2026-10-07-sync-and-refresh-tokens-design.md`

## Global Constraints

- All DB columns camelCase with explicit `gorm:"column:…"` tags; table names singular (see `CLAUDE.md`).
- Raw SQL migrations only, registered in `internal/migrations/runner.go`.
- Domain errors are `*common.AppError`; handlers respond via `common.RespondAppError` / `RespondError`.
- Scope every query to `userId` (default categories: `userId IS NULL`).
- Soft deletes only.
- Every handler keeps/gets Swagger annotations; regenerate with `make swagger`.
- Access JWT default `1h` (`JWT_EXPIRES_IN`); refresh token TTL `2160h` (`REFRESH_TOKEN_TTL`), sliding; reuse grace `60s`.
- Pull `limit` default `500`, max `1000`.
- Real-DB tests are named `TestRealDB_*`, use `testutil.NewRealDB(t)` and skip when Postgres is unreachable. Run with `go test -p 1 ./...` (see `CLAUDE.md` for env vars).
- Commits: Conventional Commits. The user's global rule is "do not commit"; only run the commit steps if the user has authorized commits for this execution, otherwise leave changes staged.

## Review Focus

1. **Refresh response lost on a flaky network** → the client retries with the just-used token; it must get a fresh pair (within 60 s), not a family revocation. Test: Task 11 `TestRealDB_RefreshStore_ReuseWithinGrace`.
2. **Device clock ahead of the server** → a future `clientModifiedAt` must be clamped to server now, so later edits are not rejected as stale forever. Tests: Task 2 `TestClientTime_ClampsFuture`, Task 3 `TestRealDB_Sync_Update_FutureClockIsClamped`.
3. **Client cursor ahead of the server** (DB reset/restore) → `409`, never a silent empty feed. Test: Task 9 `TestRealDB_Changes_CursorAheadIsConflict`.
4. **A page boundary splitting a transfer pair** → both legs are delivered across pages, each pointing at the other. Test: Task 9 `TestRealDB_Changes_PagingDeliversEverythingOnce` (uses a transfer).
5. **Account soft-deleted while its transactions stay live** → the account arrives in `deleted`, its transactions still arrive in `changes`. Test: Task 9 `TestRealDB_Changes_DeletedAccountKeepsTransactions`.

---

### Task 0: Branch

The work builds on migrations 009–011, which are uncommitted on `feat/api-v1-prefix` at the time of writing.

- [ ] **Step 1:** Ask the user to commit (or confirm) the outstanding work on `feat/api-v1-prefix`. Do not commit it yourself.
- [ ] **Step 2:** `git switch -c feat/sync-and-refresh-tokens`
- [ ] **Step 3:** Baseline: `go build ./... && go test -p 1 ./...` — note any pre-existing failures before changing anything.

---

### Task 1: Test seeds and the `seq` / `clientModifiedAt` migration

**Files:**
- Create: `internal/testutil/seed.go`
- Modify: `internal/testutil/realdb.go` (add `NewRealDBShared`)
- Create: `internal/migrations/012_sync_seq.go`
- Modify: `internal/migrations/runner.go`
- Test: `internal/migrations/sync_seq_realdb_test.go`

**Interfaces:**
- Produces: `testutil.SeedUser(t, db) string`, `testutil.SeedAccount(t, db, userId, currency string) string`, `testutil.SeedCategory(t, db, userId, typ string) string`, `testutil.Seq(t, db, table, id string) int64`, `testutil.NewRealDBShared(t) *gorm.DB`; DB sequence `sync_seq`; columns `seq bigint NOT NULL`, `clientModifiedAt timestamptz` on `account`, `category`, `transaction`, `budget`, `recurring_transaction`, `user_settings`.

- [ ] **Step 1: Add raw-SQL seed helpers** (raw SQL so any package's tests can use them without import cycles)

`internal/testutil/seed.go`:

```go
package testutil

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// SeedUser inserts a user with a unique email and returns its id.
func SeedUser(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var id string
	err := db.Raw(`INSERT INTO "user" ("email", "password") VALUES (?, 'hashed') RETURNING "id"`,
		"test-"+uuid.NewString()+"@example.com").Scan(&id).Error
	require.NoError(t, err)
	return id
}

// SeedAccount inserts an account with balance 1000 and returns its id.
func SeedAccount(t *testing.T, db *gorm.DB, userId, currency string) string {
	t.Helper()
	var id string
	err := db.Raw(`INSERT INTO "account" ("name", "currency", "balance", "userId") VALUES ('Test', ?, 1000, ?) RETURNING "id"`,
		currency, userId).Scan(&id).Error
	require.NoError(t, err)
	return id
}

// SeedCategory inserts a user category of type "income" or "expense" and returns its id.
func SeedCategory(t *testing.T, db *gorm.DB, userId, typ string) string {
	t.Helper()
	var id string
	err := db.Raw(`INSERT INTO "category" ("name", "type", "userId") VALUES (?, ?, ?) RETURNING "id"`,
		"Cat "+uuid.NewString(), typ, userId).Scan(&id).Error
	require.NoError(t, err)
	return id
}

// Seq returns the sync sequence number of a row, including soft-deleted rows.
func Seq(t *testing.T, db *gorm.DB, table, id string) int64 {
	t.Helper()
	var seq int64
	err := db.Raw(fmt.Sprintf(`SELECT "seq" FROM %q WHERE "id" = ?`, table), id).Scan(&seq).Error
	require.NoError(t, err)
	return seq
}
```

- [ ] **Step 2: Add `NewRealDBShared`** to `internal/testutil/realdb.go`. Move the body of `setupOnce.Do(...)` plus the skip check into a helper and reuse it:

```go
// ensureRealDB connects and migrates once per test binary, or skips the test.
func ensureRealDB(t *testing.T) {
	t.Helper()
	setupOnce.Do(func() {
		// … existing body of setupOnce.Do, unchanged …
	})
	if setupErr != nil {
		t.Skipf("real db unavailable, skipping: %v", setupErr)
	}
}

// NewRealDB returns a *gorm.DB backed by the real test database.
// … keep the existing doc comment …
func NewRealDB(t *testing.T) *gorm.DB {
	t.Helper()
	ensureRealDB(t)
	tx := sharedDB.Begin()
	t.Cleanup(func() { tx.Rollback() })
	return tx
}

// NewRealDBShared returns the shared connection without a wrapping transaction,
// for tests that need several concurrent, committed transactions. Such tests
// must delete what they create (deleting the user cascades to its rows).
func NewRealDBShared(t *testing.T) *gorm.DB {
	t.Helper()
	ensureRealDB(t)
	return sharedDB
}
```

- [ ] **Step 3: Write the failing migration test**

`internal/migrations/sync_seq_realdb_test.go`:

```go
package migrations_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/testutil"
)

var syncedTables = []string{"account", "category", "transaction", "budget", "recurring_transaction", "user_settings"}

func TestRealDB_SyncSeq_ColumnsExistAndAreFilled(t *testing.T) {
	db := testutil.NewRealDB(t)
	for _, table := range syncedTables {
		var nullable string
		require.NoError(t, db.Raw(`SELECT is_nullable FROM information_schema.columns
			WHERE table_name = ? AND column_name = 'seq'`, table).Scan(&nullable).Error)
		assert.Equal(t, "NO", nullable, table)

		var cma int64
		require.NoError(t, db.Raw(`SELECT COUNT(*) FROM information_schema.columns
			WHERE table_name = ? AND column_name = 'clientModifiedAt'`, table).Scan(&cma).Error)
		assert.Equal(t, int64(1), cma, table)
	}
}

func TestRealDB_SyncSeq_BumpsOnInsertUpdateAndSoftDelete(t *testing.T) {
	db := testutil.NewRealDB(t)
	userId := testutil.SeedUser(t, db)
	accId := testutil.SeedAccount(t, db, userId, "USD")

	inserted := testutil.Seq(t, db, "account", accId)
	require.Positive(t, inserted)

	require.NoError(t, db.Exec(`UPDATE "account" SET "name" = 'Renamed' WHERE "id" = ?`, accId).Error)
	updated := testutil.Seq(t, db, "account", accId)
	assert.Greater(t, updated, inserted)

	require.NoError(t, db.Exec(`UPDATE "account" SET "deletedAt" = now() WHERE "id" = ?`, accId).Error)
	assert.Greater(t, testutil.Seq(t, db, "account", accId), updated)
}

func TestRealDB_SyncSeq_DefaultCategoryWithoutUserGetsSeq(t *testing.T) {
	db := testutil.NewRealDB(t)
	var id string
	require.NoError(t, db.Raw(`INSERT INTO "category" ("name", "type", "isDefault") VALUES ('Default X', 'expense', true) RETURNING "id"`).Scan(&id).Error)
	assert.Positive(t, testutil.Seq(t, db, "category", id))
}
```

- [ ] **Step 4: Run it to see it fail**

Run: `go test -p 1 ./internal/migrations/ -run TestRealDB_SyncSeq -v`
Expected: FAIL — `column "seq" does not exist` (or `is_nullable` empty).

- [ ] **Step 5: Write the migration**

`internal/migrations/012_sync_seq.go`:

```go
package migrations

import (
	"fmt"

	"gorm.io/gorm"
)

// syncedTables are the tables mobile clients mirror through GET /sync/changes.
var syncedTables = []string{"account", "category", "transaction", "budget", "recurring_transaction", "user_settings"}

// Every write to a synced table takes a new value from one global sequence, so a
// client can pull "everything after seq N". The trigger first takes a per-user
// advisory lock: one user's writes are serialized, their seq values become
// visible in commit order, and a pull can never skip a row that commits late.
// Default categories (userId NULL) only change in migrations/seeding and take no lock.
func migration012() Migration {
	return Migration{
		Version: 12,
		Name:    "SyncSeqAndClientModifiedAt",
		Up: func(db *gorm.DB) error {
			return db.Transaction(func(tx *gorm.DB) error {
				if err := tx.Exec(`
					CREATE SEQUENCE IF NOT EXISTS sync_seq;
					CREATE OR REPLACE FUNCTION sync_bump_seq() RETURNS trigger AS $$
					BEGIN
						IF NEW."userId" IS NOT NULL THEN
							PERFORM pg_advisory_xact_lock(hashtext(NEW."userId"::text));
						END IF;
						NEW."seq" := nextval('sync_seq');
						RETURN NEW;
					END
					$$ LANGUAGE plpgsql;
				`).Error; err != nil {
					return err
				}
				for _, table := range syncedTables {
					if err := tx.Exec(fmt.Sprintf(`
						ALTER TABLE "%[1]s" ADD COLUMN IF NOT EXISTS "seq" bigint;
						ALTER TABLE "%[1]s" ADD COLUMN IF NOT EXISTS "clientModifiedAt" timestamptz;
						UPDATE "%[1]s" SET "seq" = nextval('sync_seq') WHERE "seq" IS NULL;
						ALTER TABLE "%[1]s" ALTER COLUMN "seq" SET NOT NULL;
						CREATE INDEX IF NOT EXISTS "IDX_%[1]s_user_seq" ON "%[1]s" ("userId", "seq");
						DROP TRIGGER IF EXISTS "TRG_%[1]s_sync_seq" ON "%[1]s";
						CREATE TRIGGER "TRG_%[1]s_sync_seq" BEFORE INSERT OR UPDATE ON "%[1]s"
							FOR EACH ROW EXECUTE FUNCTION sync_bump_seq();
					`, table)).Error; err != nil {
						return fmt.Errorf("%s: %w", table, err)
					}
				}
				return nil
			})
		},
	}
}
```

Register it in `runner.go` after `migration011(),`:

```go
	migration012(),
```

- [ ] **Step 6: Run the tests**

Run: `go test -p 1 ./internal/migrations/ -run TestRealDB_SyncSeq -v`
Expected: PASS (3 tests). Then `go test -p 1 ./...` — all previously passing tests still pass.

- [ ] **Step 7: Commit**

```bash
git add internal/testutil/seed.go internal/testutil/realdb.go internal/migrations/012_sync_seq.go internal/migrations/runner.go internal/migrations/sync_seq_realdb_test.go
git commit -m "feat(sync): add seq and clientModifiedAt to synced tables"
```

---

### Task 2: Shared sync helpers and model fields

**Files:**
- Create: `internal/common/sync.go`
- Test: `internal/common/sync_test.go`, `internal/common/sync_realdb_test.go`
- Modify: `internal/account/model.go`, `internal/category/model.go`, `internal/transaction/model.go`, `internal/budget/model.go`, `internal/recurring_transaction/model.go`, `internal/user/settings_model.go`
- Modify: `internal/transaction/fixes_test.go:62-66` (sqlmock UPDATE args)

**Interfaces:**
- Consumes: Task 1 columns and seeds.
- Produces (all in package `common`):
  - `type SyncFields struct { Seq int64; ClientModifiedAt *time.Time }` — embedded in the six models (JSON `seq`, `clientModifiedAt`).
  - `type CreateMeta struct { ID *string; ClientModifiedAt *time.Time }` — JSON `id`, `clientModifiedAt`.
  - `type UpdateMeta struct { ClientModifiedAt *time.Time }`.
  - `func NewGone(message string) *AppError` (410).
  - `func ClientTime(t *time.Time) *time.Time` — clamps a future time to now.
  - `func IsStale(stored, incoming *time.Time) bool`.
  - `func CreateReplay(db *gorm.DB, table string, id *string, userId string) (bool, error)`.
  - `func GoneIfDeleted(db *gorm.DB, table, id, userId string, err error) error`.
  - `func IgnoreIfDeleted(db *gorm.DB, table, id, userId string, err error) error`.

- [ ] **Step 1: Write failing unit tests**

`internal/common/sync_test.go`:

```go
package common

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func ptr(t time.Time) *time.Time { return &t }

func TestIsStale(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	assert.False(t, IsStale(nil, ptr(base)), "no stored edit → apply")
	assert.False(t, IsStale(ptr(base), nil), "no incoming time → apply")
	assert.True(t, IsStale(ptr(base), ptr(base.Add(-time.Second))), "older → stale")
	assert.False(t, IsStale(ptr(base), ptr(base)), "equal → apply")
	assert.False(t, IsStale(ptr(base), ptr(base.Add(time.Second))), "newer → apply")
}

func TestClientTime_ClampsFuture(t *testing.T) {
	assert.Nil(t, ClientTime(nil))
	past := time.Now().Add(-time.Minute)
	assert.Equal(t, past, *ClientTime(&past))
	future := time.Now().Add(24 * time.Hour)
	got := ClientTime(&future)
	assert.WithinDuration(t, time.Now(), *got, time.Second)
}

func TestIsStale_FutureStoredValueCannotBlockEdits(t *testing.T) {
	// Values are clamped before storing, so a stored value is never in the future.
	stored := ClientTime(ptr(time.Now().Add(48 * time.Hour)))
	assert.False(t, IsStale(stored, ptr(time.Now().Add(time.Second))))
}
```

`internal/common/sync_realdb_test.go`:

```go
package common_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/common"
	"ger_fin_api/internal/testutil"
)

func code(t *testing.T, err error) int {
	t.Helper()
	appErr, ok := err.(*common.AppError)
	require.True(t, ok, "expected *AppError, got %v", err)
	return appErr.Code
}

func TestRealDB_CreateReplay(t *testing.T) {
	db := testutil.NewRealDB(t)
	owner := testutil.SeedUser(t, db)
	other := testutil.SeedUser(t, db)
	accId := testutil.SeedAccount(t, db, owner, "USD")

	replay, err := common.CreateReplay(db, "account", nil, owner)
	require.NoError(t, err)
	assert.False(t, replay, "no client id → normal create")

	fresh := uuid.NewString()
	replay, err = common.CreateReplay(db, "account", &fresh, owner)
	require.NoError(t, err)
	assert.False(t, replay, "unknown id → normal create")

	replay, err = common.CreateReplay(db, "account", &accId, owner)
	require.NoError(t, err)
	assert.True(t, replay, "own live row → return it")

	_, err = common.CreateReplay(db, "account", &accId, other)
	assert.Equal(t, 409, code(t, err), "someone else's id")

	require.NoError(t, db.Exec(`UPDATE "account" SET "deletedAt" = now() WHERE "id" = ?`, accId).Error)
	_, err = common.CreateReplay(db, "account", &accId, owner)
	assert.Equal(t, 410, code(t, err), "own deleted row")
}

func TestRealDB_GoneAndIgnoreIfDeleted(t *testing.T) {
	db := testutil.NewRealDB(t)
	owner := testutil.SeedUser(t, db)
	txId := uuid.NewString()
	accId := testutil.SeedAccount(t, db, owner, "USD")
	catId := testutil.SeedCategory(t, db, owner, "expense")
	require.NoError(t, db.Exec(`INSERT INTO "transaction" ("id","type","amount","description","categoryId","userId","accountId","deletedAt")
		VALUES (?, 'expense', -1, 'x', ?, ?, ?, now())`, txId, catId, owner, accId).Error)

	notFound := common.NewNotFound("Transaction not found")
	assert.Equal(t, 410, code(t, common.GoneIfDeleted(db, "transaction", txId, owner, notFound)))
	assert.NoError(t, common.IgnoreIfDeleted(db, "transaction", txId, owner, notFound))

	missing := uuid.NewString()
	assert.Equal(t, 404, code(t, common.GoneIfDeleted(db, "transaction", missing, owner, notFound)))
	assert.Equal(t, 404, code(t, common.IgnoreIfDeleted(db, "transaction", missing, owner, notFound)))

	forbidden := common.NewForbidden("nope")
	assert.Equal(t, 403, code(t, common.GoneIfDeleted(db, "transaction", txId, owner, forbidden)), "non-404 errors pass through")
}
```

- [ ] **Step 2: Run to see them fail**

Run: `go test -p 1 ./internal/common/ -v`
Expected: FAIL to compile — `undefined: IsStale`, `ClientTime`, `CreateReplay`, …

- [ ] **Step 3: Implement the helpers**

`internal/common/sync.go`:

```go
package common

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// SyncFields is embedded in every model mobile clients mirror. Seq is set by a
// database trigger on every write (read-only for GORM); ClientModifiedAt is the
// device time of the latest client edit, used for last-write-wins.
type SyncFields struct {
	Seq              int64      `gorm:"column:seq;->"           json:"seq"`
	ClientModifiedAt *time.Time `gorm:"column:clientModifiedAt" json:"clientModifiedAt,omitempty"`
}

// CreateMeta lets an offline client choose the id of a new row, so retrying
// the create is safe.
type CreateMeta struct {
	ID               *string    `json:"id"               binding:"omitempty,uuid"`
	ClientModifiedAt *time.Time `json:"clientModifiedAt" binding:"omitempty"`
}

// UpdateMeta carries the device time of a client edit (last-write-wins).
type UpdateMeta struct {
	ClientModifiedAt *time.Time `json:"clientModifiedAt" binding:"omitempty"`
}

func NewGone(message string) *AppError {
	return &AppError{Code: 410, Msg: message, ErrText: "Gone"}
}

// ClientTime clamps a device timestamp to the server clock, so a device whose
// clock runs ahead cannot make its edits win over every later edit.
func ClientTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	if now := time.Now(); t.After(now) {
		return &now
	}
	return t
}

// IsStale reports whether an incoming client edit is older than the stored
// one; stale edits are dropped (last-write-wins).
func IsStale(stored, incoming *time.Time) bool {
	in := ClientTime(incoming)
	return stored != nil && in != nil && in.Before(*stored)
}

type rowState int

const (
	rowAbsent rowState = iota
	rowLive
	rowDeleted
	rowForeign
)

// lookupRow inspects a row including soft-deleted ones. table must be a
// trusted constant.
func lookupRow(db *gorm.DB, table, id, userId string) (rowState, error) {
	var rows []struct {
		UserId    *string    `gorm:"column:userId"`
		DeletedAt *time.Time `gorm:"column:deletedAt"`
	}
	if err := db.Raw(fmt.Sprintf(`SELECT "userId", "deletedAt" FROM %q WHERE "id" = ?`, table), id).
		Scan(&rows).Error; err != nil {
		return rowAbsent, err
	}
	switch {
	case len(rows) == 0:
		return rowAbsent, nil
	case rows[0].UserId == nil || *rows[0].UserId != userId:
		return rowForeign, nil
	case rows[0].DeletedAt != nil:
		return rowDeleted, nil
	default:
		return rowLive, nil
	}
}

// CreateReplay reports whether a create with this client-chosen id was
// already applied, in which case the caller returns the existing row instead
// of creating a duplicate. A deleted row answers 410 (deletes win); an id
// owned by someone else answers 409.
func CreateReplay(db *gorm.DB, table string, id *string, userId string) (bool, error) {
	if id == nil {
		return false, nil
	}
	state, err := lookupRow(db, table, *id, userId)
	if err != nil {
		return false, err
	}
	switch state {
	case rowLive:
		return true, nil
	case rowDeleted:
		return false, NewGone("This item was deleted")
	case rowForeign:
		return false, NewConflict("id is already in use")
	default:
		return false, nil
	}
}

func isNotFound(err error) bool {
	var appErr *AppError
	return errors.As(err, &appErr) && appErr.Code == 404
}

// GoneIfDeleted turns a lookup's 404 into 410 when the user's row exists but
// is soft-deleted, so an offline edit of a deleted row is dropped by the
// client instead of retried. Other errors pass through.
func GoneIfDeleted(db *gorm.DB, table, id, userId string, err error) error {
	if !isNotFound(err) {
		return err
	}
	state, lookupErr := lookupRow(db, table, id, userId)
	if lookupErr != nil {
		return lookupErr
	}
	if state == rowDeleted {
		return NewGone("This item was deleted")
	}
	return err
}

// IgnoreIfDeleted makes a repeated delete succeed: a lookup's 404 becomes nil
// when the user's row is already soft-deleted. Other errors pass through.
func IgnoreIfDeleted(db *gorm.DB, table, id, userId string, err error) error {
	if !isNotFound(err) {
		return err
	}
	state, lookupErr := lookupRow(db, table, id, userId)
	if lookupErr != nil {
		return lookupErr
	}
	if state == rowDeleted {
		return nil
	}
	return err
}
```

- [ ] **Step 4: Embed `SyncFields` in the six models.** In each struct add the line directly after `common.BaseModel`:

```go
	common.SyncFields
```

Files: `internal/account/model.go` (`Account`), `internal/category/model.go` (`Category`), `internal/transaction/model.go` (`Transaction`), `internal/budget/model.go` (`Budget`), `internal/recurring_transaction/model.go` (`RecurringTransaction`), `internal/user/settings_model.go` (`UserSettings`).

- [ ] **Step 5: Fix the sqlmock `UPDATE` expectation.** `Save` now also writes `clientModifiedAt` (after `deletedAt`), so `TestUpdate_ChangesAccountId` sees 17 args and `accountId` moves from index 8 to 9. In `internal/transaction/fixes_test.go`:

```go
	args := make([]driver.Value, 17)
	for i := range args {
		args[i] = sqlmock.AnyArg()
	}
	args[9] = equalsArg{testNewAccId} // "accountId" in the SET list
```

- [ ] **Step 6: Run everything**

Run: `go test -p 1 ./...`
Expected: PASS, including the new `internal/common` tests. If another sqlmock test fails on argument count, apply the same fix (one extra `clientModifiedAt` arg right after `deletedAt`).

- [ ] **Step 7: Commit**

```bash
git add internal/common/sync.go internal/common/sync_test.go internal/common/sync_realdb_test.go internal/*/model.go internal/user/settings_model.go internal/transaction/fixes_test.go
git commit -m "feat(sync): add shared replay, deletes-win and last-write-wins helpers"
```

---

### Task 3: Transactions and transfers

**Files:**
- Modify: `internal/transaction/dto.go`, `internal/transaction/service.go` (`Create`, `Update`, `Remove`, `Transfer`), `internal/transaction/handler.go` (Swagger only)
- Test: `internal/transaction/sync_realdb_test.go`

**Interfaces:**
- Consumes: `common.CreateMeta`, `common.UpdateMeta`, `common.CreateReplay`, `common.GoneIfDeleted`, `common.IgnoreIfDeleted`, `common.IsStale`, `common.ClientTime`.
- Produces: `CreateTransactionDto` embeds `common.CreateMeta`; `UpdateTransactionDto` embeds `common.UpdateMeta`; `CreateTransferDto` gains `ID *string` (`json:"id"`), `RelatedID *string` (`json:"relatedId"`), `ClientModifiedAt *time.Time`.

- [ ] **Step 1: Write the failing tests**

`internal/transaction/sync_realdb_test.go`:

```go
package transaction

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"ger_fin_api/internal/common"
	"ger_fin_api/internal/testutil"
)

func balanceOf(t *testing.T, db *gorm.DB, accountId string) float64 {
	t.Helper()
	var b float64
	require.NoError(t, db.Raw(`SELECT "balance" FROM "account" WHERE "id" = ?`, accountId).Scan(&b).Error)
	return b
}

func appCode(t *testing.T, err error) int {
	t.Helper()
	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	return appErr.Code
}

func expenseDto(id *string, accountId, categoryId string) CreateTransactionDto {
	return CreateTransactionDto{
		CreateMeta: common.CreateMeta{ID: id},
		Type:       common.TransactionTypeExpense, Amount: -10, Description: "Lunch",
		CategoryId: categoryId, AccountId: accountId,
	}
}

func TestRealDB_Sync_CreateTwiceWithSameIdAppliesOnce(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userId := seedUser(t, db)
	acc := seedAccount(t, db, userId)
	cat := seedCategory(t, db, userId, common.TransactionTypeExpense)
	id := uuid.NewString()

	first, err := svc.Create(expenseDto(&id, acc.ID, cat.ID), userId)
	require.NoError(t, err)
	second, err := svc.Create(expenseDto(&id, acc.ID, cat.ID), userId)
	require.NoError(t, err)

	assert.Equal(t, id, first.ID)
	assert.Equal(t, id, second.ID)
	assert.InDelta(t, 990.0, balanceOf(t, db, acc.ID), 0.001, "balance moved once")
}

func TestRealDB_Sync_CreateWithForeignOrDeletedId(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	owner := seedUser(t, db)
	other := seedUser(t, db)
	acc := seedAccount(t, db, owner)
	cat := seedCategory(t, db, owner, common.TransactionTypeExpense)
	otherAcc := seedAccount(t, db, other)
	otherCat := seedCategory(t, db, other, common.TransactionTypeExpense)
	id := uuid.NewString()

	_, err := svc.Create(expenseDto(&id, acc.ID, cat.ID), owner)
	require.NoError(t, err)

	_, err = svc.Create(expenseDto(&id, otherAcc.ID, otherCat.ID), other)
	assert.Equal(t, 409, appCode(t, err))

	require.NoError(t, svc.Remove(id, owner))
	_, err = svc.Create(expenseDto(&id, acc.ID, cat.ID), owner)
	assert.Equal(t, 410, appCode(t, err))
}

func TestRealDB_Sync_UpdateLastWriteWins(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userId := seedUser(t, db)
	acc := seedAccount(t, db, userId)
	cat := seedCategory(t, db, userId, common.TransactionTypeExpense)
	tx, err := svc.Create(expenseDto(nil, acc.ID, cat.ID), userId)
	require.NoError(t, err)

	t2 := time.Now().Add(-time.Minute)
	newer := "newer"
	_, err = svc.Update(tx.ID, UpdateTransactionDto{UpdateMeta: common.UpdateMeta{ClientModifiedAt: &t2}, Description: &newer}, userId)
	require.NoError(t, err)

	t1 := t2.Add(-time.Minute)
	older := "older"
	got, err := svc.Update(tx.ID, UpdateTransactionDto{UpdateMeta: common.UpdateMeta{ClientModifiedAt: &t1}, Description: &older}, userId)
	require.NoError(t, err)
	assert.Equal(t, "newer", got.Description, "stale edit ignored, current row returned")
	require.NotNil(t, got.ClientModifiedAt)
	assert.WithinDuration(t, t2, *got.ClientModifiedAt, time.Millisecond)
}

func TestRealDB_Sync_Update_FutureClockIsClamped(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userId := seedUser(t, db)
	acc := seedAccount(t, db, userId)
	cat := seedCategory(t, db, userId, common.TransactionTypeExpense)
	tx, err := svc.Create(expenseDto(nil, acc.ID, cat.ID), userId)
	require.NoError(t, err)

	future := time.Now().Add(48 * time.Hour)
	ahead := "clock ahead"
	_, err = svc.Update(tx.ID, UpdateTransactionDto{UpdateMeta: common.UpdateMeta{ClientModifiedAt: &future}, Description: &ahead}, userId)
	require.NoError(t, err)

	now := time.Now()
	later := "later edit"
	got, err := svc.Update(tx.ID, UpdateTransactionDto{UpdateMeta: common.UpdateMeta{ClientModifiedAt: &now}, Description: &later}, userId)
	require.NoError(t, err)
	assert.Equal(t, "later edit", got.Description)
}

func TestRealDB_Sync_UpdateDeletedIsGoneAndDeleteIsIdempotent(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userId := seedUser(t, db)
	acc := seedAccount(t, db, userId)
	cat := seedCategory(t, db, userId, common.TransactionTypeExpense)
	tx, err := svc.Create(expenseDto(nil, acc.ID, cat.ID), userId)
	require.NoError(t, err)

	require.NoError(t, svc.Remove(tx.ID, userId))
	require.NoError(t, svc.Remove(tx.ID, userId), "second delete succeeds")
	assert.InDelta(t, 1000.0, balanceOf(t, db, acc.ID), 0.001, "balance reversed once")

	desc := "edit"
	_, err = svc.Update(tx.ID, UpdateTransactionDto{Description: &desc}, userId)
	assert.Equal(t, 410, appCode(t, err))

	_, err = svc.Update(uuid.NewString(), UpdateTransactionDto{Description: &desc}, userId)
	assert.Equal(t, 404, appCode(t, err), "unknown id stays 404")
}

func TestRealDB_Sync_TransferReplayAppliesOnce(t *testing.T) {
	db := testutil.NewRealDB(t)
	seedDefaultCategories(t, db)
	svc := NewService(db)
	userId := seedUser(t, db)
	src := seedAccountNamed(t, db, userId, "Src")
	dst := seedAccountNamed(t, db, userId, "Dst")
	id, relatedId := uuid.NewString(), uuid.NewString()
	dto := CreateTransferDto{
		ID: &id, RelatedID: &relatedId,
		SourceAccountId: src.ID, DestinationAccountId: dst.ID, Amount: 25, Description: "move",
	}

	first, err := svc.Transfer(dto, userId)
	require.NoError(t, err)
	second, err := svc.Transfer(dto, userId)
	require.NoError(t, err)

	assert.Equal(t, id, first.SourceTransaction.ID)
	assert.Equal(t, relatedId, first.DestinationTransaction.ID)
	assert.Equal(t, id, second.SourceTransaction.ID)
	assert.Equal(t, relatedId, second.DestinationTransaction.ID)
	assert.InDelta(t, 975.0, balanceOf(t, db, src.ID), 0.001)
	assert.InDelta(t, 1025.0, balanceOf(t, db, dst.ID), 0.001)
}

func TestRealDB_Sync_TransferIdsMustComeTogether(t *testing.T) {
	db := testutil.NewRealDB(t)
	seedDefaultCategories(t, db)
	svc := NewService(db)
	userId := seedUser(t, db)
	src := seedAccountNamed(t, db, userId, "Src")
	dst := seedAccountNamed(t, db, userId, "Dst")
	id := uuid.NewString()
	base := CreateTransferDto{SourceAccountId: src.ID, DestinationAccountId: dst.ID, Amount: 5, Description: "x"}

	onlyId := base
	onlyId.ID = &id
	_, err := svc.Transfer(onlyId, userId)
	assert.Equal(t, 400, appCode(t, err))

	same := base
	same.ID, same.RelatedID = &id, &id
	_, err = svc.Transfer(same, userId)
	assert.Equal(t, 400, appCode(t, err))
}
```

- [ ] **Step 2: Run to see them fail**

Run: `go test -p 1 ./internal/transaction/ -run TestRealDB_Sync -v`
Expected: FAIL to compile — unknown field `CreateMeta` / `UpdateMeta` / `ID` in DTOs.

- [ ] **Step 3: Update the DTOs** in `internal/transaction/dto.go`:

```go
type CreateTransactionDto struct {
	common.CreateMeta
	Type            common.TransactionType `json:"type"            binding:"required,oneof=income expense"`
	// … remaining fields unchanged …
}

type UpdateTransactionDto struct {
	common.UpdateMeta
	Type            *common.TransactionType `json:"type"            binding:"omitempty,oneof=income expense"`
	// … remaining fields unchanged …
}

type CreateTransferDto struct {
	// ID and RelatedID are client-chosen ids for the source and destination
	// legs; send both or neither. They make retrying the transfer safe.
	ID                   *string    `json:"id"                   binding:"omitempty,uuid"`
	RelatedID            *string    `json:"relatedId"            binding:"omitempty,uuid"`
	ClientModifiedAt     *time.Time `json:"clientModifiedAt"     binding:"omitempty"`
	SourceAccountId      string     `json:"sourceAccountId"      binding:"required,uuid"`
	// … remaining fields unchanged …
}
```

- [ ] **Step 4: Update the service** in `internal/transaction/service.go`.

At the top of `Create`:

```go
func (s *Service) Create(dto CreateTransactionDto, userId string) (*Transaction, error) {
	if replay, err := common.CreateReplay(s.db, "transaction", dto.ID, userId); err != nil {
		return nil, err
	} else if replay {
		return s.FindOne(*dto.ID, userId)
	}
	// … existing body …
```

and where the struct is built, set the id and client time:

```go
	tx := &Transaction{
		// … existing fields …
		BalanceApplied:   true,
	}
	tx.ClientModifiedAt = common.ClientTime(dto.ClientModifiedAt)
	if dto.ID != nil {
		tx.ID = *dto.ID
	}
```

At the top of `Update`:

```go
	tx, err := s.FindOne(id, userId)
	if err != nil {
		return nil, common.GoneIfDeleted(s.db, "transaction", id, userId, err)
	}
	if common.IsStale(tx.ClientModifiedAt, dto.ClientModifiedAt) {
		return tx, nil
	}
```

and next to the other field assignments (before the `s.db.Transaction(...)` save):

```go
	if dto.ClientModifiedAt != nil {
		tx.ClientModifiedAt = common.ClientTime(dto.ClientModifiedAt)
	}
```

At the top of `Remove`:

```go
	tx, err := s.FindOne(id, userId)
	if err != nil {
		return common.IgnoreIfDeleted(s.db, "transaction", id, userId, err)
	}
```

At the top of `Transfer` (before the same-account check):

```go
	if (dto.ID == nil) != (dto.RelatedID == nil) {
		return nil, common.NewBadRequest("id and relatedId must be sent together")
	}
	if dto.ID != nil && *dto.ID == *dto.RelatedID {
		return nil, common.NewBadRequest("id and relatedId must differ")
	}
	if replay, err := common.CreateReplay(s.db, "transaction", dto.ID, userId); err != nil {
		return nil, err
	} else if replay {
		src, err := s.FindOne(*dto.ID, userId)
		if err != nil {
			return nil, err
		}
		dst, err := s.FindOne(*dto.RelatedID, userId)
		if err != nil {
			return nil, err
		}
		return &TransferResult{SourceTransaction: src, DestinationTransaction: dst}, nil
	}
```

and give the two legs their ids:

```go
	srcDto := CreateTransactionDto{
		CreateMeta:      common.CreateMeta{ID: dto.ID, ClientModifiedAt: dto.ClientModifiedAt},
		// … existing fields …
	}
	dstDto := CreateTransactionDto{
		CreateMeta:      common.CreateMeta{ID: dto.RelatedID, ClientModifiedAt: dto.ClientModifiedAt},
		// … existing fields …
	}
```

- [ ] **Step 5: Swagger.** On `create`, `update`, `remove`, `createTransfer` in `handler.go` add:

```go
// @Failure		409 {object} common.ErrorResponse
// @Failure		410 {object} common.ErrorResponse
```

(`409` only on the two create handlers; `410` on create, update and transfer.)

- [ ] **Step 6: Run the tests**

Run: `go test -p 1 ./internal/transaction/ -v`
Expected: PASS (new and existing).

- [ ] **Step 7: Commit**

```bash
git add internal/transaction
git commit -m "feat(transaction): make writes idempotent and last-write-wins"
```

---

### Task 4: Accounts

**Files:**
- Modify: `internal/account/dto.go`, `internal/account/service.go` (`Create`, `Update`, `Remove`), `internal/account/handler.go` (Swagger)
- Test: `internal/account/sync_realdb_test.go`

**Interfaces:**
- Consumes: Task 2 helpers.
- Produces: `CreateAccountDto` embeds `common.CreateMeta`; `UpdateAccountDto` embeds `common.UpdateMeta`.

- [ ] **Step 1: Write the failing tests**

`internal/account/sync_realdb_test.go`:

```go
package account

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/common"
	"ger_fin_api/internal/testutil"
)

func appCode(t *testing.T, err error) int {
	t.Helper()
	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	return appErr.Code
}

func TestRealDB_Sync_Account(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userId := testutil.SeedUser(t, db)
	id := uuid.NewString()
	dto := CreateAccountDto{CreateMeta: common.CreateMeta{ID: &id}, Name: "Cash", Currency: "VND"}

	first, err := svc.Create(userId, dto)
	require.NoError(t, err)
	second, err := svc.Create(userId, dto)
	require.NoError(t, err)
	assert.Equal(t, id, first.ID)
	assert.Equal(t, id, second.ID)

	t2 := time.Now().Add(-time.Minute)
	t1 := t2.Add(-time.Minute)
	newer, older := "Wallet", "Old name"
	_, err = svc.Update(id, userId, UpdateAccountDto{UpdateMeta: common.UpdateMeta{ClientModifiedAt: &t2}, Name: &newer})
	require.NoError(t, err)
	got, err := svc.Update(id, userId, UpdateAccountDto{UpdateMeta: common.UpdateMeta{ClientModifiedAt: &t1}, Name: &older})
	require.NoError(t, err)
	assert.Equal(t, "Wallet", got.Name)

	require.NoError(t, svc.Remove(id, userId))
	require.NoError(t, svc.Remove(id, userId))
	_, err = svc.Update(id, userId, UpdateAccountDto{Name: &newer})
	assert.Equal(t, 410, appCode(t, err))
	_, err = svc.Create(userId, dto)
	assert.Equal(t, 410, appCode(t, err))
}
```

- [ ] **Step 2: Run to see it fail**

Run: `go test -p 1 ./internal/account/ -run TestRealDB_Sync -v`
Expected: FAIL to compile — unknown field `CreateMeta`.

- [ ] **Step 3: Implement.** `dto.go`: add `common.CreateMeta` as the first field of `CreateAccountDto` and `common.UpdateMeta` as the first field of `UpdateAccountDto` (import `ger_fin_api/internal/common`).

`service.go`:

```go
func (s *Service) Create(userId string, dto CreateAccountDto) (*Account, error) {
	if replay, err := common.CreateReplay(s.db, "account", dto.ID, userId); err != nil {
		return nil, err
	} else if replay {
		return s.FindOne(*dto.ID, userId)
	}
	a := &Account{
		// … existing fields …
	}
	a.ClientModifiedAt = common.ClientTime(dto.ClientModifiedAt)
	if dto.ID != nil {
		a.ID = *dto.ID
	}
	// … existing Create call …
}
```

In `Update`, replace the opening lookup and add the stale check and the timestamp:

```go
	a, err := s.FindOne(id, userId)
	if err != nil {
		return nil, common.GoneIfDeleted(s.db, "account", id, userId, err)
	}
	if common.IsStale(a.ClientModifiedAt, dto.ClientModifiedAt) {
		return a, nil
	}
	if dto.ClientModifiedAt != nil {
		a.ClientModifiedAt = common.ClientTime(dto.ClientModifiedAt)
	}
	// … existing field updates and Save …
```

In `Remove`:

```go
	a, err := s.FindOne(id, userId)
	if err != nil {
		return common.IgnoreIfDeleted(s.db, "account", id, userId, err)
	}
```

- [ ] **Step 4: Swagger.** Add `409`/`410` failures to `create`, `410` to `update` in `handler.go` (same lines as Task 3 Step 5).

- [ ] **Step 5: Run** `go test -p 1 ./internal/account/ -v` → PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/account
git commit -m "feat(account): make writes idempotent and last-write-wins"
```

---

### Task 5: Categories

**Files:**
- Modify: `internal/category/dto.go`, `internal/category/service.go` (`Create`, `Update`, `Remove`), `internal/category/handler.go` (Swagger)
- Test: `internal/category/sync_realdb_test.go`

**Interfaces:**
- Produces: `CreateCategoryDto` embeds `common.CreateMeta`; `UpdateCategoryDto` embeds `common.UpdateMeta`.

- [ ] **Step 1: Write the failing test**

`internal/category/sync_realdb_test.go`:

```go
package category

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/common"
	"ger_fin_api/internal/testutil"
)

func appCode(t *testing.T, err error) int {
	t.Helper()
	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	return appErr.Code
}

func TestRealDB_Sync_Category(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userId := testutil.SeedUser(t, db)
	id := uuid.NewString()
	dto := CreateCategoryDto{CreateMeta: common.CreateMeta{ID: &id}, Name: "Coffee", Type: common.TransactionTypeExpense}

	first, err := svc.Create(dto, userId)
	require.NoError(t, err)
	second, err := svc.Create(dto, userId)
	require.NoError(t, err)
	assert.Equal(t, id, first.ID)
	assert.Equal(t, id, second.ID)

	t2 := time.Now().Add(-time.Minute)
	t1 := t2.Add(-time.Minute)
	newer, older := "Cafe", "Old"
	_, err = svc.Update(id, UpdateCategoryDto{UpdateMeta: common.UpdateMeta{ClientModifiedAt: &t2}, Name: &newer}, userId)
	require.NoError(t, err)
	got, err := svc.Update(id, UpdateCategoryDto{UpdateMeta: common.UpdateMeta{ClientModifiedAt: &t1}, Name: &older}, userId)
	require.NoError(t, err)
	assert.Equal(t, "Cafe", got.Name)

	require.NoError(t, svc.Remove(id, userId))
	require.NoError(t, svc.Remove(id, userId))
	_, err = svc.Update(id, UpdateCategoryDto{Name: &newer}, userId)
	assert.Equal(t, 410, appCode(t, err))
}
```

- [ ] **Step 2: Run to see it fail**

Run: `go test -p 1 ./internal/category/ -run TestRealDB_Sync -v` → FAIL to compile.

- [ ] **Step 3: Implement.** `dto.go`: embed `common.CreateMeta` / `common.UpdateMeta` as first fields.

`service.go` — `Create`:

```go
func (s *Service) Create(dto CreateCategoryDto, userId string) (*Category, error) {
	if replay, err := common.CreateReplay(s.db, "category", dto.ID, userId); err != nil {
		return nil, err
	} else if replay {
		return s.FindOne(*dto.ID, userId)
	}
	// … existing parent validation …
	cat := &Category{
		// … existing fields …
	}
	cat.ClientModifiedAt = common.ClientTime(dto.ClientModifiedAt)
	if dto.ID != nil {
		cat.ID = *dto.ID
	}
	// … existing Create call …
}
```

`Update` — replace the opening lookup; put the stale check before the default-category guard:

```go
	cat, err := s.FindOne(id, userId)
	if err != nil {
		return nil, common.GoneIfDeleted(s.db, "category", id, userId, err)
	}
	if common.IsStale(cat.ClientModifiedAt, dto.ClientModifiedAt) {
		return cat, nil
	}
	if cat.IsDefault {
		return nil, common.NewForbidden("Cannot update default categories")
	}
```

and before the final `Save`:

```go
	if dto.ClientModifiedAt != nil {
		cat.ClientModifiedAt = common.ClientTime(dto.ClientModifiedAt)
	}
```

`Remove`:

```go
	cat, err := s.FindOne(id, userId)
	if err != nil {
		return common.IgnoreIfDeleted(s.db, "category", id, userId, err)
	}
```

- [ ] **Step 4: Swagger** `409`/`410` on `create`, `410` on `update`.

- [ ] **Step 5: Run** `go test -p 1 ./internal/category/ -v` → PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/category
git commit -m "feat(category): make writes idempotent and last-write-wins"
```

---

### Task 6: Budgets

**Files:**
- Modify: `internal/budget/dto.go`, `internal/budget/service.go` (`Create`, `Update`, `Remove`), `internal/budget/handler.go` (Swagger)
- Test: `internal/budget/sync_realdb_test.go`

**Interfaces:**
- Produces: `CreateBudgetDto` embeds `common.CreateMeta`; `UpdateBudgetDto` embeds `common.UpdateMeta`.

- [ ] **Step 1: Write the failing test.** The budget package's real-DB tests already build the service with `newSvc(db)` (`internal/budget/service_test.go:72`); reuse it.

`internal/budget/sync_realdb_test.go`:

```go
package budget

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/common"
	"ger_fin_api/internal/testutil"
)

func appCode(t *testing.T, err error) int {
	t.Helper()
	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	return appErr.Code
}

func TestRealDB_Sync_Budget(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := newSvc(db)
	userId := testutil.SeedUser(t, db)
	id := uuid.NewString()
	dto := CreateBudgetDto{CreateMeta: common.CreateMeta{ID: &id}, PeriodType: common.BudgetPeriodMonthly, Amount: 500}

	first, err := svc.Create(dto, userId)
	require.NoError(t, err)
	second, err := svc.Create(dto, userId)
	require.NoError(t, err, "replay is not a category/period conflict")
	assert.Equal(t, id, first.ID)
	assert.Equal(t, id, second.ID)

	t2 := time.Now().Add(-time.Minute)
	t1 := t2.Add(-time.Minute)
	newer, older := 700.0, 300.0
	_, err = svc.Update(id, UpdateBudgetDto{UpdateMeta: common.UpdateMeta{ClientModifiedAt: &t2}, Amount: &newer}, userId)
	require.NoError(t, err)
	got, err := svc.Update(id, UpdateBudgetDto{UpdateMeta: common.UpdateMeta{ClientModifiedAt: &t1}, Amount: &older}, userId)
	require.NoError(t, err)
	assert.InDelta(t, 700.0, got.Amount, 0.001)

	require.NoError(t, svc.Remove(id, userId))
	require.NoError(t, svc.Remove(id, userId))
	_, err = svc.Update(id, UpdateBudgetDto{Amount: &newer}, userId)
	assert.Equal(t, 410, appCode(t, err))
}
```

- [ ] **Step 2: Run to see it fail** — `go test -p 1 ./internal/budget/ -run TestRealDB_Sync -v` → FAIL to compile.

- [ ] **Step 3: Implement.** `dto.go`: embed `common.CreateMeta` / `common.UpdateMeta` as first fields.

`service.go` — `Create`, before `validateCategory` (a replay must not hit the period-conflict check):

```go
	if replay, err := common.CreateReplay(s.db, "budget", dto.ID, userId); err != nil {
		return nil, err
	} else if replay {
		return s.findOneRaw(*dto.ID, userId)
	}
```

and after building `b`:

```go
	b.ClientModifiedAt = common.ClientTime(dto.ClientModifiedAt)
	if dto.ID != nil {
		b.ID = *dto.ID
	}
```

`Update`:

```go
	b, err := s.findOneRaw(id, userId)
	if err != nil {
		return nil, common.GoneIfDeleted(s.db, "budget", id, userId, err)
	}
	if common.IsStale(b.ClientModifiedAt, dto.ClientModifiedAt) {
		return b, nil
	}
	if dto.ClientModifiedAt != nil {
		b.ClientModifiedAt = common.ClientTime(dto.ClientModifiedAt)
	}
	// … existing field updates …
```

`Remove`:

```go
	b, err := s.findOneRaw(id, userId)
	if err != nil {
		return common.IgnoreIfDeleted(s.db, "budget", id, userId, err)
	}
```

- [ ] **Step 4: Swagger** `409`/`410` on `create`, `410` on `update`.

- [ ] **Step 5: Run** `go test -p 1 ./internal/budget/ -v` → PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/budget
git commit -m "feat(budget): make writes idempotent and last-write-wins"
```

---

### Task 7: Recurring transactions

**Files:**
- Modify: `internal/recurring_transaction/dto.go`, `internal/recurring_transaction/service.go` (`Create`, `Update`, `Remove`), `internal/recurring_transaction/handler.go` (Swagger)
- Test: `internal/recurring_transaction/sync_realdb_test.go`

**Interfaces:**
- Produces: `CreateRecurringTransactionDto` embeds `common.CreateMeta`; `UpdateRecurringTransactionDto` embeds `common.UpdateMeta`.

- [ ] **Step 1: Write the failing test.** The constructor is `NewService(db *gorm.DB, transactionSvc *transaction.Service)`.

`internal/recurring_transaction/sync_realdb_test.go`:

```go
package recurring_transaction

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/common"
	"ger_fin_api/internal/testutil"
	"ger_fin_api/internal/transaction"
)

func appCode(t *testing.T, err error) int {
	t.Helper()
	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	return appErr.Code
}

func TestRealDB_Sync_RecurringTransaction(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db, transaction.NewService(db))
	userId := testutil.SeedUser(t, db)
	accId := testutil.SeedAccount(t, db, userId, "USD")
	catId := testutil.SeedCategory(t, db, userId, "expense")
	id := uuid.NewString()
	dto := CreateRecurringTransactionDto{
		CreateMeta: common.CreateMeta{ID: &id},
		Type:       common.TransactionTypeExpense, Amount: 9, Description: "Netflix",
		CategoryId: catId, AccountId: accId, Frequency: common.RecurrenceFrequencyMonthly,
		StartDate: time.Now().AddDate(0, 1, 0),
	}

	first, err := svc.Create(dto, userId)
	require.NoError(t, err)
	second, err := svc.Create(dto, userId)
	require.NoError(t, err)
	assert.Equal(t, id, first.ID)
	assert.Equal(t, id, second.ID)

	t2 := time.Now().Add(-time.Minute)
	t1 := t2.Add(-time.Minute)
	newer, older := "Netflix HD", "Old"
	_, err = svc.Update(id, UpdateRecurringTransactionDto{UpdateMeta: common.UpdateMeta{ClientModifiedAt: &t2}, Description: &newer}, userId)
	require.NoError(t, err)
	got, err := svc.Update(id, UpdateRecurringTransactionDto{UpdateMeta: common.UpdateMeta{ClientModifiedAt: &t1}, Description: &older}, userId)
	require.NoError(t, err)
	assert.Equal(t, "Netflix HD", got.Description)

	require.NoError(t, svc.Remove(id, userId))
	require.NoError(t, svc.Remove(id, userId))
	_, err = svc.Update(id, UpdateRecurringTransactionDto{Description: &newer}, userId)
	assert.Equal(t, 410, appCode(t, err))
}
```

- [ ] **Step 2: Run to see it fail** — `go test -p 1 ./internal/recurring_transaction/ -run TestRealDB_Sync -v` → FAIL to compile.

- [ ] **Step 3: Implement.** `dto.go`: embed the meta structs as first fields.

`service.go` — `Create`:

```go
func (s *Service) Create(dto CreateRecurringTransactionDto, userId string) (*RecurringTransaction, error) {
	if replay, err := common.CreateReplay(s.db, "recurring_transaction", dto.ID, userId); err != nil {
		return nil, err
	} else if replay {
		return s.FindOne(*dto.ID, userId)
	}
	// … existing validation and struct …
	rt.ClientModifiedAt = common.ClientTime(dto.ClientModifiedAt)
	if dto.ID != nil {
		rt.ID = *dto.ID
	}
	// … existing Create + FindOne …
}
```

`Update`:

```go
	rt, err := s.FindOne(id, userId)
	if err != nil {
		return nil, common.GoneIfDeleted(s.db, "recurring_transaction", id, userId, err)
	}
	if common.IsStale(rt.ClientModifiedAt, dto.ClientModifiedAt) {
		return rt, nil
	}
	if dto.ClientModifiedAt != nil {
		rt.ClientModifiedAt = common.ClientTime(dto.ClientModifiedAt)
	}
	// … existing body …
```

`Remove`:

```go
	rt, err := s.FindOne(id, userId)
	if err != nil {
		return common.IgnoreIfDeleted(s.db, "recurring_transaction", id, userId, err)
	}
```

- [ ] **Step 4: Swagger** `409`/`410` on `create`, `410` on `update`.

- [ ] **Step 5: Run** `go test -p 1 ./internal/recurring_transaction/ -v` → PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/recurring_transaction
git commit -m "feat(recurring): make writes idempotent and last-write-wins"
```

---

### Task 8: User settings

**Files:**
- Modify: `internal/user/dto.go` (`UpdateSettingsDto`), `internal/user/service.go` (`UpdateSettings`)
- Test: `internal/user/settings_sync_realdb_test.go`

**Interfaces:**
- Produces: `UpdateSettingsDto` embeds `common.UpdateMeta`.

- [ ] **Step 1: Write the failing test**

`internal/user/settings_sync_realdb_test.go`:

```go
package user

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/common"
	"ger_fin_api/internal/testutil"
)

func TestRealDB_Sync_SettingsLastWriteWins(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userId := testutil.SeedUser(t, db)

	t2 := time.Now().Add(-time.Minute)
	t1 := t2.Add(-time.Minute)
	eur, jpy := "EUR", "JPY"
	_, err := svc.UpdateSettings(userId, UpdateSettingsDto{UpdateMeta: common.UpdateMeta{ClientModifiedAt: &t2}, BaseCurrency: &eur})
	require.NoError(t, err)
	got, err := svc.UpdateSettings(userId, UpdateSettingsDto{UpdateMeta: common.UpdateMeta{ClientModifiedAt: &t1}, BaseCurrency: &jpy})
	require.NoError(t, err)
	assert.Equal(t, "EUR", got.BaseCurrency)
}
```

- [ ] **Step 2: Run to see it fail** — `go test -p 1 ./internal/user/ -run TestRealDB_Sync -v` → FAIL to compile.

- [ ] **Step 3: Implement.** `dto.go`: add `common.UpdateMeta` as the first field of `UpdateSettingsDto` (import `ger_fin_api/internal/common`).

`service.go` — at the start of `UpdateSettings`, after `GetSettings`:

```go
	if common.IsStale(settings.ClientModifiedAt, dto.ClientModifiedAt) {
		return settings, nil
	}
	if dto.ClientModifiedAt != nil {
		settings.ClientModifiedAt = common.ClientTime(dto.ClientModifiedAt)
	}
```

- [ ] **Step 4: Run** `go test -p 1 ./internal/user/ -v` → PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/user
git commit -m "feat(user): apply last-write-wins to settings updates"
```

---

### Task 9: `GET /sync/changes`

**Files:**
- Create: `internal/changes/dto.go`, `internal/changes/service.go`, `internal/changes/handler.go`
- Modify: `cmd/main.go` (wire service + handler)
- Test: `internal/changes/service_realdb_test.go`

**Interfaces:**
- Consumes: models with `SyncFields` (Task 2), ids/semantics from Tasks 3–8.
- Produces: `changes.NewService(db *gorm.DB) *Service`; `(*Service).Changes(userId string, cursor int64, limit int) (*Response, error)`; `changes.NewHandler(svc *Service) *Handler`; route `GET /api/v1/sync/changes?cursor=&limit=`. JSON:
  `{ "changes": { "accounts", "categories", "transactions", "budgets", "recurringTransactions", "userSettings" }, "deleted": [{ "entity", "id" }], "cursor", "hasMore" }`. Entity names in `deleted`: `account`, `category`, `transaction`, `budget`, `recurringTransaction`, `userSettings`.

- [ ] **Step 1: Write the failing tests**

`internal/changes/service_realdb_test.go`:

```go
package changes

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/account"
	"ger_fin_api/internal/category"
	"ger_fin_api/internal/common"
	recurringTx "ger_fin_api/internal/recurring_transaction"
	"ger_fin_api/internal/testutil"
	"ger_fin_api/internal/transaction"
)

func idsOf[T any](rows []T, id func(T) string) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = id(r)
	}
	return out
}

func accountIds(r *Response) []string {
	return idsOf(r.Changes.Accounts, func(a account.Account) string { return a.ID })
}
func categoryIds(r *Response) []string {
	return idsOf(r.Changes.Categories, func(c category.Category) string { return c.ID })
}
func transactionIds(r *Response) []string {
	return idsOf(r.Changes.Transactions, func(t transaction.Transaction) string { return t.ID })
}

func TestRealDB_Changes_FirstPullIsScopedToUserPlusDefaults(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userId := testutil.SeedUser(t, db)
	other := testutil.SeedUser(t, db)
	accId := testutil.SeedAccount(t, db, userId, "USD")
	catId := testutil.SeedCategory(t, db, userId, "expense")
	otherAcc := testutil.SeedAccount(t, db, other, "USD")
	var defaultCat string
	require.NoError(t, db.Raw(`INSERT INTO "category" ("name","type","isDefault") VALUES ('Default Y','expense',true) RETURNING "id"`).Scan(&defaultCat).Error)

	res, err := svc.Changes(userId, 0, 1000)
	require.NoError(t, err)
	assert.Contains(t, accountIds(res), accId)
	assert.NotContains(t, accountIds(res), otherAcc)
	assert.Contains(t, categoryIds(res), catId)
	assert.Contains(t, categoryIds(res), defaultCat)
	assert.False(t, res.HasMore)
	assert.Positive(t, res.Cursor)
	for _, a := range res.Changes.Accounts {
		assert.Positive(t, a.Seq)
	}
}

func TestRealDB_Changes_CursorReturnsOnlyNewerRows(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userId := testutil.SeedUser(t, db)
	a1 := testutil.SeedAccount(t, db, userId, "USD")
	a2 := testutil.SeedAccount(t, db, userId, "USD")

	first, err := svc.Changes(userId, 0, 1000)
	require.NoError(t, err)
	require.NoError(t, db.Exec(`UPDATE "account" SET "name" = 'Changed' WHERE "id" = ?`, a2).Error)

	next, err := svc.Changes(userId, first.Cursor, 1000)
	require.NoError(t, err)
	assert.Equal(t, []string{a2}, accountIds(next))
	assert.NotContains(t, accountIds(next), a1)
	assert.Greater(t, next.Cursor, first.Cursor)

	empty, err := svc.Changes(userId, next.Cursor, 1000)
	require.NoError(t, err)
	assert.Empty(t, accountIds(empty))
	assert.Equal(t, next.Cursor, empty.Cursor, "cursor unchanged when nothing is new")
}

func TestRealDB_Changes_PagingDeliversEverythingOnce(t *testing.T) {
	db := testutil.NewRealDB(t)
	require.NoError(t, category.NewService(db).SeedDefaultCategories())
	svc := NewService(db)
	txSvc := transaction.NewService(db)
	userId := testutil.SeedUser(t, db)
	src := testutil.SeedAccount(t, db, userId, "USD")
	dst := testutil.SeedAccount(t, db, userId, "USD")
	tr, err := txSvc.Transfer(transaction.CreateTransferDto{SourceAccountId: src, DestinationAccountId: dst, Amount: 5, Description: "move"}, userId)
	require.NoError(t, err)

	seen := map[string]int{}
	related := map[string]string{}
	var cursor int64
	for page := 0; page < 1000; page++ {
		res, err := svc.Changes(userId, cursor, 2)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(accountIds(res))+len(categoryIds(res))+len(transactionIds(res))+len(res.Deleted), 2)
		for _, tx := range res.Changes.Transactions {
			seen[tx.ID]++
			if tx.RelatedTransactionId != nil {
				related[tx.ID] = *tx.RelatedTransactionId
			}
		}
		if res.HasMore {
			assert.Greater(t, res.Cursor, cursor)
		}
		cursor = res.Cursor
		if !res.HasMore {
			break
		}
	}
	srcId, dstId := tr.SourceTransaction.ID, tr.DestinationTransaction.ID
	assert.Equal(t, 1, seen[srcId])
	assert.Equal(t, 1, seen[dstId])
	assert.Equal(t, dstId, related[srcId])
	assert.Equal(t, srcId, related[dstId])
}

func TestRealDB_Changes_DeletedAccountKeepsTransactions(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userId := testutil.SeedUser(t, db)
	accId := testutil.SeedAccount(t, db, userId, "USD")
	catId := testutil.SeedCategory(t, db, userId, "expense")
	tx, err := transaction.NewService(db).Create(transaction.CreateTransactionDto{
		Type: common.TransactionTypeExpense, Amount: -3, Description: "x", CategoryId: catId, AccountId: accId,
	}, userId)
	require.NoError(t, err)
	require.NoError(t, account.NewService(db).Remove(accId, userId))

	res, err := svc.Changes(userId, 0, 1000)
	require.NoError(t, err)
	assert.Contains(t, res.Deleted, DeletedRef{Entity: "account", ID: accId})
	assert.NotContains(t, accountIds(res), accId)
	assert.Contains(t, transactionIds(res), tx.ID)
}

func TestRealDB_Changes_IncludesSchedulerCreatedTransactions(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	txSvc := transaction.NewService(db)
	userId := testutil.SeedUser(t, db)
	accId := testutil.SeedAccount(t, db, userId, "USD")
	catId := testutil.SeedCategory(t, db, userId, "expense")
	rtSvc := recurringTx.NewService(db, txSvc)
	_, err := rtSvc.Create(recurringTx.CreateRecurringTransactionDto{
		Type: common.TransactionTypeExpense, Amount: 4, Description: "Gym",
		CategoryId: catId, AccountId: accId, Frequency: common.RecurrenceFrequencyMonthly,
		StartDate: time.Now().Add(-time.Hour),
	}, userId)
	require.NoError(t, err)
	before, err := svc.Changes(userId, 0, 1000)
	require.NoError(t, err)

	require.NoError(t, rtSvc.ProcessDueRecurringTransactions())

	after, err := svc.Changes(userId, before.Cursor, 1000)
	require.NoError(t, err)
	require.NotEmpty(t, after.Changes.Transactions)
	assert.Equal(t, "Gym", after.Changes.Transactions[0].Description)
}

func TestRealDB_Changes_CursorAheadIsConflict(t *testing.T) {
	db := testutil.NewRealDB(t)
	svc := NewService(db)
	userId := testutil.SeedUser(t, db)

	_, err := svc.Changes(userId, 1<<62, 10)
	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, 409, appErr.Code)
}
```

(If `recurring_transaction`'s due-processing expects `nextDueDate` strictly in the past, `StartDate: time.Now().Add(-time.Hour)` covers it; check `processOne` if the test finds no row.)

- [ ] **Step 2: Run to see it fail** — `go test -p 1 ./internal/changes/ -v` → FAIL (package does not exist).

- [ ] **Step 3: Implement the DTOs**

`internal/changes/dto.go`:

```go
package changes

import (
	"ger_fin_api/internal/account"
	"ger_fin_api/internal/budget"
	"ger_fin_api/internal/category"
	recurringTx "ger_fin_api/internal/recurring_transaction"
	"ger_fin_api/internal/transaction"
	"ger_fin_api/internal/user"
)

// ChangeSet holds live rows changed since the cursor, as raw model rows
// (relations as ids, no computed fields).
type ChangeSet struct {
	Accounts              []account.Account                  `json:"accounts"`
	Categories            []category.Category                `json:"categories"`
	Transactions          []transaction.Transaction          `json:"transactions"`
	Budgets               []budget.Budget                    `json:"budgets"`
	RecurringTransactions []recurringTx.RecurringTransaction `json:"recurringTransactions"`
	UserSettings          []user.UserSettings                `json:"userSettings"`
}

// DeletedRef identifies a row soft-deleted since the cursor.
type DeletedRef struct {
	Entity string `json:"entity"`
	ID     string `json:"id"`
}

// Response is one page of the change feed.
type Response struct {
	Changes ChangeSet    `json:"changes"`
	Deleted []DeletedRef `json:"deleted"`
	Cursor  int64        `json:"cursor"`
	HasMore bool         `json:"hasMore"`
}

func newResponse(cursor int64) *Response {
	return &Response{
		Changes: ChangeSet{
			Accounts:              []account.Account{},
			Categories:            []category.Category{},
			Transactions:          []transaction.Transaction{},
			Budgets:               []budget.Budget{},
			RecurringTransactions: []recurringTx.RecurringTransaction{},
			UserSettings:          []user.UserSettings{},
		},
		Deleted: []DeletedRef{},
		Cursor:  cursor,
	}
}
```

- [ ] **Step 4: Implement the service**

`internal/changes/service.go`:

```go
package changes

import (
	"database/sql"

	"ger_fin_api/internal/common"

	"gorm.io/gorm"
)

const (
	DefaultLimit = 500
	MaxLimit     = 1000
)

type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

// indexSQL lists changed rows across all synced tables in seq order. Default
// categories (userId NULL) belong to every user.
const indexSQL = `
SELECT * FROM (
	SELECT 'account' AS entity, "id", "seq", "deletedAt" IS NOT NULL AS deleted
		FROM "account" WHERE "userId" = @user AND "seq" > @cursor
	UNION ALL
	SELECT 'category', "id", "seq", "deletedAt" IS NOT NULL
		FROM "category" WHERE ("userId" = @user OR "userId" IS NULL) AND "seq" > @cursor
	UNION ALL
	SELECT 'transaction', "id", "seq", "deletedAt" IS NOT NULL
		FROM "transaction" WHERE "userId" = @user AND "seq" > @cursor
	UNION ALL
	SELECT 'budget', "id", "seq", "deletedAt" IS NOT NULL
		FROM "budget" WHERE "userId" = @user AND "seq" > @cursor
	UNION ALL
	SELECT 'recurringTransaction', "id", "seq", "deletedAt" IS NOT NULL
		FROM "recurring_transaction" WHERE "userId" = @user AND "seq" > @cursor
	UNION ALL
	SELECT 'userSettings', "id", "seq", "deletedAt" IS NOT NULL
		FROM "user_settings" WHERE "userId" = @user AND "seq" > @cursor
) AS changed
ORDER BY "seq"
LIMIT @limit`

type indexRow struct {
	Entity  string `gorm:"column:entity"`
	ID      string `gorm:"column:id"`
	Seq     int64  `gorm:"column:seq"`
	Deleted bool   `gorm:"column:deleted"`
}

// Changes returns up to limit rows changed after cursor, read from one
// consistent snapshot.
func (s *Service) Changes(userId string, cursor int64, limit int) (*Response, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	resp := newResponse(cursor)

	err := s.db.Transaction(func(tx *gorm.DB) error {
		var latest int64
		if err := tx.Raw(`SELECT last_value FROM sync_seq`).Scan(&latest).Error; err != nil {
			return err
		}
		if cursor > latest {
			return common.NewConflict("cursor is ahead of the server; pull again from 0")
		}

		var index []indexRow
		if err := tx.Raw(indexSQL, map[string]interface{}{
			"user": userId, "cursor": cursor, "limit": limit + 1,
		}).Scan(&index).Error; err != nil {
			return err
		}
		if len(index) > limit {
			resp.HasMore = true
			index = index[:limit]
		}

		live := map[string][]string{}
		for _, row := range index {
			if row.Deleted {
				resp.Deleted = append(resp.Deleted, DeletedRef{Entity: row.Entity, ID: row.ID})
			} else {
				live[row.Entity] = append(live[row.Entity], row.ID)
			}
			resp.Cursor = row.Seq
		}
		return loadLive(tx, live, &resp.Changes)
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func loadLive(tx *gorm.DB, live map[string][]string, out *ChangeSet) error {
	loads := []struct {
		entity string
		dst    interface{}
	}{
		{"account", &out.Accounts},
		{"category", &out.Categories},
		{"transaction", &out.Transactions},
		{"budget", &out.Budgets},
		{"recurringTransaction", &out.RecurringTransactions},
		{"userSettings", &out.UserSettings},
	}
	for _, l := range loads {
		ids := live[l.entity]
		if len(ids) == 0 {
			continue
		}
		if err := tx.Where(`"id" IN ?`, ids).Order(`"seq"`).Find(l.dst).Error; err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 5: Implement the handler**

`internal/changes/handler.go`:

```go
package changes

import (
	"ger_fin_api/internal/common"
	"ger_fin_api/internal/middleware"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, auth gin.HandlerFunc) {
	g := rg.Group("/sync", auth)
	g.GET("/changes", h.changes)
}

type changesQuery struct {
	Cursor int64 `form:"cursor" binding:"omitempty,min=0"`
	Limit  int   `form:"limit"  binding:"omitempty,min=1,max=1000"`
}

// @Summary		Pull changes
// @Description	Rows changed after the cursor across all synced entities, in seq order. Repeat with the returned cursor while hasMore is true. 409 means the cursor is ahead of the server: pull again from 0.
// @Tags		sync
// @Produce		json
// @Param		cursor query integer false "Last cursor received (0 for a full pull)" default(0)
// @Param		limit  query integer false "Max rows per page" default(500)
// @Success		200 {object} Response
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Failure		409 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/sync/changes [get]
func (h *Handler) changes(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	var q changesQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		common.RespondError(c, common.NewBadRequest(err.Error()))
		return
	}

	resp, err := h.svc.Changes(claims.Sub, q.Cursor, q.Limit)
	if err != nil {
		common.RespondAppError(c, err)
		return
	}
	c.JSON(200, resp)
}
```

- [ ] **Step 6: Wire it** in `cmd/main.go`: import `"ger_fin_api/internal/changes"`, then

```go
	changesSvc     := changes.NewService(db)
```

next to the other services,

```go
	changesHandler     := changes.NewHandler(changesSvc)
```

next to the other handlers, and

```go
	changesHandler.RegisterRoutes(api, jwtAuth)
```

after `budgetHandler.RegisterRoutes(api, jwtAuth)`.

- [ ] **Step 7: Run** `go test -p 1 ./internal/changes/ -v && go build ./...` → PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/changes cmd/main.go
git commit -m "feat(sync): add cursor-based change feed endpoint"
```

---

### Task 10: Concurrency guarantee of the cursor

**Files:**
- Test: `internal/changes/concurrency_realdb_test.go`

**Interfaces:**
- Consumes: `testutil.NewRealDBShared`, `changes.Service.Changes`, the migration 012 trigger lock.

- [ ] **Step 1: Write the test**

`internal/changes/concurrency_realdb_test.go`:

```go
package changes

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/testutil"
)

// Writer A allocates a seq and keeps its transaction open; writer B (same user)
// must wait for A, so a pull taken in between cannot advance past A's row.
func TestRealDB_Changes_NeverSkipsALateCommit(t *testing.T) {
	db := testutil.NewRealDBShared(t)
	userId := testutil.SeedUser(t, db) // committed
	t.Cleanup(func() { db.Exec(`DELETE FROM "user" WHERE "id" = ?`, userId) })
	svc := NewService(db)

	start, err := svc.Changes(userId, 0, MaxLimit)
	require.NoError(t, err)

	txA := db.Begin()
	var accA string
	require.NoError(t, txA.Raw(`INSERT INTO "account" ("name","currency","userId") VALUES ('A','USD',?) RETURNING "id"`, userId).Scan(&accA).Error)

	bDone := make(chan string, 1)
	go func() {
		var accB string
		_ = db.Raw(`INSERT INTO "account" ("name","currency","userId") VALUES ('B','USD',?) RETURNING "id"`, userId).Scan(&accB).Error
		bDone <- accB
	}()

	select {
	case <-bDone:
		txA.Rollback()
		t.Fatal("writer B committed while writer A held the user's lock")
	case <-time.After(300 * time.Millisecond):
	}

	mid, err := svc.Changes(userId, start.Cursor, MaxLimit)
	require.NoError(t, err)
	assert.Empty(t, accountIds(mid), "neither uncommitted A nor blocked B is visible")

	require.NoError(t, txA.Commit().Error)
	accB := <-bDone
	require.NotEmpty(t, accB)

	after, err := svc.Changes(userId, mid.Cursor, MaxLimit)
	require.NoError(t, err)
	assert.Contains(t, accountIds(after), accA)
	assert.Contains(t, accountIds(after), accB)
}
```

- [ ] **Step 2: Run** `go test -p 1 ./internal/changes/ -run NeverSkips -v -count=1` → PASS. To confirm it guards something, temporarily comment out the `PERFORM pg_advisory_xact_lock(...)` line, drop and recreate the test database (`DROP DATABASE "<name>_test"`), rerun → FAIL ("writer B committed…"); restore the line, recreate the test DB again, rerun → PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/changes/concurrency_realdb_test.go
git commit -m "test(sync): prove the change feed never skips a late commit"
```

---

### Task 11: Refresh token store

**Files:**
- Create: `internal/migrations/013_refresh_token.go`
- Modify: `internal/migrations/runner.go`
- Create: `internal/auth/refresh_store.go`
- Modify: `internal/config/config.go` (`RefreshTTL`), `.env.example`
- Test: `internal/auth/refresh_store_realdb_test.go`

**Interfaces:**
- Produces: table `refresh_token`; `auth.NewRefreshStore(db *gorm.DB, ttl time.Duration) *RefreshStore`; `(*RefreshStore).Issue(userId string) (string, error)`; `(*RefreshStore).Rotate(token string) (userId string, next string, err error)`; `(*RefreshStore).RevokeFamilyOf(token, userId string) error`; `auth.ErrInvalidRefreshToken` (`*common.AppError`, 401); `config.JWTConfig.RefreshTTL time.Duration`.

- [ ] **Step 1: Write the failing tests**

`internal/auth/refresh_store_realdb_test.go`:

```go
package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/testutil"
)

const testRefreshTTL = 90 * 24 * time.Hour

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestRefreshStore(t *testing.T) (*RefreshStore, *fakeClock, string) {
	t.Helper()
	db := testutil.NewRealDB(t)
	clock := &fakeClock{t: time.Now()}
	store := NewRefreshStore(db, testRefreshTTL)
	store.now = clock.now
	return store, clock, testutil.SeedUser(t, db)
}

func TestRealDB_RefreshStore_RotateIssuesSuccessor(t *testing.T) {
	store, _, userId := newTestRefreshStore(t)
	first, err := store.Issue(userId)
	require.NoError(t, err)

	gotUser, next, err := store.Rotate(first)
	require.NoError(t, err)
	assert.Equal(t, userId, gotUser)
	assert.NotEqual(t, first, next)

	_, _, err = store.Rotate(next)
	assert.NoError(t, err, "successor is valid")
}

func TestRealDB_RefreshStore_RejectsUnknownAndExpired(t *testing.T) {
	store, clock, userId := newTestRefreshStore(t)
	_, _, err := store.Rotate("not-a-token")
	assert.Equal(t, ErrInvalidRefreshToken, err)

	tok, err := store.Issue(userId)
	require.NoError(t, err)
	clock.advance(testRefreshTTL + time.Minute)
	_, _, err = store.Rotate(tok)
	assert.Equal(t, ErrInvalidRefreshToken, err)
}

func TestRealDB_RefreshStore_SlidingExpiry(t *testing.T) {
	store, clock, userId := newTestRefreshStore(t)
	tok, err := store.Issue(userId)
	require.NoError(t, err)

	clock.advance(80 * 24 * time.Hour)
	_, next, err := store.Rotate(tok)
	require.NoError(t, err)

	clock.advance(80 * 24 * time.Hour) // 160 days after login, 80 after last use
	_, _, err = store.Rotate(next)
	assert.NoError(t, err)
}

func TestRealDB_RefreshStore_ReuseAfterGraceRevokesFamily(t *testing.T) {
	store, clock, userId := newTestRefreshStore(t)
	first, err := store.Issue(userId)
	require.NoError(t, err)
	_, second, err := store.Rotate(first)
	require.NoError(t, err)

	clock.advance(reuseGrace + time.Second)
	_, _, err = store.Rotate(first)
	assert.Equal(t, ErrInvalidRefreshToken, err, "reused token rejected")

	_, _, err = store.Rotate(second)
	assert.Equal(t, ErrInvalidRefreshToken, err, "whole family revoked")
}

func TestRealDB_RefreshStore_ReuseWithinGrace(t *testing.T) {
	store, clock, userId := newTestRefreshStore(t)
	first, err := store.Issue(userId)
	require.NoError(t, err)
	_, second, err := store.Rotate(first) // response lost on the way to the phone
	require.NoError(t, err)

	clock.advance(5 * time.Second)
	_, retried, err := store.Rotate(first) // phone retries with the token it still has
	require.NoError(t, err)
	assert.NotEqual(t, second, retried)

	_, _, err = store.Rotate(retried)
	assert.NoError(t, err, "family not revoked")
}

func TestRealDB_RefreshStore_RevokeFamilyOf(t *testing.T) {
	store, _, userId := newTestRefreshStore(t)
	tok, err := store.Issue(userId)
	require.NoError(t, err)

	require.NoError(t, store.RevokeFamilyOf(tok, "00000000-0000-0000-0000-000000000000"))
	_, next, err := store.Rotate(tok)
	require.NoError(t, err, "another user cannot revoke it")

	require.NoError(t, store.RevokeFamilyOf(next, userId))
	_, _, err = store.Rotate(next)
	assert.Equal(t, ErrInvalidRefreshToken, err)
}
```

- [ ] **Step 2: Run to see it fail** — `go test -p 1 ./internal/auth/ -run RefreshStore -v` → FAIL to compile.

- [ ] **Step 3: Migration**

`internal/migrations/013_refresh_token.go`:

```go
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
```

Register `migration013(),` after `migration012(),` in `runner.go`.

- [ ] **Step 4: Store**

`internal/auth/refresh_store.go`:

```go
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"time"

	"ger_fin_api/internal/common"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// reuseGrace lets a client that lost the refresh response (flaky mobile
// network) retry with the token it still holds without being treated as a thief.
const reuseGrace = 60 * time.Second

var ErrInvalidRefreshToken = common.NewUnauthorized("Invalid refresh token")

// RefreshStore issues and rotates opaque refresh tokens. Each successful
// rotation extends the session by ttl (sliding expiry).
type RefreshStore struct {
	db  *gorm.DB
	ttl time.Duration
	now func() time.Time
}

func NewRefreshStore(db *gorm.DB, ttl time.Duration) *RefreshStore {
	return &RefreshStore{db: db, ttl: ttl, now: time.Now}
}

type refreshRow struct {
	ID        string     `gorm:"column:id"`
	UserId    string     `gorm:"column:userId"`
	FamilyId  string     `gorm:"column:familyId"`
	ExpiresAt time.Time  `gorm:"column:expiresAt"`
	UsedAt    *time.Time `gorm:"column:usedAt"`
	RevokedAt *time.Time `gorm:"column:revokedAt"`
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// Issue starts a new token family for a fresh login and prunes expired tokens.
func (s *RefreshStore) Issue(userId string) (string, error) {
	if err := s.db.Exec(`DELETE FROM "refresh_token" WHERE "expiresAt" < ?`, s.now()).Error; err != nil {
		return "", err
	}
	return s.insert(s.db, userId, uuid.NewString())
}

func (s *RefreshStore) insert(db *gorm.DB, userId, familyId string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	err := db.Exec(`INSERT INTO "refresh_token" ("userId", "familyId", "tokenHash", "expiresAt") VALUES (?, ?, ?, ?)`,
		userId, familyId, hashToken(token), s.now().Add(s.ttl)).Error
	if err != nil {
		return "", err
	}
	return token, nil
}

// Rotate exchanges a refresh token for its successor in the same family and
// returns the owner's id. Presenting a token that was already used (outside
// reuseGrace) revokes the whole family: one of the two holders is a thief.
func (s *RefreshStore) Rotate(token string) (string, string, error) {
	var userId, next string
	reused := false
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var rows []refreshRow
		if err := tx.Raw(`SELECT "id", "userId", "familyId", "expiresAt", "usedAt", "revokedAt"
			FROM "refresh_token" WHERE "tokenHash" = ? FOR UPDATE`, hashToken(token)).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return ErrInvalidRefreshToken
		}
		row, now := rows[0], s.now()
		if row.RevokedAt != nil || !row.ExpiresAt.After(now) {
			return ErrInvalidRefreshToken
		}
		if row.UsedAt != nil && now.Sub(*row.UsedAt) > reuseGrace {
			reused = true
			return tx.Exec(`UPDATE "refresh_token" SET "revokedAt" = ? WHERE "familyId" = ? AND "revokedAt" IS NULL`,
				now, row.FamilyId).Error
		}
		if row.UsedAt == nil {
			if err := tx.Exec(`UPDATE "refresh_token" SET "usedAt" = ? WHERE "id" = ?`, now, row.ID).Error; err != nil {
				return err
			}
		}
		issued, err := s.insert(tx, row.UserId, row.FamilyId)
		if err != nil {
			return err
		}
		userId, next = row.UserId, issued
		return nil
	})
	if err != nil {
		return "", "", err
	}
	if reused {
		// The revocation above must commit, so the error is returned only after it.
		return "", "", ErrInvalidRefreshToken
	}
	return userId, next, nil
}

// RevokeFamilyOf ends the session that token belongs to (logout). Tokens of
// other users are left alone.
func (s *RefreshStore) RevokeFamilyOf(token, userId string) error {
	return s.db.Exec(`UPDATE "refresh_token" SET "revokedAt" = ?
		WHERE "revokedAt" IS NULL AND "familyId" = (
			SELECT "familyId" FROM "refresh_token" WHERE "tokenHash" = ? AND "userId" = ?)`,
		s.now(), hashToken(token), userId).Error
}
```

- [ ] **Step 5: Config.** In `internal/config/config.go` add to `JWTConfig`:

```go
	// RefreshTTL from REFRESH_TOKEN_TTL; each refresh extends the session by this much.
	RefreshTTL time.Duration
```

parse it next to `expiresIn`:

```go
	refreshTTL := 90 * 24 * time.Hour
	if raw := viper.GetString("REFRESH_TOKEN_TTL"); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil {
			refreshTTL = d
		}
	}
```

and set `RefreshTTL: refreshTTL,` in the `JWT: JWTConfig{…}` literal. Add to `.env.example`:

```
REFRESH_TOKEN_TTL=2160h
```

- [ ] **Step 6: Run** `go test -p 1 ./internal/auth/ -run RefreshStore -v && go build ./...` → PASS (6 tests).

- [ ] **Step 7: Commit**

```bash
git add internal/migrations/013_refresh_token.go internal/migrations/runner.go internal/auth/refresh_store.go internal/auth/refresh_store_realdb_test.go internal/config/config.go .env.example
git commit -m "feat(auth): add rotating refresh token store"
```

---

### Task 12: Login, refresh and logout endpoints

**Files:**
- Modify: `internal/auth/dto.go`, `internal/auth/service.go`, `internal/auth/handler.go`, `cmd/main.go`
- Test: `internal/auth/handler_refresh_realdb_test.go`

**Interfaces:**
- Consumes: `RefreshStore` (Task 11), `config.JWTConfig.RefreshTTL`.
- Produces:
  - `POST /auth/login` → `{"access_token","refresh_token","expires_in"}` (200).
  - `POST /auth/refresh` body `{"refresh_token"}` → same shape (200) or 401.
  - `POST /auth/logout` (bearer) optional body `{"refresh_token"}` → revokes that family too.
  - `(*Service).AccessToken(u *user.User) (string, error)`, `(*Service).FindUser(id string) (*user.User, error)`, `(*Service).AccessTTL() time.Duration`.
  - `auth.NewHandler(svc *Service, tokens TokenRevoker, refresh RefreshTokens) *Handler`.

- [ ] **Step 1: Write the failing test**

`internal/auth/handler_refresh_realdb_test.go`:

```go
package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ger_fin_api/internal/middleware"
	"ger_fin_api/internal/testutil"
	"ger_fin_api/internal/user"
)

const handlerTestSecret = "handler-test-secret-handler-test-secret"

func newAuthRouter(t *testing.T) *gin.Engine {
	t.Helper()
	db := testutil.NewRealDB(t)
	svc := NewService(user.NewService(db), handlerTestSecret, time.Hour)
	revoked := NewTokenStore(db)
	h := NewHandler(svc, revoked, NewRefreshStore(db, 90*24*time.Hour))
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h.RegisterRoutes(r.Group(""), middleware.JWTAuth(handlerTestSecret, revoked))
	return r
}

func send(t *testing.T, r *gin.Engine, path, bearer string, body any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestRealDB_AuthHandler_LoginRefreshLogout(t *testing.T) {
	r := newAuthRouter(t)
	email := uniqueEmail(t)
	code, _ := send(t, r, "/auth/register", "", map[string]string{"email": email, "password": "password123"})
	require.Equal(t, 201, code)

	code, login := send(t, r, "/auth/login", "", map[string]string{"email": email, "password": "password123"})
	require.Equal(t, 200, code)
	require.NotEmpty(t, login["access_token"])
	require.NotEmpty(t, login["refresh_token"])
	assert.Equal(t, float64(3600), login["expires_in"])

	code, refreshed := send(t, r, "/auth/refresh", "", map[string]any{"refresh_token": login["refresh_token"]})
	require.Equal(t, 200, code)
	require.NotEmpty(t, refreshed["access_token"])
	assert.NotEqual(t, login["refresh_token"], refreshed["refresh_token"])

	code, _ = send(t, r, "/auth/logout", refreshed["access_token"].(string), map[string]any{"refresh_token": refreshed["refresh_token"]})
	require.Equal(t, 200, code)

	code, _ = send(t, r, "/auth/refresh", "", map[string]any{"refresh_token": refreshed["refresh_token"]})
	assert.Equal(t, 401, code, "logout revoked the session")
}

func TestRealDB_AuthHandler_RefreshRejectsGarbage(t *testing.T) {
	r := newAuthRouter(t)
	code, _ := send(t, r, "/auth/refresh", "", map[string]any{"refresh_token": "nope"})
	assert.Equal(t, 401, code)
	code, _ = send(t, r, "/auth/refresh", "", map[string]any{})
	assert.Equal(t, 400, code)
}

func TestRealDB_AuthHandler_LogoutWithoutBodyStillWorks(t *testing.T) {
	r := newAuthRouter(t)
	email := uniqueEmail(t)
	send(t, r, "/auth/register", "", map[string]string{"email": email, "password": "password123"})
	_, login := send(t, r, "/auth/login", "", map[string]string{"email": email, "password": "password123"})

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+login["access_token"].(string))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
}
```

- [ ] **Step 2: Run to see it fail** — `go test -p 1 ./internal/auth/ -run AuthHandler -v` → FAIL to compile (`NewHandler` arity).

- [ ] **Step 3: DTOs.** In `internal/auth/dto.go` replace `LoginResponse` and add the new bodies:

```go
// LoginResponse is returned by login and refresh.
type LoginResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"` // access token lifetime in seconds
}

type RefreshDto struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

// LogoutDto is optional; sending the refresh token also ends that session.
type LogoutDto struct {
	RefreshToken *string `json:"refresh_token"`
}
```

- [ ] **Step 4: Service.** In `internal/auth/service.go` extract token signing out of `Login` and add two helpers:

```go
// AccessToken signs a short-lived JWT for u.
func (s *Service) AccessToken(u *user.User) (string, error) {
	claims := common.JWTClaims{
		Sub:   u.ID,
		Email: u.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.New().String(), // jti, used to revoke the token on logout
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.jwtExpiry)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.jwtSecret))
}

func (s *Service) Login(email, password string) (string, error) {
	u, err := s.ValidateUser(email, password)
	if err != nil {
		return "", err
	}
	return s.AccessToken(u)
}

// FindUser loads the owner of a refresh token; a deleted user is unauthorized.
func (s *Service) FindUser(id string) (*user.User, error) {
	u, err := s.userSvc.FindById(id)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, ErrInvalidRefreshToken
	}
	return u, nil
}

// AccessTTL is the access token lifetime reported to clients as expires_in.
func (s *Service) AccessTTL() time.Duration { return s.jwtExpiry }
```

- [ ] **Step 5: Handler.** In `internal/auth/handler.go`:

```go
// RefreshTokens issues, rotates and revokes refresh tokens (see RefreshStore).
type RefreshTokens interface {
	Issue(userId string) (string, error)
	Rotate(token string) (userId string, next string, err error)
	RevokeFamilyOf(token, userId string) error
}

type Handler struct {
	svc     *Service
	tokens  TokenRevoker
	refresh RefreshTokens
}

func NewHandler(svc *Service, tokens TokenRevoker, refresh RefreshTokens) *Handler {
	return &Handler{svc: svc, tokens: tokens, refresh: refresh}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, auth gin.HandlerFunc) {
	g := rg.Group("/auth")
	g.POST("/register", h.register)
	g.POST("/login", h.login)
	g.POST("/refresh", h.refreshTokens)
	g.POST("/logout", auth, h.logout)
}

func (h *Handler) respondWithTokens(c *gin.Context, u *user.User, refreshToken string) {
	access, err := h.svc.AccessToken(u)
	if err != nil {
		common.RespondInternalError(c)
		return
	}
	c.JSON(200, LoginResponse{
		AccessToken:  access,
		RefreshToken: refreshToken,
		ExpiresIn:    int(h.svc.AccessTTL().Seconds()),
	})
}
```

Replace the body of `login` (keep its Swagger block; its `@Success` already points at `LoginResponse`):

```go
func (h *Handler) login(c *gin.Context) {
	var dto LoginDto
	if !common.BindJSON(c, &dto) {
		return
	}
	u, err := h.svc.ValidateUser(dto.Email, dto.Password)
	if err != nil {
		common.RespondAppError(c, err)
		return
	}
	refreshToken, err := h.refresh.Issue(u.ID)
	if err != nil {
		common.RespondInternalError(c)
		return
	}
	h.respondWithTokens(c, u, refreshToken)
}
```

Add `refreshTokens`:

```go
// @Summary		Refresh tokens
// @Description	Exchange a refresh token for a new access token and a new refresh token (the old one stops working). Reusing an old refresh token ends the session.
// @Tags		auth
// @Accept		json
// @Produce		json
// @Param		body body RefreshDto true "Refresh token"
// @Success		200 {object} LoginResponse
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Failure		429 {object} common.ErrorResponse
// @Router		/auth/refresh [post]
func (h *Handler) refreshTokens(c *gin.Context) {
	var dto RefreshDto
	if !common.BindJSON(c, &dto) {
		return
	}
	userId, next, err := h.refresh.Rotate(dto.RefreshToken)
	if err != nil {
		common.RespondAppError(c, err)
		return
	}
	u, err := h.svc.FindUser(userId)
	if err != nil {
		common.RespondAppError(c, err)
		return
	}
	h.respondWithTokens(c, u, next)
}
```

In `logout`, after revoking the access token and before responding:

```go
	var dto LogoutDto
	_ = c.ShouldBindJSON(&dto) // body is optional
	if dto.RefreshToken != nil {
		if err := h.refresh.RevokeFamilyOf(*dto.RefreshToken, claims.Sub); err != nil {
			common.RespondInternalError(c)
			return
		}
	}
```

and add to its Swagger block: `// @Param		body body LogoutDto false "Refresh token to revoke"`.

Import `"ger_fin_api/internal/user"` in `handler.go`.

- [ ] **Step 6: Wire it** in `cmd/main.go`:

```go
	refreshStore       := auth.NewRefreshStore(db, cfg.JWT.RefreshTTL)
	authHandler        := auth.NewHandler(authSvc, tokenStore, refreshStore)
```

(`/auth/refresh` sits in the auth group, so the existing `RateLimit(20, time.Minute)` applies.)

- [ ] **Step 7: Run** `go test -p 1 ./internal/auth/ -v && go build ./...` → PASS (existing auth tests unchanged; `Login` still returns the access token string).

- [ ] **Step 8: Commit**

```bash
git add internal/auth cmd/main.go
git commit -m "feat(auth): issue, rotate and revoke refresh tokens"
```

---

### Task 13: Swagger and docs

**Files:**
- Regenerate: `docs/docs.go`, `docs/swagger.json`, `docs/swagger.yaml`
- Modify: `CLAUDE.md`

- [ ] **Step 1: Regenerate Swagger** — `make swagger`. If `swag` is missing: `go install github.com/swaggo/swag/cmd/swag@<version of github.com/swaggo/swag in go.mod>` then rerun.

- [ ] **Step 2: Check the output** — `rtk proxy grep -c '"/sync/changes"\|"/auth/refresh"' docs/swagger.json` → `2`; `rtk proxy grep -c '"clientModifiedAt"' docs/swagger.json` → non-zero.

- [ ] **Step 3: Update `CLAUDE.md`.** Add `| \`REFRESH_TOKEN_TTL\` | \`2160h\` | Sliding refresh-token lifetime; each refresh extends it |` to the security settings table, and this section after "Common Pitfalls":

```markdown
## Sync (offline-first clients)

- Synced tables (`account`, `category`, `transaction`, `budget`, `recurring_transaction`,
  `user_settings`) embed `common.SyncFields`. `seq` is set by a DB trigger — never write it.
- New synced writes must follow `internal/common/sync.go`: `CreateReplay` on create (client
  ids), `GoneIfDeleted` on update, `IgnoreIfDeleted` on delete, `IsStale` + `ClientTime`
  for `clientModifiedAt` (last-write-wins).
- Adding a synced table: add it to `syncedTables` in a new migration (seq column, index,
  trigger), embed `SyncFields`, and add it to `indexSQL`/`loadLive` in `internal/changes`.
- Default categories must only change in migrations/seeding: they take no per-user lock, so
  concurrent changes could be skipped by the change feed.
- `GET /sync/changes` and `POST /auth/refresh` are the client contract — see
  `docs/superpowers/specs/2026-10-07-sync-and-refresh-tokens-design.md`.
```

- [ ] **Step 4: Full verification** — `go vet ./... && go test -p 1 -count=1 ./...` → all PASS.

- [ ] **Step 5: Commit**

```bash
git add docs/docs.go docs/swagger.json docs/swagger.yaml CLAUDE.md
git commit -m "docs(api): document sync contract and refresh tokens"
```

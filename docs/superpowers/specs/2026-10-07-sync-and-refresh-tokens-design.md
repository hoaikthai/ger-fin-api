# Sync Contract and Refresh Tokens — Design

Date: 2026-10-07
Status: Approved in conversation, pending written-spec review
Client: `ger_fin_app_swift` (offline-first). The Flutter app is retired and is not a constraint.
Companion spec: `ger_fin_app_swift/docs/superpowers/specs/2026-10-07-offline-first-sync-design.md`

## 1. Goal

Let an offline-first mobile client keep a full local copy of a user's data, queue writes while
offline, and replay them safely later; and keep the user signed in the way mobile apps do.

Success:
- Replaying any queued create/update/delete any number of times yields the same server state
  as applying it once (no duplicate transactions, balances moved once).
- A client can fetch every change since its last sync — including server-generated rows
  (recurring transactions, imports) and deletions — with one cursor.
- A user who opens the app at least once every 90 days never re-enters their password.

### Out of scope

Batch push endpoint, field-level merge, real-time push (websockets/APNs), multi-tenant sharing,
changes to report endpoints.

## 2. Decisions

| Topic | Decision |
|---|---|
| Push | Existing REST endpoints, made idempotent and conflict-aware |
| Pull | One endpoint, `GET /sync/changes`, cursor = server sequence number |
| Conflicts | Last-write-wins per record on `clientModifiedAt`; **deletes always win** |
| Cursor | Global Postgres sequence (`seq`), never wall-clock time |
| Auth | Access JWT 1 h (unchanged) + rotating opaque refresh token, 90 days sliding, reuse detection |

## 3. Synced entities

| Table | Pull | Push via |
|---|---|---|
| `account` | yes | `POST/PATCH/DELETE /accounts` |
| `category` (user rows **and** defaults, `userId IS NULL`) | yes | `POST/PATCH/DELETE /categories` |
| `transaction` | yes | `POST/PATCH/DELETE /transactions`, `POST /transfers` |
| `budget` | yes | `/budgets` endpoints |
| `recurring_transaction` | yes | `/recurring-transactions` endpoints |
| `user_settings` | yes | `PATCH /users/settings` |
| exchange rates | no (read via `GET /currencies/rates`) | — |
| `user`, `revoked_token`, `refresh_token` | no | — |

All synced tables already have `deletedAt`; deletes stay soft deletes.

## 4. Schema changes (migration 012)

For each synced table:

- `"seq" bigint NOT NULL` — assigned from one global sequence `sync_seq` by a
  `BEFORE INSERT OR UPDATE` trigger, so every insert, update and soft delete (including
  scheduler, import and balance updates) gets a new, strictly larger value. A
  statement-level trigger first takes a global advisory lock (migration 014, see §6).
- `"clientModifiedAt" timestamptz` — nullable; set from client writes, used for last-write-wins.
- Index `("userId", "seq")` (for `category`, the index also serves `userId IS NULL`).
- Backfill: existing rows get `nextval('sync_seq')` in the migration.

Add `"updatedAt"`/`"deletedAt"` only where missing (none expected; verify during implementation).

## 5. Push semantics

All rules apply to every synced entity's create/update/delete endpoints.

### 5.1 Create

- Request body accepts an optional `id` (UUID). Absent → server generates, as today.
- If a row with that `id` already exists **for this user**: return the current row with the
  endpoint's normal success status (`201`), so a replay is indistinguishable from the first
  success. Soft-deleted → `410 Gone`. No side effects run again (balances move once).
- If the `id` exists for **another** user: `409 Conflict` (never leak the row).
- Body accepts optional `clientModifiedAt`; stored on the row.
- `POST /transfers` accepts `id` and `relatedId` for the two legs; idempotency is checked on `id`.
  A replay must name the same pair (the `id` row's `relatedTransactionId` equals `relatedId`),
  else `409`. A new transfer whose `relatedId` already exists (any user, live or deleted) → `409`.

### 5.2 Update (`PATCH`)

- Body accepts optional `clientModifiedAt`.
- Row soft-deleted → `410 Gone`.
- `clientModifiedAt` older than the stored value → no change, `200` with the current row.
- Otherwise apply as today (validation, balance adjustments) and store `clientModifiedAt`.
- Absent `clientModifiedAt` → apply unconditionally (backwards compatible).
- A `clientModifiedAt` in the future (device clock ahead) is clamped to server `now()` before
  comparing and storing, so one bad clock cannot make a record un-editable.

### 5.3 Delete

- Soft delete as today; balance reversal runs once.
- Already deleted → the endpoint's normal success response (`200` + message), no side effects.

### 5.4 Errors

Validation errors keep the existing `ErrorBody` shape and status codes (`400`/`404`/`422`),
so the client can show the server message for a failed queued change.

## 6. Pull: `GET /api/v1/sync/changes`

Query: `cursor` (int64, default `0`), `limit` (default `500`, max `1000`).

Returns rows with `seq > cursor`, ordered by `seq`, across all synced tables, scoped to the
user (plus default categories), up to `limit` rows total.

```json
{
  "changes": {
    "accounts": [ … ], "categories": [ … ], "transactions": [ … ],
    "budgets": [ … ], "recurringTransactions": [ … ], "userSettings": [ … ]
  },
  "deleted": [ { "entity": "transaction", "id": "…" } ],
  "cursor": 12345,
  "hasMore": false
}
```

- Rows are the raw model rows (the GORM model's JSON) including `seq` and
  `clientModifiedAt`, with relations as ids only (`accountId`, `categoryId`,
  `relatedTransactionId`, `parentId`) — no nested objects and no computed fields such as
  `baseAmount`; the client has the related rows and the rates locally.
- Soft-deleted rows appear only in `deleted`.
- `cursor` greater than the current `sync_seq` value (e.g. the server database was reset) →
  `409 Conflict`; the client discards its cursor and pulls from `0`.
- `cursor` = largest `seq` returned (or the request cursor when empty).
- Consistency: the query runs in one `REPEATABLE READ` transaction.
- **No skipped rows.** A `seq` allocated by a transaction that commits *after* a reader has
  already seen a larger `seq` would be skipped forever. To prevent this, every `INSERT` or
  `UPDATE` statement on a synced table runs a `BEFORE … FOR EACH STATEMENT` trigger
  (migration 014) that takes the global lock `pg_advisory_xact_lock(1, 0)`, held until the
  transaction ends. Writes to synced tables are serialized and all `seq` values become
  visible in commit order. Cost: concurrent writing transactions wait on each other, across
  all users — acceptable for a personal finance app with short write transactions.
- Why a statement-level, global lock: migration 012 originally took a per-user lock in the
  `FOR EACH ROW` trigger. Postgres locks the target row *before* row triggers run, so a
  transaction holding the user lock and later updating row X could wait on a statement that
  had already locked X and was waiting in its trigger for the user lock — a deadlock (e.g. a
  transaction create adjusting an account balance racing `PATCH /accounts/:id`). A
  statement-level `BEFORE` trigger fires before any row is scanned or locked, so a waiting
  writer holds no row lock and cannot be part of such a cycle. The row trigger now only
  assigns `seq`.
- Default categories (`userId IS NULL`) only change through migrations or seeding; they are
  covered by the same global lock.
- The implementation plan must include a concurrency test: writer A allocates `seq` and
  holds its transaction open, writer B for the same user commits, a pull runs, A commits,
  the next pull returns A's row.

## 7. Refresh tokens

### 7.1 Table `refresh_token` (migration 013)

`id uuid PK`, `userId uuid FK`, `familyId uuid`, `tokenHash bytea UNIQUE` (SHA-256 of the
opaque token), `expiresAt timestamptz`, `usedAt timestamptz NULL`, `revokedAt timestamptz NULL`,
`createdAt timestamptz`. Index on `familyId`.

Token value: 32 random bytes, base64url. Only the hash is stored.

### 7.2 Endpoints

- `POST /auth/login` → `{ "access_token": "…", "refresh_token": "…", "expires_in": 3600 }`
  and starts a new family. `POST /auth/register` is unchanged (client logs in afterwards).
- `POST /auth/refresh` body `{ "refresh_token": "…" }`:
  - unknown / expired / revoked → `401`.
  - already used within the last **60 s** (the client lost the previous response on a flaky
    network and retried) → issue another successor in the same family, no revocation.
  - already used earlier than that → revoke the whole family, `401` (reuse detection).
  - otherwise mark used, issue a new access token and a new refresh token in the same family
    with `expiresAt = now + 90 days` (sliding), return the same shape as login.
  - Rate-limited like `/auth/login`.
- `POST /auth/logout` additionally accepts `refresh_token` and revokes its family; the access
  token denylist behaviour is unchanged.

### 7.3 Configuration

`JWT_EXPIRES_IN` default stays `1h`. New `REFRESH_TOKEN_TTL` default `2160h` (90 days).
Expired/revoked rows are purged by the same mechanism that purges `revoked_token`.

## 8. Testing

Follow existing `*_test.go` and `*_realdb_test.go` patterns.

- Create with the same `id` twice → same row, `200`, account balance changed once.
- Create with an `id` owned by another user → `409`.
- Transfer with client ids, retried → one pair, balances moved once.
- Update with older `clientModifiedAt` → unchanged row returned; newer → applied.
- Update of a deleted row → `410`; delete twice → `200` both times, balance reversed once.
- `/sync/changes`: paging and `hasMore`; cursor strictly increases; default categories
  included; deletions in `deleted`; scheduler-created transactions appear; other users' rows
  never appear; concurrent writer test for the cursor rule in §6.
- Migration on a populated database backfills `seq` for every row.
- Refresh: rotation; reuse of an old token revokes the family; expired token rejected;
  logout revokes the family; sliding expiry extends `expiresAt`.

## 9. Rollout

All changes are additive (optional request fields, new endpoints, new columns), so the API
ships before the client. Swagger docs are regenerated with the new fields and endpoints.

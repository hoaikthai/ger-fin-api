# GerFin — Business Documentation

## What is GerFin?

GerFin is a personal finance management application. Users track income and expenses across multiple accounts, organize transactions by category, and automate recurring payments. The system enforces strict per-user data isolation — all data is private to the account that created it.

---

## Core Entities

### User

A registered account. Each user has an email (unique), hashed password, optional first/last name, and owns all other entities.

Authentication uses JWT (Bearer token). Each token carries a unique ID (`jti`); `POST /auth/logout` adds it to a denylist (`revoked_token`) until it expires. Token expiry is configured via `JWT_EXPIRES_IN`; `JWT_SECRET` must be at least 32 bytes.

### Account

A financial account belonging to a user (e.g. "Checking", "Cash", "Savings EUR"). Key fields:

| Field       | Description                              |
|-------------|------------------------------------------|
| name        | Display name                             |
| currency    | ISO 4217 code (e.g. `EUR`, `USD`)        |
| balance     | Running balance, numeric(10,2)           |
| description | Optional notes                           |
| isActive    | Soft-disable without deleting            |

Balance is maintained by the application — it is updated whenever a transaction is created, updated, or deleted on that account. It is **not** recomputed on the fly from transaction history.

### Category

Classifies a transaction as `income` or `expense`. Categories form a two-level hierarchy: a top-level category can have child categories (`parentId`).

Two kinds of categories exist:

- **Default categories** — seeded at startup (`isDefault: true`, `userId: null`). Visible to all users. Cannot be modified by users.
- **User categories** — created by a user (`userId` set). Private to that user.

Default categories are re-seeded each time the server starts (idempotent upsert).

### Transaction

A single financial event. Key fields:

| Field                  | Description                                                    |
|------------------------|----------------------------------------------------------------|
| type                   | `income` or `expense`                                          |
| amount                 | Positive decimal, numeric(15,2)                               |
| description            | Free-text note                                                 |
| transactionDate        | When the transaction occurred (not necessarily now)            |
| accountId              | Which account this affects                                     |
| categoryId             | Classification                                                 |
| relatedTransactionId   | Links two sides of a transfer between accounts                 |
| recurringTransactionId | Set when spawned by a recurring transaction template           |

**Transfers** between accounts are represented as two transactions: one expense on the source account and one income on the destination, linked by `relatedTransactionId`.

**Account balance** is updated atomically with each transaction create/update/delete.

### Recurring Transaction

A template that generates real transactions on a schedule. Key fields:

| Field       | Description                                          |
|-------------|------------------------------------------------------|
| frequency   | `daily`, `weekly`, `monthly`, `yearly`               |
| startDate   | When recurrence begins                               |
| endDate     | Optional — stops generating after this date          |
| nextRunAt   | Timestamp the scheduler uses to decide what to fire  |
| isActive    | Can be paused without deleting                       |

The scheduler runs via `robfig/cron` on server startup. Each tick it finds all active recurring transactions where `nextRunAt <= now`, creates the corresponding transaction, and advances `nextRunAt` to the next scheduled time.

Generated transactions have `recurringTransactionId` set so they can be traced back to their template.

---

## Business Rules

### Authentication & Authorization

- All endpoints except `POST /auth/register` and `POST /auth/login` require a valid Bearer token.
- Every query is scoped to the authenticated user's ID (`claims.Sub`). A user cannot read or modify another user's data even with a valid token.

### Account Balance

- Creating an `income` transaction increases the account balance by `amount`.
- Creating an `expense` transaction decreases the account balance by `amount`.
- Updating a transaction recalculates the balance delta (reverses the old effect, applies the new one).
- Deleting a transaction reverses its balance effect before soft-deleting.
- Transactions created before balance tracking (migration 009, `balanceApplied = false`) never moved the balance, so editing or deleting them leaves the balance unchanged.
- An account's currency cannot be changed once it has transactions.

### Transfers

Creating a transfer requires specifying source and destination accounts. The API creates two transactions atomically and links them. Deleting one side of a transfer deletes both.

### Soft Deletes

All entities use soft deletes (`deletedAt` column). Deleted records are excluded from all queries automatically by GORM. There is no hard-delete endpoint.

### Category Seeding

On startup, a fixed set of default categories (income and expense) is upserted. Users cannot modify default categories but can create their own.

---

## API Surface

Base URL: `http://localhost:3000/api/v1` (`/health` and `/swagger/*` stay at root)  
Swagger UI: `http://localhost:3000/swagger/index.html`

### Auth

| Method | Path               | Description          |
|--------|--------------------|----------------------|
| POST   | /auth/register     | Register new user (same 201 response whether or not the email exists) |
| POST   | /auth/login        | Login, receive JWT   |
| POST   | /auth/logout       | Revoke current token |

`/auth/*` is rate-limited to 20 requests per minute per client IP.

### User

| Method | Path       | Description              |
|--------|------------|--------------------------|
| GET    | /users/me  | Get current user profile |
| PATCH  | /users/me  | Update profile           |

### Accounts

| Method | Path           | Description          |
|--------|----------------|----------------------|
| POST   | /accounts      | Create account       |
| GET    | /accounts      | List user's accounts |
| GET    | /accounts/:id  | Get single account   |
| PATCH  | /accounts/:id  | Update account       |
| DELETE | /accounts/:id  | Soft-delete account  |

### Categories

| Method | Path              | Description            |
|--------|-------------------|------------------------|
| POST   | /categories       | Create user category   |
| GET    | /categories       | List all categories    |
| GET    | /categories/:id   | Get single category    |
| PATCH  | /categories/:id   | Update category        |
| DELETE | /categories/:id   | Soft-delete category   |

### Transactions

| Method | Path                   | Description                          |
|--------|------------------------|--------------------------------------|
| POST   | /transactions          | Create transaction                   |
| GET    | /transactions          | List transactions (filterable)       |
| GET    | /transactions/:id      | Get single transaction               |
| PATCH  | /transactions/:id      | Update transaction                   |
| DELETE | /transactions/:id      | Soft-delete transaction              |
| GET    | /transactions/summary  | Aggregate totals by period           |
| GET    | /transactions/export   | Export as CSV                        |

Query filters on `GET /transactions`: `period` (day/week/month/quarter/year), `offset` (relative period shift), `type` (income/expense), `categoryId`, `accountId`.

### Recurring Transactions

| Method | Path                         | Description                    |
|--------|------------------------------|--------------------------------|
| POST   | /recurring-transactions      | Create recurring template      |
| GET    | /recurring-transactions      | List user's recurring templates|
| GET    | /recurring-transactions/:id  | Get single template            |
| PATCH  | /recurring-transactions/:id  | Update template                |
| DELETE | /recurring-transactions/:id  | Soft-delete template           |

---

## Data Model (ERD summary)

```
user
 └── account (userId)
 └── category (userId, nullable — null = default)
      └── category (parentId, self-ref)
 └── transaction (userId)
      ├── account (accountId)
      ├── category (categoryId)
      ├── transaction (relatedTransactionId, self-ref, nullable)
      └── recurring_transaction (recurringTransactionId, nullable)
 └── recurring_transaction (userId)
      ├── account (accountId)
      └── category (categoryId)
```

All PKs are UUID v4 (Postgres `uuid_generate_v4()`). All foreign key deletes cascade.

---

## Infrastructure

| Component  | Technology              |
|------------|-------------------------|
| Language   | Go 1.23                 |
| HTTP       | Gin                     |
| ORM        | GORM                    |
| Database   | PostgreSQL 15           |
| Auth       | JWT (golang-jwt/jwt v5) |
| Scheduler  | robfig/cron v3          |
| Config     | Viper (env vars)        |
| Docs       | Swaggo / Swagger UI     |
| Container  | Docker Compose          |

Postgres runs on port **5433** locally (mapped from container's 5432) to avoid conflicts with a local Postgres install.

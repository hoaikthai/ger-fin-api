# GerFin API — Go Coding Standards

## Project Overview

Personal finance REST API. Go 1.23, Gin, GORM, PostgreSQL, JWT auth, Swagger, Cron scheduler.
Module: `ger_fin_api` — entry point: `cmd/main.go`.

---

## Architecture

```
cmd/main.go                  # wires everything: config → db → migrations → services → handlers → router
internal/
  <domain>/
    model.go                 # GORM struct + TableName()
    dto.go                   # request/response shapes
    service.go               # business logic, talks to db
    handler.go               # HTTP layer, delegates to service
    service_test.go          # unit tests with sqlmock
  common/                    # shared: BaseModel, AppError, enums, jwt helpers, bind helpers
  config/                    # Viper config
  database/                  # GORM connection
  middleware/                # JWT auth middleware
  migrations/                # versioned SQL migrations
```

One package per domain. No cross-domain service calls — only cross-domain model imports (e.g. transaction imports account and category for Preload).

---

## Column Naming — CRITICAL

All DB columns use **camelCase**, not snake_case. This matches the original Node.js schema and cannot change without a migration.

```go
// CORRECT
ID        string `gorm:"column:id"`
CreatedAt time.Time `gorm:"column:createdAt"`
UserId    string `gorm:"column:userId"`

// WRONG — do not do this
UserId    string `gorm:"column:user_id"`
```

GORM's default snake_case conversion is overridden by always providing explicit `column:` tags. Every field in every model must have an explicit `column:` tag.

---

## Models

Embed `common.BaseModel` for all entities. It provides `id` (UUID, auto-gen), `createdAt`, `updatedAt`, `deletedAt` (soft delete).

```go
type Account struct {
    common.BaseModel
    Name   string `gorm:"column:name;not null" json:"name"`
    UserId string `gorm:"column:userId;type:uuid;not null" json:"userId"`
}

func (Account) TableName() string { return "account" }
```

Table names are singular and quoted by `TableName()`. Always implement `TableName()`.

Soft deletes are automatic via `gorm.DeletedAt` in `BaseModel` — never use hard deletes.

---

## Error Handling

Use `common.AppError` for all domain errors. Never return raw `error` strings to the HTTP layer.

```go
// In service
if result.RowsAffected == 0 {
    return nil, common.NewNotFound("account not found")
}

// In handler
if err != nil {
    var appErr *common.AppError
    if errors.As(err, &appErr) {
        common.RespondError(c, appErr)
        return
    }
    common.RespondInternalError(c)
    return
}
```

Available constructors: `NewBadRequest`, `NewUnauthorized`, `NewForbidden`, `NewNotFound`, `NewConflict`.

Never expose internal error messages. `RespondInternalError` sends a generic 500.

---

## Handler Pattern

Handlers own only: extract claims, bind DTO, call service, respond. No business logic.

```go
func (h *Handler) create(c *gin.Context) {
    claims := middleware.CurrentUser(c)   // always first for authenticated routes

    var dto CreateFooDto
    if !common.BindJSON(c, &dto) {        // handles 400 response internally
        return
    }

    result, err := h.svc.Create(claims.Sub, dto)
    if err != nil {
        var appErr *common.AppError
        if errors.As(err, &appErr) {
            common.RespondError(c, appErr)
            return
        }
        common.RespondInternalError(c)
        return
    }

    c.JSON(201, result)
}
```

Register routes in `RegisterRoutes(rg *gin.RouterGroup, auth gin.HandlerFunc)`.

---

## Swagger Annotations

Every handler func must have Swagger annotations. Follow the existing format:

```go
// @Summary     Create foo
// @Description Create a new foo for the authenticated user
// @Tags        foos
// @Accept      json
// @Produce     json
// @Param       body body CreateFooDto true "Foo details"
// @Success     201 {object} Foo
// @Failure     400 {object} common.ErrorResponse
// @Failure     401 {object} common.ErrorResponse
// @Security    BearerAuth
// @Router      /foos [post]
func (h *Handler) create(c *gin.Context) {
```

After adding or modifying annotations, regenerate docs:

```bash
swag init -g cmd/main.go
```

---

## DTOs

Use pointer fields for optional/patch fields. Use `validate` tags for required fields.

```go
type CreateFooDto struct {
    Name string `json:"name" binding:"required"`
}

type UpdateFooDto struct {
    Name *string `json:"name"`
}
```

`common.BindJSON` calls `c.ShouldBindJSON` and writes the 400 response automatically — just return if it returns false.

---

## Service Pattern

Services take `*gorm.DB` directly. No repository layer.

```go
type Service struct {
    db *gorm.DB
}

func NewService(db *gorm.DB) *Service {
    return &Service{db: db}
}
```

Scope all queries to `userId` — never return data from other users.

```go
func (s *Service) FindOne(id, userId string) (*Foo, error) {
    var f Foo
    err := s.db.Where("id = ? AND \"userId\" = ?", id, userId).First(&f).Error
    if errors.Is(err, gorm.ErrRecordNotFound) {
        return nil, common.NewNotFound("foo not found")
    }
    return &f, err
}
```

---

## Migrations

Each migration is a `Migration` struct with a `Version int`, `Name string`, and `Up func(*gorm.DB) error`. Add new files as `NNN_description.go` in `internal/migrations/` and register in `runner.go`.

Write raw SQL in migrations — do not use GORM AutoMigrate. Migrations are applied once and tracked in the `migrations` table.

---

## Testing

Use `github.com/DATA-DOG/go-sqlmock` for service tests. Tests live alongside the package they test (`service_test.go`).

```go
func TestCreate(t *testing.T) {
    db, mock, _ := sqlmock.NewWithDSN(...)
    svc := NewService(db)
    // set up mock expectations, call svc, assert
}
```

Run tests:

```bash
go test ./...
```

---

## Environment & Local Dev

Copy `.env.example` to `.env`. Start Postgres:

```bash
docker compose up -d
```

Run the API:

```bash
go run cmd/main.go
```

API routes are served under `/api/v1` (e.g. `POST /api/v1/auth/login`); `/health` and `/swagger/*` stay at root.

Swagger UI: `http://localhost:3000/swagger/index.html`

Config is loaded via Viper from environment variables (see `internal/config/config.go`).

Security-related settings:

| Variable               | Default            | Notes                                                                 |
|------------------------|--------------------|-----------------------------------------------------------------------|
| `JWT_SECRET`           | — (required)       | Must be ≥ 32 bytes or startup fails. `openssl rand -base64 48`        |
| `DB_SSLMODE`           | `disable`          | Use `require` (or stricter) when Postgres is on another host          |
| `CORS_ALLOWED_ORIGINS` | allow all          | Comma-separated origin allow-list                                     |
| `TRUSTED_PROXIES`      | trust none         | Comma-separated IPs/CIDRs of reverse proxies; needed for correct client IPs (auth rate limit) behind a proxy |

Real-DB tests (`TestRealDB_*`) skip when Postgres is unreachable. Point them at any Postgres via env vars, e.g.
`DB_HOST=localhost DB_PORT=55432 DB_USER=postgres DB_PASS=… DB_NAME=fin_db go test -p 1 ./...`

---

## Common Pitfalls

- Forgetting `column:` tags → GORM silently uses snake_case, breaking queries against the camelCase schema.
- Missing `TableName()` → GORM pluralizes and lowercases, producing wrong table names.
- Returning non-`AppError` errors from service to handler → caller gets a 500 instead of the correct status code.
- Querying without scoping to `userId` → data leak across users.
- Hard-deleting instead of soft-deleting → bypasses GORM's `DeletedAt` filter and data is unrecoverable.
- `db.Save(x)` on a struct with preloaded belongs-to associations (e.g. `Account`, `Category`, `Parent`) copies the association's ID back onto the foreign key — an update to `accountId` is silently reverted. Use `db.Omit(clause.Associations).Save(x)` and reload.
- Preloading `Children` of shared default categories must be filtered by `userId`, or other users' categories leak.

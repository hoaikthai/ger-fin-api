package testutil

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"ger_fin_api/internal/database"
	"ger_fin_api/internal/migrations"
	"github.com/spf13/viper"
	"gorm.io/gorm"
)

var (
	sharedDB  *gorm.DB
	setupOnce sync.Once
	setupErr  error
)

// NewRealDB returns a *gorm.DB backed by the real test database.
// Setup (connect + migrate) runs once per test binary. Each call wraps
// the connection in a transaction that rolls back when the test ends,
// keeping tests isolated without truncating tables.
// If the database is unreachable the test is skipped, not failed.
func NewRealDB(t *testing.T) *gorm.DB {
	t.Helper()

	setupOnce.Do(func() {
		p, err := testDBParams()
		if err != nil {
			setupErr = err
			return
		}
		if err := createDBIfNotExists(p); err != nil {
			setupErr = fmt.Errorf("create test db: %w", err)
			return
		}
		db, err := database.Connect(p.dsn())
		if err != nil {
			setupErr = fmt.Errorf("connect to test db: %w", err)
			return
		}
		if err := migrations.Run(db); err != nil {
			setupErr = fmt.Errorf("migrate test db: %w", err)
			return
		}
		sharedDB = db
	})

	if setupErr != nil {
		t.Skipf("real db unavailable, skipping: %v", setupErr)
	}

	tx := sharedDB.Begin()
	t.Cleanup(func() { tx.Rollback() })
	return tx
}

type dbParams struct {
	host, user, pass, name string
	port                   int
}

func (p dbParams) dsn() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		p.host, p.port, p.user, p.pass, p.name,
	)
}

func testDBParams() (dbParams, error) {
	// runtime.Caller gives us the source path at compile time so we can
	// navigate to the project root to load .env regardless of where the
	// test binary's working directory is.
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")

	v := viper.New()
	v.SetConfigFile(filepath.Join(root, ".env"))
	v.SetConfigType("env")
	v.AutomaticEnv()
	_ = v.ReadInConfig() // fine if absent — fall through to real env vars

	host := v.GetString("DB_HOST")
	if host == "" {
		host = "localhost"
	}
	port := v.GetInt("DB_PORT")
	if port == 0 {
		port = 5432
	}

	name := v.GetString("TEST_DB_NAME")
	if name == "" {
		base := v.GetString("DB_NAME")
		if base == "" {
			return dbParams{}, fmt.Errorf("DB_NAME not set")
		}
		name = base + "_test"
	}

	return dbParams{
		host: host,
		port: port,
		user: v.GetString("DB_USER"),
		pass: v.GetString("DB_PASS"),
		name: name,
	}, nil
}

func createDBIfNotExists(p dbParams) error {
	sysDSN := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=postgres sslmode=disable",
		p.host, p.port, p.user, p.pass,
	)
	sys, err := database.Connect(sysDSN)
	if err != nil {
		return fmt.Errorf("connect to postgres system db: %w", err)
	}
	sqlDB, _ := sys.DB()
	defer sqlDB.Close()

	var count int64
	sys.Raw("SELECT COUNT(*) FROM pg_database WHERE datname = ?", p.name).Scan(&count)
	if count == 0 {
		if err := sys.Exec(fmt.Sprintf(`CREATE DATABASE "%s"`, p.name)).Error; err != nil {
			return err
		}
	}
	return nil
}

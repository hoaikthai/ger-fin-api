package migrations

import (
	"fmt"
	"log"

	"gorm.io/gorm"
)

type Migration struct {
	Version int
	Name    string
	Up      func(db *gorm.DB) error
}

var all = []Migration{
	migration001(),
	migration002(),
	migration003(),
	migration004(),
	migration005(),
	migration006(),
	migration007(),
	migration008(),
	migration009(),
	migration010(),
	migration011(),
	migration012(),
}

func Run(db *gorm.DB) error {
	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version     INTEGER PRIMARY KEY,
			name        VARCHAR(255) NOT NULL,
			applied_at  TIMESTAMP   NOT NULL DEFAULT NOW()
		)
	`).Error; err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	var applied []int
	if err := db.Raw("SELECT version FROM schema_migrations ORDER BY version").Scan(&applied).Error; err != nil {
		return fmt.Errorf("query applied migrations: %w", err)
	}

	appliedSet := make(map[int]bool, len(applied))
	for _, v := range applied {
		appliedSet[v] = true
	}

	for _, m := range all {
		if appliedSet[m.Version] {
			continue
		}

		log.Printf("running migration %03d: %s", m.Version, m.Name)

		if err := m.Up(db); err != nil {
			return fmt.Errorf("migration %03d (%s): %w", m.Version, m.Name, err)
		}

		if err := db.Exec(
			"INSERT INTO schema_migrations (version, name) VALUES (?, ?)",
			m.Version, m.Name,
		).Error; err != nil {
			return fmt.Errorf("record migration %03d: %w", m.Version, err)
		}

		log.Printf("migration %03d applied", m.Version)
	}

	return nil
}

// All returns the registered migrations in order (used by tests).
func All() []Migration { return all }

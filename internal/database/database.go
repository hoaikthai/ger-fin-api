package database

import (
	"unicode"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// camelNamingStrategy converts Go PascalCase field names to JavaScript-style
// camelCase column names to match the existing TypeORM schema.
type camelNamingStrategy struct {
	schema.NamingStrategy
}

func (n camelNamingStrategy) ColumnName(table, column string) string {
	if len(column) == 0 {
		return column
	}
	runes := []rune(column)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}

func Connect(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		NamingStrategy: camelNamingStrategy{
			NamingStrategy: schema.NamingStrategy{
				SingularTable: true,
			},
		},
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}
	return db, nil
}

package testutil

import (
	"unicode"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

type camelNamingStrategy struct {
	schema.NamingStrategy
}

func (n camelNamingStrategy) ColumnName(table, column string) string {
	runes := []rune(column)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}

// NewMockDB returns a GORM DB backed by sqlmock, using the same camelCase naming
// strategy as the production database connection.
func NewMockDB() (*gorm.DB, sqlmock.Sqlmock, error) {
	db, mock, err := sqlmock.New()
	if err != nil {
		return nil, nil, err
	}

	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{
		NamingStrategy: camelNamingStrategy{
			NamingStrategy: schema.NamingStrategy{SingularTable: true},
		},
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, nil, err
	}

	return gormDB, mock, nil
}

// UserCols returns the ordered column list for User rows in sqlmock.
func UserCols() []string {
	return []string{"id", "createdAt", "updatedAt", "deletedAt", "email", "password", "firstName", "lastName"}
}

// AccountCols returns the ordered column list for Account rows in sqlmock.
func AccountCols() []string {
	return []string{"id", "createdAt", "updatedAt", "deletedAt", "name", "currency", "balance", "description", "isActive", "userId"}
}

// CategoryCols returns the ordered column list for Category rows in sqlmock.
func CategoryCols() []string {
	return []string{"id", "createdAt", "updatedAt", "deletedAt", "name", "type", "isDefault", "userId", "parentId"}
}

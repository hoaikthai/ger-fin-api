package budget

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"ger_fin_api/internal/common"
)

// CurrencyConverter converts an amount between currencies (satisfied by *currency.Service).
type CurrencyConverter interface {
	Convert(amount float64, from, to string) (float64, error)
}

// BaseCurrencyProvider resolves a user's base currency (satisfied by *user.Service).
type BaseCurrencyProvider interface {
	GetBaseCurrency(userId string) (string, error)
}

type Service struct {
	db       *gorm.DB
	conv     CurrencyConverter
	settings BaseCurrencyProvider
}

func NewService(db *gorm.DB, conv CurrencyConverter, settings BaseCurrencyProvider) *Service {
	return &Service{db: db, conv: conv, settings: settings}
}

func (s *Service) Create(dto CreateBudgetDto, userId string) (*Budget, error) {
	if err := s.validateCategory(dto.CategoryId, userId); err != nil {
		return nil, err
	}

	start, end := periodBounds(string(dto.PeriodType), time.Now().UTC())

	if err := s.assertNoConflict(userId, dto.CategoryId, string(dto.PeriodType), start); err != nil {
		return nil, err
	}

	baseCurrency, err := s.settings.GetBaseCurrency(userId)
	if err != nil || baseCurrency == "" {
		baseCurrency = "USD"
	}

	rollover := false
	if dto.Rollover != nil {
		rollover = *dto.Rollover
	}

	b := &Budget{
		SeriesId:    uuid.New().String(),
		CategoryId:  dto.CategoryId,
		UserId:      userId,
		PeriodType:  string(dto.PeriodType),
		PeriodStart: start,
		PeriodEnd:   end,
		Amount:      dto.Amount,
		Currency:    baseCurrency,
		CarriedOver: 0,
		Rollover:    rollover,
		IsActive:    true,
	}
	if err := s.db.Create(b).Error; err != nil {
		return nil, err
	}
	return b, nil
}

// validateCategory ensures a non-nil categoryId refers to an expense category
// owned by the user (or a default category). nil categoryId (overall budget) is allowed.
func (s *Service) validateCategory(categoryId *string, userId string) error {
	if categoryId == nil {
		return nil
	}
	var row struct {
		Type      string  `gorm:"column:type"`
		IsDefault bool    `gorm:"column:isDefault"`
		UserId    *string `gorm:"column:userId"`
	}
	err := s.db.Raw(`
		SELECT "type", "isDefault", "userId"
		FROM "category" WHERE "id" = ? AND "deletedAt" IS NULL
	`, *categoryId).Scan(&row).Error
	if err != nil {
		return err
	}
	if row.Type == "" {
		return common.NewNotFound("Category not found")
	}
	if !row.IsDefault && (row.UserId == nil || *row.UserId != userId) {
		return common.NewForbidden("Access denied to this category")
	}
	if row.Type != string(common.TransactionTypeExpense) {
		return common.NewBadRequest("Budgets can only target expense categories")
	}
	return nil
}

func (s *Service) assertNoConflict(userId string, categoryId *string, periodType string, start time.Time) error {
	q := s.db.Model(&Budget{}).
		Where(`"userId" = ? AND "periodType" = ? AND "periodStart" = ?`, userId, periodType, start)
	if categoryId == nil {
		q = q.Where(`"categoryId" IS NULL`)
	} else {
		q = q.Where(`"categoryId" = ?`, *categoryId)
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return common.NewConflict("A budget already exists for this category and period")
	}
	return nil
}

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation (SQLState 23505) — a backstop for the partial unique indexes
// when two requests race.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

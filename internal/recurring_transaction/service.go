package recurring_transaction

import (
	"errors"
	"fmt"
	"log"
	"time"

	"ger_fin_api/internal/account"
	"ger_fin_api/internal/category"
	"ger_fin_api/internal/common"
	txPkg "ger_fin_api/internal/transaction"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// maxCatchUpPerRun bounds how many overdue occurrences a single scheduler run
// generates for one template; any remainder is picked up by the next run.
const maxCatchUpPerRun = 366

type Service struct {
	db             *gorm.DB
	transactionSvc *txPkg.Service
}

func NewService(db *gorm.DB, transactionSvc *txPkg.Service) *Service {
	return &Service{db: db, transactionSvc: transactionSvc}
}

func (s *Service) withRelations(q *gorm.DB) *gorm.DB {
	return q.Preload("Account").Preload("Category")
}

// validateRefs ensures the account belongs to the user and the category is the
// user's own (or a default) with a type matching txType.
func (s *Service) validateRefs(accountId, categoryId string, txType common.TransactionType, userId string) error {
	var acc account.Account
	if err := s.db.Where("id = ? AND \"userId\" = ?", accountId, userId).First(&acc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.NewNotFound("Account not found")
		}
		return err
	}

	var cat category.Category
	if err := s.db.First(&cat, "id = ?", categoryId).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.NewNotFound("Category not found")
		}
		return err
	}
	if !cat.IsDefault && (cat.UserId == nil || *cat.UserId != userId) {
		return common.NewNotFound("Category not found")
	}
	if cat.Type != string(txType) {
		return common.NewBadRequest("Category type must match transaction type")
	}
	return nil
}

func (s *Service) Create(dto CreateRecurringTransactionDto, userId string) (*RecurringTransaction, error) {
	if err := s.validateRefs(dto.AccountId, dto.CategoryId, dto.Type, userId); err != nil {
		return nil, err
	}

	nextDueDate := dto.StartDate

	isActive := true
	if dto.IsActive != nil {
		isActive = *dto.IsActive
	}

	rt := &RecurringTransaction{
		Type:        string(dto.Type),
		Amount:      dto.Amount,
		Description: dto.Description,
		CategoryId:  dto.CategoryId,
		UserId:      userId,
		AccountId:   dto.AccountId,
		Frequency:   string(dto.Frequency),
		StartDate:   dto.StartDate,
		EndDate:     dto.EndDate,
		NextDueDate: &nextDueDate,
		IsActive:    isActive,
	}

	if err := s.db.Create(rt).Error; err != nil {
		return nil, err
	}
	return s.FindOne(rt.ID, userId)
}

func (s *Service) FindAll(userId string) ([]RecurringTransaction, error) {
	var rts []RecurringTransaction
	err := s.withRelations(s.db).
		Where("\"userId\" = ?", userId).
		Order("\"createdAt\" DESC").
		Find(&rts).Error
	return rts, err
}

func (s *Service) FindOne(id, userId string) (*RecurringTransaction, error) {
	var rt RecurringTransaction
	err := s.withRelations(s.db).
		Where("id = ? AND \"userId\" = ?", id, userId).
		First(&rt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, common.NewNotFound("Recurring transaction not found")
	}
	return &rt, err
}

func (s *Service) Update(id string, dto UpdateRecurringTransactionDto, userId string) (*RecurringTransaction, error) {
	rt, err := s.FindOne(id, userId)
	if err != nil {
		return nil, err
	}

	if dto.Type != nil || dto.AccountId != nil || dto.CategoryId != nil {
		finalType := common.TransactionType(rt.Type)
		if dto.Type != nil {
			finalType = *dto.Type
		}
		finalAccountId := rt.AccountId
		if dto.AccountId != nil {
			finalAccountId = *dto.AccountId
		}
		finalCategoryId := rt.CategoryId
		if dto.CategoryId != nil {
			finalCategoryId = *dto.CategoryId
		}
		if err := s.validateRefs(finalAccountId, finalCategoryId, finalType, userId); err != nil {
			return nil, err
		}
	}

	if dto.Type != nil {
		rt.Type = string(*dto.Type)
	}
	if dto.Amount != nil {
		rt.Amount = *dto.Amount
	}
	if dto.Description != nil {
		rt.Description = *dto.Description
	}
	if dto.CategoryId != nil {
		rt.CategoryId = *dto.CategoryId
	}
	if dto.AccountId != nil {
		rt.AccountId = *dto.AccountId
	}
	if dto.Frequency != nil {
		rt.Frequency = string(*dto.Frequency)
	}
	if dto.StartDate != nil {
		rt.StartDate = *dto.StartDate
		// Restart the schedule from the new start date.
		nextDue := *dto.StartDate
		rt.NextDueDate = &nextDue
	}
	if dto.EndDate != nil {
		rt.EndDate = dto.EndDate
	}
	if dto.IsActive != nil {
		rt.IsActive = *dto.IsActive
	}

	// Omit associations: stale preloaded Account/Category would overwrite the
	// foreign keys with their old IDs.
	if err := s.db.Omit(clause.Associations).Save(rt).Error; err != nil {
		return nil, err
	}
	return s.FindOne(id, userId)
}

func (s *Service) Remove(id, userId string) error {
	rt, err := s.FindOne(id, userId)
	if err != nil {
		return err
	}
	return s.db.Delete(rt).Error
}

func (s *Service) ProcessDueRecurringTransactions() error {
	now := time.Now()

	var rts []RecurringTransaction
	if err := s.db.Where("\"isActive\" = ? AND \"nextDueDate\" <= ?", true, now).Find(&rts).Error; err != nil {
		return fmt.Errorf("failed to query due recurring transactions: %w", err)
	}

	// Date columns are "timestamp" (no time zone): the driver stores local wall-clock
	// time and reads it back labelled UTC. Compare against now in that same frame.
	nowWall := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second(), now.Nanosecond(), time.UTC)
	for _, rt := range rts {
		if err := s.processOne(rt, nowWall); err != nil {
			log.Printf("failed to process recurring transaction %s: %v", rt.ID, err)
		}
	}
	return nil
}

// processOne generates every occurrence due up to now (bounded by
// maxCatchUpPerRun), dated on its due date, and advances nextDueDate in the
// same DB transaction so a failure cannot produce duplicates on the next run.
func (s *Service) processOne(rt RecurringTransaction, now time.Time) error {
	if rt.NextDueDate == nil {
		return fmt.Errorf("recurring transaction %s has nil nextDueDate", rt.ID)
	}

	amount := rt.Amount
	if rt.Type == string(common.TransactionTypeExpense) {
		amount = -amount
	}
	frequency := common.RecurrenceFrequency(rt.Frequency)
	anchorDay := rt.StartDate.Day()

	return s.db.Transaction(func(db *gorm.DB) error {
		txSvc := txPkg.NewService(db)
		due := *rt.NextDueDate
		for i := 0; !due.After(now) && i < maxCatchUpPerRun; i++ {
			if rt.EndDate != nil && due.After(*rt.EndDate) {
				return db.Model(&rt).Updates(map[string]interface{}{"isActive": false, "nextDueDate": due}).Error
			}

			txDate := due
			dto := txPkg.CreateTransactionDto{
				Type:            common.TransactionType(rt.Type),
				Amount:          amount,
				Description:     fmt.Sprintf("%s (Auto-generated)", rt.Description),
				CategoryId:      rt.CategoryId,
				AccountId:       rt.AccountId,
				TransactionDate: &txDate,
			}
			if _, err := txSvc.Create(dto, rt.UserId); err != nil {
				return fmt.Errorf("failed to create auto-generated transaction: %w", err)
			}
			due = calculateNextDueDate(due, frequency, anchorDay)
		}
		if rt.EndDate != nil && due.After(*rt.EndDate) {
			return db.Model(&rt).Updates(map[string]interface{}{"isActive": false, "nextDueDate": due}).Error
		}
		return db.Model(&rt).Update("nextDueDate", due).Error
	})
}

// calculateNextDueDate advances from by one period. Monthly and yearly steps
// aim for anchorDay (the start date's day) and clamp to the month's last day,
// so a schedule starting on Jan 31 runs Feb 28, Mar 31, ... instead of drifting.
func calculateNextDueDate(from time.Time, frequency common.RecurrenceFrequency, anchorDay int) time.Time {
	switch frequency {
	case common.RecurrenceFrequencyDaily:
		return from.AddDate(0, 0, 1)
	case common.RecurrenceFrequencyWeekly:
		return from.AddDate(0, 0, 7)
	case common.RecurrenceFrequencyYearly:
		return addMonthsClamped(from, 12, anchorDay)
	default: // monthly
		return addMonthsClamped(from, 1, anchorDay)
	}
}

func addMonthsClamped(from time.Time, months, anchorDay int) time.Time {
	first := time.Date(from.Year(), from.Month(), 1, from.Hour(), from.Minute(), from.Second(), from.Nanosecond(), from.Location())
	target := first.AddDate(0, months, 0)
	lastDay := target.AddDate(0, 1, -1).Day()
	day := anchorDay
	if day > lastDay {
		day = lastDay
	}
	return target.AddDate(0, 0, day-1)
}

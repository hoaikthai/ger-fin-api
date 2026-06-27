package recurring_transaction

import (
	"fmt"
	"log"
	"time"

	"ger_fin_api/internal/common"
	txPkg "ger_fin_api/internal/transaction"

	"gorm.io/gorm"
)

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

func (s *Service) Create(dto CreateRecurringTransactionDto, userId string) (*RecurringTransaction, error) {
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
	return rt, nil
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
	if err == gorm.ErrRecordNotFound {
		return nil, common.NewNotFound("Recurring transaction not found")
	}
	return &rt, err
}

func (s *Service) Update(id string, dto UpdateRecurringTransactionDto, userId string) (*RecurringTransaction, error) {
	rt, err := s.FindOne(id, userId)
	if err != nil {
		return nil, err
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
	}
	if dto.EndDate != nil {
		rt.EndDate = dto.EndDate
	}
	if dto.IsActive != nil {
		rt.IsActive = *dto.IsActive
	}

	if err := s.db.Save(rt).Error; err != nil {
		return nil, err
	}
	return rt, nil
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

	for _, rt := range rts {
		if err := s.processOne(rt, now); err != nil {
			log.Printf("failed to process recurring transaction %s: %v", rt.ID, err)
		}
	}
	return nil
}

func (s *Service) processOne(rt RecurringTransaction, now time.Time) error {
	if rt.EndDate != nil && now.After(*rt.EndDate) {
		return s.db.Model(&rt).Update("isActive", false).Error
	}

	amount := rt.Amount
	if rt.Type == string(common.TransactionTypeExpense) {
		amount = -amount
	}

	dto := txPkg.CreateTransactionDto{
		Type:        common.TransactionType(rt.Type),
		Amount:      amount,
		Description: fmt.Sprintf("%s (Auto-generated)", rt.Description),
		CategoryId:  rt.CategoryId,
		AccountId:   rt.AccountId,
	}

	if _, err := s.transactionSvc.Create(dto, rt.UserId); err != nil {
		return fmt.Errorf("failed to create auto-generated transaction: %w", err)
	}

	nextDue := calculateNextDueDate(*rt.NextDueDate, common.RecurrenceFrequency(rt.Frequency))
	return s.db.Model(&rt).Update("nextDueDate", nextDue).Error
}

func calculateNextDueDate(from time.Time, frequency common.RecurrenceFrequency) time.Time {
	switch frequency {
	case common.RecurrenceFrequencyDaily:
		return from.AddDate(0, 0, 1)
	case common.RecurrenceFrequencyWeekly:
		return from.AddDate(0, 0, 7)
	case common.RecurrenceFrequencyMonthly:
		return from.AddDate(0, 1, 0)
	case common.RecurrenceFrequencyYearly:
		return from.AddDate(1, 0, 0)
	default:
		return from.AddDate(0, 1, 0)
	}
}

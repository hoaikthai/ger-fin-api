package account

import (
	"errors"

	"ger_fin_api/internal/common"

	"gorm.io/gorm"
)

type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

func (s *Service) Create(userId string, dto CreateAccountDto) (*Account, error) {
	if replay, err := common.CreateReplay(s.db, "account", dto.ID, userId); err != nil {
		return nil, err
	} else if replay {
		return s.FindOne(*dto.ID, userId)
	}
	a := &Account{
		Name:        dto.Name,
		Currency:    dto.Currency,
		Balance:     dto.Balance,
		Description: dto.Description,
		UserId:      userId,
	}
	a.ClientModifiedAt = common.ClientTime(dto.ClientModifiedAt)
	if dto.ID != nil {
		a.ID = *dto.ID
	}
	if err := s.db.Create(a).Error; err != nil {
		return nil, err
	}
	return a, nil
}

func (s *Service) FindAllByUser(userId string) ([]Account, error) {
	var accounts []Account
	err := s.db.
		Where("\"userId\" = ?", userId).
		Order("\"createdAt\" DESC").
		Find(&accounts).Error
	return accounts, err
}

func (s *Service) FindOne(id, userId string) (*Account, error) {
	var a Account
	err := s.db.Where("id = ? AND \"userId\" = ?", id, userId).First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, common.NewNotFound("Account not found")
	}
	return &a, err
}

func (s *Service) Update(id, userId string, dto UpdateAccountDto) (*Account, error) {
	a, err := s.FindOne(id, userId)
	if err != nil {
		return nil, common.GoneIfDeleted(s.db, "account", id, userId, err)
	}
	if common.IsStale(a.ClientModifiedAt, dto.ClientModifiedAt) {
		return a, nil
	}
	if dto.ClientModifiedAt != nil {
		a.ClientModifiedAt = common.ClientTime(dto.ClientModifiedAt)
	}

	if dto.Name != nil {
		a.Name = *dto.Name
	}
	if dto.Currency != nil && *dto.Currency != a.Currency {
		// Transaction amounts are stored in the account's currency; changing it
		// would silently relabel them as a different currency.
		var txCount int64
		if err := s.db.Raw(`SELECT COUNT(*) FROM "transaction" WHERE "accountId" = ? AND "deletedAt" IS NULL`, a.ID).
			Scan(&txCount).Error; err != nil {
			return nil, err
		}
		if txCount > 0 {
			return nil, common.NewBadRequest("Cannot change the currency of an account that has transactions")
		}
		a.Currency = *dto.Currency
	}
	if dto.Balance != nil {
		a.Balance = *dto.Balance
	}
	if dto.Description != nil {
		a.Description = dto.Description
	}

	if err := s.db.Save(a).Error; err != nil {
		return nil, err
	}
	return a, nil
}

func (s *Service) Remove(id, userId string) error {
	a, err := s.FindOne(id, userId)
	if err != nil {
		return common.IgnoreIfDeleted(s.db, "account", id, userId, err)
	}
	return s.db.Delete(a).Error
}

func (s *Service) UpdateBalance(id, userId string, newBalance float64) (*Account, error) {
	a, err := s.FindOne(id, userId)
	if err != nil {
		return nil, err
	}
	a.Balance = newBalance
	if err := s.db.Save(a).Error; err != nil {
		return nil, err
	}
	return a, nil
}

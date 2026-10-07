package user

import (
	"errors"

	"ger_fin_api/internal/common"

	"github.com/google/uuid"

	"gorm.io/gorm"
)

type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

func (s *Service) FindByEmail(email string) (*User, error) {
	var u User
	err := s.db.Where("LOWER(email) = LOWER(?)", email).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &u, err
}

func (s *Service) FindById(id string) (*User, error) {
	var u User
	err := s.db.First(&u, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &u, err
}

func (s *Service) Create(u *User) (*User, error) {
	if err := s.db.Create(u).Error; err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Service) UpdateProfile(id string, dto UpdateProfileDto) (*User, error) {
	u, err := s.FindById(id)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, common.NewNotFound("User not found")
	}

	if dto.FirstName != nil {
		u.FirstName = dto.FirstName
	}
	if dto.LastName != nil {
		u.LastName = dto.LastName
	}

	if err := s.db.Save(u).Error; err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Service) GetProfile(id string) (*User, error) {
	u, err := s.FindById(id)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, common.NewNotFound("User not found")
	}
	return u, nil
}

// GetSettings returns the user's settings, auto-creating defaults on first access.
func (s *Service) GetSettings(userId string) (*UserSettings, error) {
	var settings UserSettings
	err := s.db.Where("\"userId\" = ?", userId).First(&settings).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		settings = UserSettings{UserId: userId}
		if err := s.db.Create(&settings).Error; err != nil {
			// A concurrent first request created the row; read it back.
			if common.IsUniqueViolation(err) {
				settings = UserSettings{}
				if err := s.db.Where("\"userId\" = ?", userId).First(&settings).Error; err != nil {
					return nil, err
				}
				return &settings, nil
			}
			return nil, err
		}
		return &settings, nil
	}
	if err != nil {
		return nil, err
	}
	return &settings, nil
}

func (s *Service) UpdateSettings(userId string, dto UpdateSettingsDto) (*UserSettings, error) {
	settings, err := s.GetSettings(userId)
	if err != nil {
		return nil, err
	}
	if common.IsStale(settings.ClientModifiedAt, dto.ClientModifiedAt) {
		return settings, nil
	}
	if dto.ClientModifiedAt != nil {
		settings.ClientModifiedAt = common.ClientTime(dto.ClientModifiedAt)
	}

	if dto.CashFlowMonths != nil {
		settings.CashFlowMonths = *dto.CashFlowMonths
	}
	if dto.YearOverYearYears != nil {
		settings.YearOverYearYears = *dto.YearOverYearYears
	}
	if dto.BaseCurrency != nil {
		settings.BaseCurrency = *dto.BaseCurrency
	}
	if dto.DefaultAccountId != nil {
		if *dto.DefaultAccountId == "" {
			settings.DefaultAccountId = nil
		} else {
			if _, err := uuid.Parse(*dto.DefaultAccountId); err != nil {
				return nil, common.NewBadRequest("defaultAccountId must be a UUID")
			}
			var count int64
			if err := s.db.Table("account").
				Where(`id = ? AND "userId" = ? AND "deletedAt" IS NULL`, *dto.DefaultAccountId, userId).
				Count(&count).Error; err != nil {
				return nil, err
			}
			if count == 0 {
				return nil, common.NewNotFound("Account not found")
			}
			settings.DefaultAccountId = dto.DefaultAccountId
		}
	}

	if err := s.db.Save(settings).Error; err != nil {
		return nil, err
	}
	return settings, nil
}

// GetReportDefaults satisfies report.SettingsProvider. Falls back to hardcoded defaults on error.
func (s *Service) GetReportDefaults(userId string) (cashFlowMonths, yearOverYearYears int, err error) {
	settings, err := s.GetSettings(userId)
	if err != nil {
		return 3, 2, nil
	}
	return settings.CashFlowMonths, settings.YearOverYearYears, nil
}

// GetBaseCurrency returns the user's preferred base currency, defaulting to "USD".
func (s *Service) GetBaseCurrency(userId string) (string, error) {
	settings, err := s.GetSettings(userId)
	if err != nil {
		return "USD", err
	}
	return settings.BaseCurrency, nil
}

package budget

import (
	"ger_fin_api/internal/currency"
	"ger_fin_api/internal/user"
	"gorm.io/gorm"
)

type Service struct {
	db       *gorm.DB
	currSvc  *currency.Service
	userSvc  *user.Service
}

func NewService(db *gorm.DB, currSvc *currency.Service, userSvc *user.Service) *Service {
	return &Service{
		db:      db,
		currSvc: currSvc,
		userSvc: userSvc,
	}
}

package recurring_transaction

import (
	"time"

	"ger_fin_api/internal/account"
	"ger_fin_api/internal/category"
	"ger_fin_api/internal/common"
)

type RecurringTransaction struct {
	common.BaseModel
	Type        string              `gorm:"column:type;not null"                      json:"type"`
	Amount      float64             `gorm:"column:amount;type:numeric(15,2);not null" json:"amount"`
	Description string              `gorm:"column:description;not null"               json:"description"`
	CategoryId  string              `gorm:"column:categoryId;type:uuid;not null"      json:"categoryId"`
	Category    *category.Category  `gorm:"foreignKey:CategoryId"                     json:"category,omitempty"`
	UserId      string              `gorm:"column:userId;type:uuid;not null"          json:"userId"`
	AccountId   string              `gorm:"column:accountId;type:uuid;not null"       json:"accountId"`
	Account     *account.Account    `gorm:"foreignKey:AccountId"                      json:"account,omitempty"`
	Frequency   string              `gorm:"column:frequency;not null"                 json:"frequency"`
	StartDate   time.Time           `gorm:"column:startDate;not null"                 json:"startDate"`
	EndDate     *time.Time          `gorm:"column:endDate"                            json:"endDate,omitempty"`
	NextDueDate *time.Time          `gorm:"column:nextDueDate"                        json:"nextDueDate,omitempty"`
	IsActive    bool                `gorm:"column:isActive;default:true"              json:"isActive"`
}

func (RecurringTransaction) TableName() string { return "recurring_transaction" }

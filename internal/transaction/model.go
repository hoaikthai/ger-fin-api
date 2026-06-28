package transaction

import (
	"time"

	"ger_fin_api/internal/account"
	"ger_fin_api/internal/category"
	"ger_fin_api/internal/common"
)

type Transaction struct {
	common.BaseModel
	Type                   string              `gorm:"column:type;not null"                      json:"type"`
	Amount                 float64             `gorm:"column:amount;type:numeric(15,2);not null"  json:"amount"`
	Description            string              `gorm:"column:description;not null"               json:"description"`
	CategoryId             string              `gorm:"column:categoryId;type:uuid;not null"       json:"categoryId"`
	Category               *category.Category  `gorm:"foreignKey:CategoryId"                     json:"category,omitempty"`
	UserId                 string              `gorm:"column:userId;type:uuid;not null"           json:"userId"`
	AccountId              string              `gorm:"column:accountId;type:uuid;not null"        json:"accountId"`
	Account                *account.Account    `gorm:"foreignKey:AccountId"                      json:"account,omitempty"`
	TransactionDate        time.Time           `gorm:"column:transactionDate"                    json:"transactionDate"`
	RelatedTransactionId   *string             `gorm:"column:relatedTransactionId;type:uuid"     json:"relatedTransactionId,omitempty"`
	RecurringTransactionId *string             `gorm:"column:recurringTransactionId;type:uuid"   json:"recurringTransactionId,omitempty"`
	OriginalCurrency       *string             `gorm:"column:originalCurrency;type:varchar(3)"   json:"originalCurrency,omitempty"`
	OriginalAmount         *float64            `gorm:"column:originalAmount;type:numeric(15,2)"  json:"originalAmount,omitempty"`
}

func (Transaction) TableName() string { return "transaction" }

package recurring_transaction

import (
	"time"

	"ger_fin_api/internal/common"
)

type CreateRecurringTransactionDto struct {
	common.CreateMeta
	Type        common.TransactionType     `json:"type"        binding:"required,oneof=income expense"`
	Amount      float64                    `json:"amount"      binding:"required,gt=0"`
	Description string                     `json:"description" binding:"required"`
	CategoryId  string                     `json:"categoryId"  binding:"required,uuid"`
	AccountId   string                     `json:"accountId"   binding:"required,uuid"`
	Frequency   common.RecurrenceFrequency `json:"frequency"   binding:"required,oneof=daily weekly monthly yearly"`
	StartDate   time.Time                  `json:"startDate"   binding:"required"`
	EndDate     *time.Time                 `json:"endDate"     binding:"omitempty"`
	IsActive    *bool                      `json:"isActive"    binding:"omitempty"`
}

type UpdateRecurringTransactionDto struct {
	common.UpdateMeta
	Type        *common.TransactionType     `json:"type"        binding:"omitempty,oneof=income expense"`
	Amount      *float64                    `json:"amount"      binding:"omitempty,gt=0"`
	Description *string                     `json:"description" binding:"omitempty"`
	CategoryId  *string                     `json:"categoryId"  binding:"omitempty,uuid"`
	AccountId   *string                     `json:"accountId"   binding:"omitempty,uuid"`
	Frequency   *common.RecurrenceFrequency `json:"frequency"   binding:"omitempty,oneof=daily weekly monthly yearly"`
	StartDate   *time.Time                  `json:"startDate"   binding:"omitempty"`
	EndDate     *time.Time                  `json:"endDate"     binding:"omitempty"`
	IsActive    *bool                       `json:"isActive"    binding:"omitempty"`
}

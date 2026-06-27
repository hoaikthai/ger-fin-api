package transaction

import (
	"time"

	"ger_fin_api/internal/common"
)

type CreateTransactionDto struct {
	Type            common.TransactionType `json:"type"            binding:"required,oneof=income expense"`
	Amount          float64                `json:"amount"          binding:"required"`
	Description     string                 `json:"description"     binding:"required"`
	CategoryId      string                 `json:"categoryId"      binding:"required,uuid"`
	AccountId       string                 `json:"accountId"       binding:"required,uuid"`
	TransactionDate *time.Time             `json:"transactionDate" binding:"omitempty"`
}

type UpdateTransactionDto struct {
	Type            *common.TransactionType `json:"type"            binding:"omitempty,oneof=income expense"`
	Amount          *float64                `json:"amount"          binding:"omitempty"`
	Description     *string                 `json:"description"     binding:"omitempty"`
	CategoryId      *string                 `json:"categoryId"      binding:"omitempty,uuid"`
	AccountId       *string                 `json:"accountId"       binding:"omitempty,uuid"`
	TransactionDate *time.Time              `json:"transactionDate" binding:"omitempty"`
}

type CreateTransferDto struct {
	SourceAccountId      string     `json:"sourceAccountId"      binding:"required,uuid"`
	DestinationAccountId string     `json:"destinationAccountId" binding:"required,uuid"`
	Amount               float64    `json:"amount"               binding:"required,gt=0"`
	Description          string     `json:"description"          binding:"required"`
	TransactionDate      *time.Time `json:"transactionDate"      binding:"omitempty"`
}

type TimeRangeQuery struct {
	Period common.TimePeriod `form:"period" binding:"omitempty,oneof=day week month quarter year"`
	Offset int               `form:"offset" binding:"omitempty,min=-100,max=100"`
}

type ImportResult struct {
	Imported          int      `json:"imported"`
	Skipped           int      `json:"skipped"`
	Errors            []string `json:"errors"`
	UnknownCategories []string `json:"unknownCategories,omitempty"`
}

type ValidateResult struct {
	Valid              bool     `json:"valid"`
	UnknownAccounts   []string `json:"unknownAccounts,omitempty"`
	UnknownCategories []string `json:"unknownCategories,omitempty"`
	RowErrors         []string `json:"rowErrors,omitempty"`
}

type ImportJobResponse struct {
	JobId string `json:"jobId"`
}

type ImportJobStatus struct {
	Status string        `json:"status"` // pending | running | done | failed
	Result *ImportResult `json:"result,omitempty"`
	Error  string        `json:"error,omitempty"`
}

type TransferResult struct {
	SourceTransaction      *Transaction `json:"sourceTransaction"`
	DestinationTransaction *Transaction `json:"destinationTransaction"`
}

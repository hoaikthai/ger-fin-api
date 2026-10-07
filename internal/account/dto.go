package account

import "ger_fin_api/internal/common"

type CreateAccountDto struct {
	common.CreateMeta
	Name        string  `json:"name"        binding:"required"`
	Currency    string  `json:"currency"    binding:"required,len=3"`
	Balance     float64 `json:"balance"     binding:"omitempty,gte=0"`
	Description *string `json:"description" binding:"omitempty"`
}

type UpdateAccountDto struct {
	common.UpdateMeta
	Name        *string  `json:"name"        binding:"omitempty"`
	Currency    *string  `json:"currency"    binding:"omitempty,len=3"`
	Balance     *float64 `json:"balance"     binding:"omitempty"`
	Description *string  `json:"description" binding:"omitempty"`
}

// AccountResponse wraps Account with computed base-currency fields.
type AccountResponse struct {
	Account
	BaseCurrencyBalance *float64 `json:"baseCurrencyBalance,omitempty"`
	BaseCurrency        string   `json:"baseCurrency"`
}

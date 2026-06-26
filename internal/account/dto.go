package account

type CreateAccountDto struct {
	Name        string  `json:"name"        binding:"required"`
	Currency    string  `json:"currency"    binding:"required,len=3"`
	Balance     float64 `json:"balance"     binding:"omitempty,gte=0"`
	Description *string `json:"description" binding:"omitempty"`
}

type UpdateAccountDto struct {
	Name        *string  `json:"name"        binding:"omitempty"`
	Currency    *string  `json:"currency"    binding:"omitempty,len=3"`
	Balance     *float64 `json:"balance"     binding:"omitempty"`
	Description *string  `json:"description" binding:"omitempty"`
}

package user

import "ger_fin_api/internal/common"

type UpdateProfileDto struct {
	FirstName *string `json:"firstName" binding:"omitempty,max=50"`
	LastName  *string `json:"lastName"  binding:"omitempty,max=50"`
}

type UpdateSettingsDto struct {
	common.UpdateMeta
	CashFlowMonths    *int    `json:"cashFlowMonths"    binding:"omitempty,min=1,max=12"`
	YearOverYearYears *int    `json:"yearOverYearYears" binding:"omitempty,min=1,max=5"`
	BaseCurrency      *string `json:"baseCurrency"      binding:"omitempty,len=3"`
	DefaultAccountId  *string `json:"defaultAccountId"` // "" clears it
}

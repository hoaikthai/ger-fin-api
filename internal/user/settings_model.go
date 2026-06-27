package user

import "ger_fin_api/internal/common"

type UserSettings struct {
	common.BaseModel
	UserId            string  `gorm:"column:userId;type:uuid;not null;uniqueIndex" json:"userId"`
	CashFlowMonths    int     `gorm:"column:cashFlowMonths;default:3"              json:"cashFlowMonths"`
	YearOverYearYears int     `gorm:"column:yearOverYearYears;default:2"           json:"yearOverYearYears"`
	BaseCurrency      string  `gorm:"column:baseCurrency;default:USD"              json:"baseCurrency"`
	DefaultAccountId  *string `gorm:"column:defaultAccountId;type:uuid"            json:"defaultAccountId"`
}

func (UserSettings) TableName() string { return "user_settings" }

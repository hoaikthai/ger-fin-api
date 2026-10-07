package account

import "ger_fin_api/internal/common"

type Account struct {
	common.BaseModel
	Name        string  `gorm:"column:name;not null"                    json:"name"`
	Currency    string  `gorm:"column:currency;type:varchar(3);not null" json:"currency"`
	Balance     float64 `gorm:"column:balance;type:numeric(15,2);default:0" json:"balance"`
	Description *string `gorm:"column:description"                      json:"description,omitempty"`
	IsActive    bool    `gorm:"column:isActive;default:true"            json:"isActive"`
	UserId      string  `gorm:"column:userId;type:uuid;not null"        json:"userId"`
}

func (Account) TableName() string { return "account" }

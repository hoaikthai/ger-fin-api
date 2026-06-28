package currency

import (
	"time"

	"ger_fin_api/internal/common"
)

type ExchangeRate struct {
	common.BaseModel
	BaseCurrency   string    `gorm:"column:baseCurrency;type:varchar(3);not null"   json:"baseCurrency"`
	TargetCurrency string    `gorm:"column:targetCurrency;type:varchar(3);not null" json:"targetCurrency"`
	Rate           float64   `gorm:"column:rate;type:numeric(20,8);not null"        json:"rate"`
	FetchedAt      time.Time `gorm:"column:fetchedAt;not null"                      json:"fetchedAt"`
}

func (ExchangeRate) TableName() string { return "exchangeRate" }

package budget

import (
	"time"

	"ger_fin_api/internal/category"
	"ger_fin_api/internal/common"
)

type Budget struct {
	common.BaseModel
	SeriesId    string             `gorm:"column:seriesId;type:uuid;not null"          json:"seriesId"`
	CategoryId  *string            `gorm:"column:categoryId;type:uuid"                 json:"categoryId"`
	Category    *category.Category `gorm:"foreignKey:CategoryId"                       json:"category,omitempty"`
	UserId      string             `gorm:"column:userId;type:uuid;not null"            json:"userId"`
	PeriodType  string             `gorm:"column:periodType;not null"                  json:"periodType"`
	PeriodStart time.Time          `gorm:"column:periodStart;type:date;not null"       json:"periodStart"`
	PeriodEnd   time.Time          `gorm:"column:periodEnd;type:date;not null"         json:"periodEnd"`
	Amount      float64            `gorm:"column:amount;type:numeric(15,2);not null"   json:"amount"`
	Currency    string             `gorm:"column:currency;type:varchar(3);not null"    json:"currency"`
	CarriedOver float64            `gorm:"column:carriedOver;type:numeric(15,2);not null;default:0" json:"carriedOver"`
	Rollover    bool               `gorm:"column:rollover;not null;default:false"      json:"rollover"`
	IsActive    bool               `gorm:"column:isActive;not null;default:true"       json:"isActive"`
}

func (Budget) TableName() string { return "budget" }

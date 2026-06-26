package user

import "ger_fin_api/internal/common"

type User struct {
	common.BaseModel
	Email     string  `gorm:"column:email;uniqueIndex;not null" json:"email"`
	Password  string  `gorm:"column:password;not null"          json:"-"`
	FirstName *string `gorm:"column:firstName"                  json:"firstName"`
	LastName  *string `gorm:"column:lastName"                   json:"lastName"`
}

func (User) TableName() string { return "user" }

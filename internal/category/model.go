package category

import "ger_fin_api/internal/common"

type Category struct {
	common.BaseModel
	Name      string      `gorm:"column:name;not null"                             json:"name"`
	Type      string      `gorm:"column:type;not null"                             json:"type"`
	IsDefault bool        `gorm:"column:isDefault;default:false"                   json:"isDefault"`
	UserId    *string     `gorm:"column:userId;type:uuid"                          json:"userId"`
	ParentId  *string     `gorm:"column:parentId;type:uuid"                        json:"parentId"`
	Parent    *Category   `gorm:"foreignKey:ParentId"                              json:"parent,omitempty"`
	Children  []Category  `gorm:"foreignKey:ParentId"                              json:"children,omitempty"`
}

func (Category) TableName() string { return "category" }

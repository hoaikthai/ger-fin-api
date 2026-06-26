package common

import (
	"time"

	"gorm.io/gorm"
)

type BaseModel struct {
	ID        string         `gorm:"column:id;primarykey;type:uuid;default:uuid_generate_v4()" json:"id"`
	CreatedAt time.Time      `gorm:"column:createdAt;autoCreateTime"                           json:"createdAt"`
	UpdatedAt time.Time      `gorm:"column:updatedAt;autoUpdateTime"                           json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"column:deletedAt;index"                                    json:"-"`
}

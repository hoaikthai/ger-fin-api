package category

import "ger_fin_api/internal/common"

type CreateCategoryDto struct {
	Name     string                 `json:"name"     binding:"required"`
	Type     common.TransactionType `json:"type"     binding:"required,oneof=income expense"`
	ParentId *string                `json:"parentId" binding:"omitempty,uuid"`
}

type UpdateCategoryDto struct {
	Name     *string                 `json:"name"     binding:"omitempty"`
	Type     *common.TransactionType `json:"type"     binding:"omitempty,oneof=income expense"`
	ParentId *string                 `json:"parentId" binding:"omitempty,uuid"`
}

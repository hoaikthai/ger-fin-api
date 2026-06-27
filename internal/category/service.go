package category

import (
	"errors"

	"ger_fin_api/internal/common"

	"gorm.io/gorm"
)

type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

func (s *Service) withRelations(q *gorm.DB) *gorm.DB {
	return q.Preload("Parent").Preload("Children")
}

func (s *Service) FindAll(userId string) ([]Category, error) {
	var categories []Category
	err := s.withRelations(s.db).
		Where("\"userId\" = ? OR \"isDefault\" = ?", userId, true).
		Order("name ASC").
		Find(&categories).Error
	return categories, err
}

func (s *Service) FindAllByType(userId string, txType common.TransactionType) ([]Category, error) {
	var categories []Category
	err := s.withRelations(s.db).
		Where("(\"userId\" = ? OR \"isDefault\" = ?) AND type = ?", userId, true, txType).
		Order("name ASC").
		Find(&categories).Error
	return categories, err
}

func (s *Service) FindOne(id, userId string) (*Category, error) {
	var cat Category
	err := s.withRelations(s.db).First(&cat, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, common.NewNotFound("Category not found")
	}
	if err != nil {
		return nil, err
	}

	if !cat.IsDefault && (cat.UserId == nil || *cat.UserId != userId) {
		return nil, common.NewForbidden("Access denied to this category")
	}

	return &cat, nil
}

func (s *Service) Create(dto CreateCategoryDto, userId string) (*Category, error) {
	if dto.ParentId != nil {
		parent, err := s.FindOne(*dto.ParentId, userId)
		if err != nil {
			return nil, err
		}
		if parent.ParentId != nil {
			return nil, common.NewBadRequest("Cannot create category under a child category")
		}
		if parent.Type != string(dto.Type) {
			return nil, common.NewBadRequest("Category type must match parent type")
		}
	}

	cat := &Category{
		Name:      dto.Name,
		Type:      string(dto.Type),
		IsDefault: false,
		UserId:    &userId,
		ParentId:  dto.ParentId,
	}

	if err := s.db.Create(cat).Error; err != nil {
		return nil, err
	}
	return cat, nil
}

func (s *Service) Update(id string, dto UpdateCategoryDto, userId string) (*Category, error) {
	cat, err := s.FindOne(id, userId)
	if err != nil {
		return nil, err
	}
	if cat.IsDefault {
		return nil, common.NewForbidden("Cannot update default categories")
	}

	if dto.ParentId != nil && dto.ParentId != cat.ParentId {
		parent, err := s.FindOne(*dto.ParentId, userId)
		if err != nil {
			return nil, err
		}
		if parent.ParentId != nil {
			return nil, common.NewBadRequest("Cannot create category under a child category")
		}
		finalType := cat.Type
		if dto.Type != nil {
			finalType = string(*dto.Type)
		}
		if parent.Type != finalType {
			return nil, common.NewBadRequest("Category type must match parent type")
		}
		cat.ParentId = dto.ParentId
	}

	if dto.Name != nil {
		cat.Name = *dto.Name
	}
	if dto.Type != nil {
		cat.Type = string(*dto.Type)
	}

	if err := s.db.Save(cat).Error; err != nil {
		return nil, err
	}
	return cat, nil
}

func (s *Service) Remove(id, userId string) error {
	cat, err := s.FindOne(id, userId)
	if err != nil {
		return err
	}
	if cat.IsDefault {
		return common.NewForbidden("Cannot delete default categories")
	}

	var childCount int64
	if err := s.db.Model(&Category{}).Where("\"parentId\" = ?", id).Count(&childCount).Error; err != nil {
		return err
	}
	if childCount > 0 {
		return common.NewBadRequest("Cannot delete category that has child categories")
	}

	var txCount int64
	if err := s.db.Raw(`SELECT COUNT(*) FROM "transaction" WHERE "categoryId" = ? AND "deletedAt" IS NULL`, id).Scan(&txCount).Error; err != nil {
		return err
	}
	if txCount > 0 {
		return common.NewBadRequest("Cannot delete category that has associated transactions")
	}

	return s.db.Delete(cat).Error
}

func (s *Service) SeedDefaultCategories() error {
	var count int64
	if err := s.db.Model(&Category{}).Where("\"isDefault\" = ?", true).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	parents := defaultParentCategories()
	for i := range parents {
		if err := s.db.Create(&parents[i]).Error; err != nil {
			return err
		}
	}

	var savedParents []Category
	if err := s.db.Where("\"isDefault\" = ? AND \"parentId\" IS NULL", true).Find(&savedParents).Error; err != nil {
		return err
	}

	parentByName := make(map[string]*Category, len(savedParents))
	for i := range savedParents {
		parentByName[savedParents[i].Name] = &savedParents[i]
	}

	for _, child := range defaultChildCategories() {
		parent, ok := parentByName[child.parentName]
		if !ok {
			continue
		}
		c := &Category{
			Name:      child.name,
			Type:      parent.Type,
			IsDefault: true,
			UserId:    nil,
			ParentId:  &parent.ID,
		}
		if err := s.db.Create(c).Error; err != nil {
			return err
		}
	}

	return nil
}

func defaultParentCategories() []Category {
	expense := string(common.TransactionTypeExpense)
	income := string(common.TransactionTypeIncome)
	t := true

	names := []struct {
		name string
		typ  string
	}{
		{"Food & Beverage", expense},
		{"Bills & Utilities", expense},
		{"Transportation", expense},
		{"Shopping", expense},
		{"Family", expense},
		{"Health & Fitness", expense},
		{"Education", expense},
		{"Entertainment", expense},
		{"Gift & Donation", expense},
		{"Insurances", expense},
		{"Other expense", expense},
		{"Outgoing transfer", expense},
		{"Travel", expense},
		{"Salary", income},
		{"Incoming transfer", income},
		{"Collect interest", income},
		{"Gifts", income},
		{"Award", income},
		{"Selling", income},
	}

	cats := make([]Category, len(names))
	for i, n := range names {
		cats[i] = Category{Name: n.name, Type: n.typ, IsDefault: t}
	}
	return cats
}

type childDef struct{ name, parentName string }

func defaultChildCategories() []childDef {
	return []childDef{
		{"Café", "Food & Beverage"},
		{"Restaurant", "Food & Beverage"},
		{"Bread and Noodles", "Food & Beverage"},
		{"Phone bill", "Bills & Utilities"},
		{"Television Bill", "Bills & Utilities"},
		{"Internet Bill", "Bills & Utilities"},
		{"Piggy bank", "Bills & Utilities"},
		{"Vehicle maintenance", "Transportation"},
		{"Parking fees", "Transportation"},
		{"Petrol", "Transportation"},
		{"Taxi", "Transportation"},
		{"Electronic devices", "Shopping"},
		{"Makeup", "Shopping"},
		{"Clothing", "Shopping"},
		{"Footwear", "Shopping"},
		{"Apps", "Shopping"},
		{"Fitness", "Health & Fitness"},
		{"Doctor", "Health & Fitness"},
		{"Personal care", "Health & Fitness"},
		{"Pharmacy", "Health & Fitness"},
		{"Sports", "Health & Fitness"},
		{"Barber", "Health & Fitness"},
		{"Books", "Education"},
		{"Streaming service", "Entertainment"},
		{"Games", "Entertainment"},
		{"Movies", "Entertainment"},
		{"Musics", "Entertainment"},
		{"Friends & Lover", "Gift & Donation"},
		{"Funeral", "Gift & Donation"},
		{"Marriage", "Gift & Donation"},
		{"Lucky money", "Gift & Donation"},
		{"Hotel", "Travel"},
	}
}

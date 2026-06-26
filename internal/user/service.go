package user

import (
	"ger_fin_api/internal/common"

	"gorm.io/gorm"
)

type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

func (s *Service) FindByEmail(email string) (*User, error) {
	var u User
	err := s.db.Where("email = ?", email).First(&u).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &u, err
}

func (s *Service) FindById(id string) (*User, error) {
	var u User
	err := s.db.First(&u, "id = ?", id).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &u, err
}

func (s *Service) Create(u *User) (*User, error) {
	if err := s.db.Create(u).Error; err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Service) UpdateProfile(id string, dto UpdateProfileDto) (*User, error) {
	u, err := s.FindById(id)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, common.NewNotFound("User not found")
	}

	if dto.FirstName != nil {
		u.FirstName = dto.FirstName
	}
	if dto.LastName != nil {
		u.LastName = dto.LastName
	}

	if err := s.db.Save(u).Error; err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Service) GetProfile(id string) (*User, error) {
	u, err := s.FindById(id)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, common.NewNotFound("User not found")
	}
	return u, nil
}

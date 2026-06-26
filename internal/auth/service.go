package auth

import (
	"time"

	"ger_fin_api/internal/common"
	"ger_fin_api/internal/user"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type Service struct {
	userSvc   *user.Service
	jwtSecret string
}

func NewService(userSvc *user.Service, jwtSecret string) *Service {
	return &Service{userSvc: userSvc, jwtSecret: jwtSecret}
}

func (s *Service) Register(dto RegisterDto) (*user.User, error) {
	existing, err := s.userSvc.FindByEmail(dto.Email)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, common.NewConflict("Email already in use")
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(dto.Password), 10)
	if err != nil {
		return nil, err
	}

	u := &user.User{
		Email:     dto.Email,
		Password:  string(hashed),
		FirstName: dto.FirstName,
		LastName:  dto.LastName,
	}

	return s.userSvc.Create(u)
}

func (s *Service) ValidateUser(email, password string) (*user.User, error) {
	u, err := s.userSvc.FindByEmail(email)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, common.NewUnauthorized("Invalid credentials")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(password)); err != nil {
		return nil, common.NewUnauthorized("Invalid credentials")
	}

	return u, nil
}

func (s *Service) Login(email, password string) (string, error) {
	u, err := s.ValidateUser(email, password)
	if err != nil {
		return "", err
	}

	claims := common.JWTClaims{
		Sub:   u.ID,
		Email: u.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.jwtSecret))
}

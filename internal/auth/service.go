package auth

import (
	"errors"
	"strings"
	"time"

	"ger_fin_api/internal/common"
	"ger_fin_api/internal/user"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = 10

// ErrEmailTaken is returned by Register for an existing email. The handler
// answers it exactly like a successful registration, so the endpoint does not
// reveal which emails have accounts.
var ErrEmailTaken = errors.New("email already in use")

// dummyHash is compared against when the user does not exist, so login takes
// the same time for unknown and known emails (prevents timing enumeration).
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing"), bcryptCost)

type Service struct {
	userSvc   *user.Service
	jwtSecret string
	jwtExpiry time.Duration
}

func NewService(userSvc *user.Service, jwtSecret string, jwtExpiry time.Duration) *Service {
	return &Service{userSvc: userSvc, jwtSecret: jwtSecret, jwtExpiry: jwtExpiry}
}

func (s *Service) Register(dto RegisterDto) (*user.User, error) {
	dto.Email = strings.ToLower(strings.TrimSpace(dto.Email))

	// Hash before the existence check so both outcomes take the same time.
	hashed, err := bcrypt.GenerateFromPassword([]byte(dto.Password), bcryptCost)
	if err != nil {
		return nil, err
	}

	existing, err := s.userSvc.FindByEmail(dto.Email)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ErrEmailTaken
	}

	u := &user.User{
		Email:     dto.Email,
		Password:  string(hashed),
		FirstName: dto.FirstName,
		LastName:  dto.LastName,
	}

	created, err := s.userSvc.Create(u)
	if common.IsUniqueViolation(err) {
		return nil, ErrEmailTaken
	}
	return created, err
}

func (s *Service) ValidateUser(email, password string) (*user.User, error) {
	u, err := s.userSvc.FindByEmail(email)
	if err != nil {
		return nil, err
	}
	if u == nil {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
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
			ID:        uuid.New().String(), // jti, used to revoke the token on logout
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.jwtExpiry)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.jwtSecret))
}

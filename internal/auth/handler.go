package auth

import (
	"errors"
	"time"

	"ger_fin_api/internal/common"
	"ger_fin_api/internal/middleware"

	"github.com/gin-gonic/gin"
)

// TokenRevoker denylists a token ID until it expires.
type TokenRevoker interface {
	Revoke(jti string, expiresAt time.Time) error
}

type Handler struct {
	svc    *Service
	tokens TokenRevoker
}

func NewHandler(svc *Service, tokens TokenRevoker) *Handler {
	return &Handler{svc: svc, tokens: tokens}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, auth gin.HandlerFunc) {
	g := rg.Group("/auth")
	g.POST("/register", h.register)
	g.POST("/login", h.login)
	g.POST("/logout", auth, h.logout)
}

// @Summary		Register a new user
// @Description	Create a new user account with email and password. The response is identical whether or not the email was already registered; log in to continue.
// @Tags		auth
// @Accept		json
// @Produce		json
// @Param		body body RegisterDto true "Registration details"
// @Success		201 {object} common.MessageResponse
// @Failure		400 {object} common.ErrorResponse
// @Failure		429 {object} common.ErrorResponse
// @Router		/auth/register [post]
func (h *Handler) register(c *gin.Context) {
	var dto RegisterDto
	if !common.BindJSON(c, &dto) {
		return
	}

	_, err := h.svc.Register(dto)
	if err != nil && !errors.Is(err, ErrEmailTaken) {
		var appErr *common.AppError
		if errors.As(err, &appErr) {
			common.RespondError(c, appErr)
			return
		}
		common.RespondInternalError(c)
		return
	}

	c.JSON(201, common.MessageResponse{Message: registerAcceptedMessage})
}

// @Summary		Logout
// @Description	Revoke the current access token
// @Tags		auth
// @Produce		json
// @Success		200 {object} common.MessageResponse
// @Failure		401 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/auth/logout [post]
func (h *Handler) logout(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	expiresAt := time.Now().Add(24 * time.Hour)
	if claims.ExpiresAt != nil {
		expiresAt = claims.ExpiresAt.Time
	}
	if err := h.tokens.Revoke(claims.ID, expiresAt); err != nil {
		common.RespondInternalError(c)
		return
	}
	c.JSON(200, common.MessageResponse{Message: "Logged out"})
}

// @Summary		Login
// @Description	Authenticate with email and password, receive a JWT access token
// @Tags		auth
// @Accept		json
// @Produce		json
// @Param		body body LoginDto true "Login credentials"
// @Success		200 {object} LoginResponse
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Router		/auth/login [post]
func (h *Handler) login(c *gin.Context) {
	var dto LoginDto
	if !common.BindJSON(c, &dto) {
		return
	}

	token, err := h.svc.Login(dto.Email, dto.Password)
	if err != nil {
		var appErr *common.AppError
		if errors.As(err, &appErr) {
			common.RespondError(c, appErr)
			return
		}
		common.RespondInternalError(c)
		return
	}

	c.JSON(200, gin.H{"access_token": token})
}

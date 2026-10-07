package auth

import (
	"errors"
	"time"

	"ger_fin_api/internal/common"
	"ger_fin_api/internal/middleware"
	"ger_fin_api/internal/user"

	"github.com/gin-gonic/gin"
)

// TokenRevoker denylists a token ID until it expires.
type TokenRevoker interface {
	Revoke(jti string, expiresAt time.Time) error
}

// RefreshTokens issues, rotates and revokes refresh tokens (see RefreshStore).
type RefreshTokens interface {
	Issue(userId string) (string, error)
	Rotate(token string) (userId string, next string, err error)
	RevokeFamilyOf(token, userId string) error
}

type Handler struct {
	svc     *Service
	tokens  TokenRevoker
	refresh RefreshTokens
}

func NewHandler(svc *Service, tokens TokenRevoker, refresh RefreshTokens) *Handler {
	return &Handler{svc: svc, tokens: tokens, refresh: refresh}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, auth gin.HandlerFunc) {
	g := rg.Group("/auth")
	g.POST("/register", h.register)
	g.POST("/login", h.login)
	g.POST("/refresh", h.refreshTokens)
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
// @Accept		json
// @Produce		json
// @Param		body body LogoutDto false "Refresh token to revoke"
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

	var dto LogoutDto
	_ = c.ShouldBindJSON(&dto) // body is optional
	if dto.RefreshToken != nil {
		if err := h.refresh.RevokeFamilyOf(*dto.RefreshToken, claims.Sub); err != nil {
			common.RespondInternalError(c)
			return
		}
	}
	c.JSON(200, common.MessageResponse{Message: "Logged out"})
}

// @Summary		Login
// @Description	Authenticate with email and password, receive a JWT access token and a refresh token
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
	u, err := h.svc.ValidateUser(dto.Email, dto.Password)
	if err != nil {
		common.RespondAppError(c, err)
		return
	}
	refreshToken, err := h.refresh.Issue(u.ID)
	if err != nil {
		common.RespondInternalError(c)
		return
	}
	h.respondWithTokens(c, u, refreshToken)
}

func (h *Handler) respondWithTokens(c *gin.Context, u *user.User, refreshToken string) {
	access, err := h.svc.AccessToken(u)
	if err != nil {
		common.RespondInternalError(c)
		return
	}
	c.JSON(200, LoginResponse{
		AccessToken:  access,
		RefreshToken: refreshToken,
		ExpiresIn:    int(h.svc.AccessTTL().Seconds()),
	})
}

// @Summary		Refresh tokens
// @Description	Exchange a refresh token for a new access token and a new refresh token (the old one stops working). Reusing an old refresh token ends the session.
// @Tags		auth
// @Accept		json
// @Produce		json
// @Param		body body RefreshDto true "Refresh token"
// @Success		200 {object} LoginResponse
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Failure		429 {object} common.ErrorResponse
// @Router		/auth/refresh [post]
func (h *Handler) refreshTokens(c *gin.Context) {
	var dto RefreshDto
	if !common.BindJSON(c, &dto) {
		return
	}
	userId, next, err := h.refresh.Rotate(dto.RefreshToken)
	if err != nil {
		common.RespondAppError(c, err)
		return
	}
	u, err := h.svc.FindUser(userId)
	if err != nil {
		common.RespondAppError(c, err)
		return
	}
	h.respondWithTokens(c, u, next)
}

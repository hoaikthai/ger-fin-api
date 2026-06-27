package auth

import (
	"errors"

	"ger_fin_api/internal/common"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	g := rg.Group("/auth")
	g.POST("/register", h.register)
	g.POST("/login", h.login)
}

// @Summary		Register a new user
// @Description	Create a new user account with email and password
// @Tags		auth
// @Accept		json
// @Produce		json
// @Param		body body RegisterDto true "Registration details"
// @Success		201 {object} user.User
// @Failure		400 {object} common.ErrorResponse
// @Failure		409 {object} common.ErrorResponse
// @Router		/auth/register [post]
func (h *Handler) register(c *gin.Context) {
	var dto RegisterDto
	if !common.BindJSON(c, &dto) {
		return
	}

	u, err := h.svc.Register(dto)
	if err != nil {
		var appErr *common.AppError
		if errors.As(err, &appErr) {
			common.RespondError(c, appErr)
			return
		}
		common.RespondInternalError(c)
		return
	}

	c.JSON(201, u)
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

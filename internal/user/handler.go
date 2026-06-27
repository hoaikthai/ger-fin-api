package user

import (
	"errors"

	"ger_fin_api/internal/common"
	"ger_fin_api/internal/middleware"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, auth gin.HandlerFunc) {
	g := rg.Group("/users", auth)
	g.GET("/me", h.getProfile)
	g.PUT("/me", h.updateProfile)
	g.GET("/settings", h.getSettings)
	g.PATCH("/settings", h.updateSettings)
}

// @Summary		Get current user profile
// @Description	Returns the authenticated user's profile
// @Tags		users
// @Produce		json
// @Success		200 {object} User
// @Failure		401 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/users/me [get]
func (h *Handler) getProfile(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	u, err := h.svc.GetProfile(claims.Sub)
	if err != nil {
		var appErr *common.AppError
		if errors.As(err, &appErr) {
			common.RespondError(c, appErr)
			return
		}
		common.RespondInternalError(c)
		return
	}

	c.JSON(200, u)
}

// @Summary		Get user settings
// @Description	Returns the authenticated user's preferences; auto-creates with defaults on first access
// @Tags		users
// @Produce		json
// @Success		200 {object} UserSettings
// @Failure		401 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/users/settings [get]
func (h *Handler) getSettings(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	settings, err := h.svc.GetSettings(claims.Sub)
	if err != nil {
		common.RespondInternalError(c)
		return
	}

	c.JSON(200, settings)
}

// @Summary		Update user settings
// @Description	Partially update the authenticated user's preferences
// @Tags		users
// @Accept		json
// @Produce		json
// @Param		body body UpdateSettingsDto true "Settings fields to update"
// @Success		200 {object} UserSettings
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/users/settings [patch]
func (h *Handler) updateSettings(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	var dto UpdateSettingsDto
	if !common.BindJSON(c, &dto) {
		return
	}

	settings, err := h.svc.UpdateSettings(claims.Sub, dto)
	if err != nil {
		common.RespondInternalError(c)
		return
	}

	c.JSON(200, settings)
}

// @Summary		Update current user profile
// @Description	Update the authenticated user's first and last name
// @Tags		users
// @Accept		json
// @Produce		json
// @Param		body body UpdateProfileDto true "Profile fields to update"
// @Success		200 {object} User
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/users/me [put]
func (h *Handler) updateProfile(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	var dto UpdateProfileDto
	if !common.BindJSON(c, &dto) {
		return
	}

	u, err := h.svc.UpdateProfile(claims.Sub, dto)
	if err != nil {
		var appErr *common.AppError
		if errors.As(err, &appErr) {
			common.RespondError(c, appErr)
			return
		}
		common.RespondInternalError(c)
		return
	}

	c.JSON(200, u)
}

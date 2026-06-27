package transaction

import (
	"errors"
	"io"
	"strconv"

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
	// /transactions
	g := rg.Group("/transactions", auth)
	g.POST("", h.create)
	g.GET("", h.findAll)
	g.GET("/:id", h.findOne)
	g.PATCH("/:id", h.update)
	g.DELETE("/:id", h.remove)
	g.POST("/import", h.importCsv)

	// /transfers
	t := rg.Group("/transfers", auth)
	t.POST("", h.createTransfer)

	// /accounts/:id/transactions — registered alongside account routes
	a := rg.Group("/accounts", auth)
	a.GET("/:id/transactions", h.getByAccount)
}

// @Summary		Create transaction
// @Description	Create a new income or expense transaction
// @Tags		transactions
// @Accept		json
// @Produce		json
// @Param		body body CreateTransactionDto true "Transaction details"
// @Success		201 {object} Transaction
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/transactions [post]
func (h *Handler) create(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	var dto CreateTransactionDto
	if !common.BindJSON(c, &dto) {
		return
	}

	tx, err := h.svc.Create(dto, claims.Sub)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(201, tx)
}

// @Summary		List transactions
// @Description	Get all transactions for the authenticated user within the given time period
// @Tags		transactions
// @Produce		json
// @Param		period query string  false "Time period" Enums(day, week, month, quarter, year) default(month)
// @Param		offset query integer false "Period offset (negative = past)"                  default(0)
// @Success		200 {array} Transaction
// @Failure		401 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/transactions [get]
func (h *Handler) findAll(c *gin.Context) {
	claims := middleware.CurrentUser(c)
	period, offset := parseTimeQuery(c)

	txs, err := h.svc.FindAll(claims.Sub, period, offset)
	if err != nil {
		common.RespondInternalError(c)
		return
	}
	c.JSON(200, txs)
}

// @Summary		Get transaction
// @Description	Get a single transaction by ID
// @Tags		transactions
// @Produce		json
// @Param		id path string true "Transaction ID"
// @Success		200 {object} Transaction
// @Failure		401 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/transactions/{id} [get]
func (h *Handler) findOne(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	tx, err := h.svc.FindOne(c.Param("id"), claims.Sub)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(200, tx)
}

// @Summary		Update transaction
// @Description	Update a transaction's fields
// @Tags		transactions
// @Accept		json
// @Produce		json
// @Param		id   path string               true "Transaction ID"
// @Param		body body UpdateTransactionDto true "Fields to update"
// @Success		200 {object} Transaction
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/transactions/{id} [patch]
func (h *Handler) update(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	var dto UpdateTransactionDto
	if !common.BindJSON(c, &dto) {
		return
	}

	tx, err := h.svc.Update(c.Param("id"), dto, claims.Sub)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(200, tx)
}

// @Summary		Delete transaction
// @Description	Soft-delete a transaction by ID
// @Tags		transactions
// @Produce		json
// @Param		id path string true "Transaction ID"
// @Success		200 {object} common.MessageResponse
// @Failure		401 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/transactions/{id} [delete]
func (h *Handler) remove(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	if err := h.svc.Remove(c.Param("id"), claims.Sub); err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(200, gin.H{"message": "Transaction deleted successfully"})
}

// @Summary		Get transactions by account
// @Description	Get all transactions for a specific account within the given time period
// @Tags		accounts
// @Produce		json
// @Param		id     path  string  true  "Account ID"
// @Param		period query string  false "Time period" Enums(day, week, month, quarter, year) default(month)
// @Param		offset query integer false "Period offset (negative = past)"                  default(0)
// @Success		200 {array} Transaction
// @Failure		401 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/accounts/{id}/transactions [get]
func (h *Handler) getByAccount(c *gin.Context) {
	claims := middleware.CurrentUser(c)
	period, offset := parseTimeQuery(c)

	txs, err := h.svc.FindByAccount(c.Param("id"), claims.Sub, period, offset)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(200, txs)
}

// @Summary		Create transfer
// @Description	Transfer funds between two accounts (creates paired expense + income transactions)
// @Tags		transfers
// @Accept		json
// @Produce		json
// @Param		body body CreateTransferDto true "Transfer details"
// @Success		201 {object} TransferResult
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/transfers [post]
func (h *Handler) createTransfer(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	var dto CreateTransferDto
	if !common.BindJSON(c, &dto) {
		return
	}

	result, err := h.svc.Transfer(dto, claims.Sub)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(201, result)
}

// @Summary		Import transactions from CSV
// @Description	Bulk-import transactions from a CSV file. Required columns: Date, Category, Amount, Currency, Wallet. Optional: Note.
// @Tags		transactions
// @Accept		multipart/form-data
// @Produce		json
// @Param		file formData file true "CSV file"
// @Success		201 {object} ImportResult
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/transactions/import [post]
func (h *Handler) importCsv(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	file, err := c.FormFile("file")
	if err != nil {
		common.RespondError(c, common.NewBadRequest("No file uploaded"))
		return
	}

	contentType := file.Header.Get("Content-Type")
	if contentType != "text/csv" && !hasCSVExtension(file.Filename) {
		common.RespondError(c, common.NewBadRequest("File must be a CSV file"))
		return
	}

	f, err := file.Open()
	if err != nil {
		common.RespondInternalError(c)
		return
	}
	defer f.Close()

	buf, err := io.ReadAll(f)
	if err != nil {
		common.RespondInternalError(c)
		return
	}

	result, err := h.svc.ImportFromCsv(buf, claims.Sub)
	if err != nil {
		respondErr(c, err)
		return
	}
	c.JSON(201, result)
}

func parseTimeQuery(c *gin.Context) (common.TimePeriod, int) {
	period := common.TimePeriod(c.DefaultQuery("period", string(common.TimePeriodMonth)))
	offsetStr := c.DefaultQuery("offset", "0")
	offset, _ := strconv.Atoi(offsetStr)
	return period, offset
}

func hasCSVExtension(filename string) bool {
	if len(filename) < 4 {
		return false
	}
	return filename[len(filename)-4:] == ".csv"
}

func respondErr(c *gin.Context, err error) {
	var appErr *common.AppError
	if errors.As(err, &appErr) {
		common.RespondError(c, appErr)
		return
	}
	common.RespondInternalError(c)
}

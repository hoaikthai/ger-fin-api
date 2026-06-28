package transaction

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"sync"

	"ger_fin_api/internal/common"
	"ger_fin_api/internal/middleware"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const maxCSVBytes = 10 * 1024 * 1024 // 10 MB

type importJobState struct {
	mu     sync.Mutex
	status string // pending | running | done | failed
	result *ImportResult
	errMsg string
}

// CurrencyConverter converts an amount from one currency to another.
type CurrencyConverter interface {
	Convert(amount float64, from, to string) (float64, error)
}

// BaseCurrencyProvider returns the user's preferred base currency.
type BaseCurrencyProvider interface {
	GetBaseCurrency(userId string) (string, error)
}

type Handler struct {
	svc         *Service
	currencySvc CurrencyConverter
	userSvc     BaseCurrencyProvider
	jobs        sync.Map // string → *importJobState
}

func NewHandler(svc *Service, currencySvc CurrencyConverter, userSvc BaseCurrencyProvider) *Handler {
	return &Handler{svc: svc, currencySvc: currencySvc, userSvc: userSvc}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, auth gin.HandlerFunc) {
	g := rg.Group("/transactions", auth)
	g.POST("", h.create)
	g.GET("", h.findAll)
	g.GET("/:id", h.findOne)
	g.PATCH("/:id", h.update)
	g.DELETE("/:id", h.remove)
	g.POST("/import/validate", h.validateImportCsv)
	g.POST("/import", h.importCsv)
	g.GET("/import/jobs/:jobId", h.getImportJob)

	t := rg.Group("/transfers", auth)
	t.POST("", h.createTransfer)

	a := rg.Group("/accounts", auth)
	a.GET("/:id/transactions", h.getByAccount)
}

func (h *Handler) baseCurrency(userId string) string {
	if h.userSvc == nil {
		return "USD"
	}
	bc, err := h.userSvc.GetBaseCurrency(userId)
	if err != nil || bc == "" {
		return "USD"
	}
	return bc
}

func (h *Handler) toResponse(tx *Transaction, bc string) TransactionResponse {
	resp := TransactionResponse{Transaction: *tx, BaseCurrency: bc}
	if h.currencySvc != nil && tx.Account != nil {
		if converted, err := h.currencySvc.Convert(tx.Amount, tx.Account.Currency, bc); err == nil {
			resp.BaseAmount = &converted
		}
	}
	return resp
}

func (h *Handler) toResponseList(txs []Transaction, bc string) []TransactionResponse {
	out := make([]TransactionResponse, len(txs))
	for i := range txs {
		out[i] = h.toResponse(&txs[i], bc)
	}
	return out
}

// @Summary		Create transaction
// @Description	Create a new income or expense transaction
// @Tags		transactions
// @Accept		json
// @Produce		json
// @Param		body body CreateTransactionDto true "Transaction details"
// @Success		201 {object} TransactionResponse
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
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
		common.RespondAppError(c, err)
		return
	}

	bc := h.baseCurrency(claims.Sub)
	c.JSON(http.StatusCreated, h.toResponse(tx, bc))
}

// @Summary		List transactions
// @Description	Get all transactions for the authenticated user within the given time period
// @Tags		transactions
// @Produce		json
// @Param		period query string  false "Time period" Enums(day, week, month, quarter, year) default(month)
// @Param		offset query integer false "Period offset (negative = past)" default(0)
// @Success		200 {array} TransactionResponse
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

	bc := h.baseCurrency(claims.Sub)
	c.JSON(200, h.toResponseList(txs, bc))
}

// @Summary		Get transaction
// @Description	Get a single transaction by ID
// @Tags		transactions
// @Produce		json
// @Param		id path string true "Transaction ID"
// @Success		200 {object} TransactionResponse
// @Failure		401 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/transactions/{id} [get]
func (h *Handler) findOne(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	tx, err := h.svc.FindOne(c.Param("id"), claims.Sub)
	if err != nil {
		common.RespondAppError(c, err)
		return
	}

	bc := h.baseCurrency(claims.Sub)
	c.JSON(200, h.toResponse(tx, bc))
}

// @Summary		Update transaction
// @Description	Update a transaction's fields
// @Tags		transactions
// @Accept		json
// @Produce		json
// @Param		id   path string               true "Transaction ID"
// @Param		body body UpdateTransactionDto true "Fields to update"
// @Success		200 {object} TransactionResponse
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
		common.RespondAppError(c, err)
		return
	}

	bc := h.baseCurrency(claims.Sub)
	c.JSON(200, h.toResponse(tx, bc))
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
		common.RespondAppError(c, err)
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
// @Param		offset query integer false "Period offset (negative = past)" default(0)
// @Success		200 {array} TransactionResponse
// @Failure		401 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/accounts/{id}/transactions [get]
func (h *Handler) getByAccount(c *gin.Context) {
	claims := middleware.CurrentUser(c)
	period, offset := parseTimeQuery(c)

	txs, err := h.svc.FindByAccount(c.Param("id"), claims.Sub, period, offset)
	if err != nil {
		common.RespondAppError(c, err)
		return
	}

	bc := h.baseCurrency(claims.Sub)
	c.JSON(200, h.toResponseList(txs, bc))
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
		common.RespondAppError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// @Summary		Validate CSV before import
// @Description	Parse a CSV file and return all issues without importing anything.
// @Tags		transactions
// @Accept		multipart/form-data
// @Produce		json
// @Param		file            formData file   true  "CSV file"
// @Param		categoryMapping formData string false "JSON map of CSV category name to existing category ID"
// @Success		200 {object} ValidateResult
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/transactions/import/validate [post]
func (h *Handler) validateImportCsv(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	buf, categoryMapping, ok := readCsvForm(c)
	if !ok {
		return
	}

	result, err := h.svc.ValidateCsv(buf, claims.Sub, categoryMapping)
	if err != nil {
		common.RespondAppError(c, err)
		return
	}
	c.JSON(200, result)
}

// @Summary		Import transactions from CSV (async)
// @Description	Start a background import job from a CSV file. Poll GET /transactions/import/jobs/{jobId} for status.
// @Tags		transactions
// @Accept		multipart/form-data
// @Produce		json
// @Param		file            formData file   true  "CSV file"
// @Param		categoryMapping formData string false "JSON map of CSV category name to existing category ID"
// @Success		202 {object} ImportJobResponse
// @Failure		400 {object} common.ErrorResponse
// @Failure		401 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/transactions/import [post]
func (h *Handler) importCsv(c *gin.Context) {
	claims := middleware.CurrentUser(c)

	buf, categoryMapping, ok := readCsvForm(c)
	if !ok {
		return
	}

	jobId := uuid.New().String()
	job := &importJobState{status: "pending"}
	h.jobs.Store(jobId, job)

	go func() {
		job.mu.Lock()
		job.status = "running"
		job.mu.Unlock()

		result, err := h.svc.ImportFromCsv(buf, claims.Sub, categoryMapping)

		job.mu.Lock()
		defer job.mu.Unlock()
		if err != nil {
			job.status = "failed"
			job.errMsg = err.Error()
		} else {
			job.status = "done"
			job.result = result
		}
	}()

	c.JSON(202, ImportJobResponse{JobId: jobId})
}

// @Summary		Get import job status
// @Description	Poll the status of a background CSV import job.
// @Tags		transactions
// @Produce		json
// @Param		jobId path string true "Job ID returned by POST /transactions/import"
// @Success		200 {object} ImportJobStatus
// @Failure		401 {object} common.ErrorResponse
// @Failure		404 {object} common.ErrorResponse
// @Security	BearerAuth
// @Router		/transactions/import/jobs/{jobId} [get]
func (h *Handler) getImportJob(c *gin.Context) {
	jobId := c.Param("jobId")
	raw, ok := h.jobs.Load(jobId)
	if !ok {
		common.RespondError(c, common.NewNotFound("import job not found"))
		return
	}

	job := raw.(*importJobState)
	job.mu.Lock()
	defer job.mu.Unlock()

	c.JSON(200, ImportJobStatus{
		Status: job.status,
		Result: job.result,
		Error:  job.errMsg,
	})
}

func readCsvForm(c *gin.Context) ([]byte, map[string]string, bool) {
	file, err := c.FormFile("file")
	if err != nil {
		common.RespondError(c, common.NewBadRequest("No file uploaded"))
		return nil, nil, false
	}

	contentType := file.Header.Get("Content-Type")
	if contentType != "text/csv" && !hasCSVExtension(file.Filename) {
		common.RespondError(c, common.NewBadRequest("File must be a CSV file"))
		return nil, nil, false
	}

	f, err := file.Open()
	if err != nil {
		common.RespondInternalError(c)
		return nil, nil, false
	}
	defer f.Close()

	buf, err := io.ReadAll(io.LimitReader(f, maxCSVBytes+1))
	if err != nil {
		common.RespondInternalError(c)
		return nil, nil, false
	}
	if int64(len(buf)) > maxCSVBytes {
		common.RespondError(c, common.NewBadRequest("CSV file exceeds 10 MB size limit"))
		return nil, nil, false
	}

	var categoryMapping map[string]string
	if raw := c.PostForm("categoryMapping"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &categoryMapping); err != nil {
			common.RespondError(c, common.NewBadRequest("Invalid categoryMapping: must be a JSON object"))
			return nil, nil, false
		}
	}

	return buf, categoryMapping, true
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

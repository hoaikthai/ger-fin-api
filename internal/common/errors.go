package common

import (
	"errors"

	"github.com/gin-gonic/gin"
)

type AppError struct {
	Code    int
	Msg     interface{}
	ErrText string
}

func (e *AppError) Error() string {
	switch v := e.Msg.(type) {
	case string:
		return v
	default:
		return e.ErrText
	}
}

func NewBadRequest(message interface{}) *AppError {
	return &AppError{Code: 400, Msg: message, ErrText: "Bad Request"}
}

func NewUnauthorized(message string) *AppError {
	return &AppError{Code: 401, Msg: message, ErrText: "Unauthorized"}
}

func NewForbidden(message string) *AppError {
	return &AppError{Code: 403, Msg: message, ErrText: "Forbidden"}
}

func NewNotFound(message string) *AppError {
	return &AppError{Code: 404, Msg: message, ErrText: "Not Found"}
}

func NewConflict(message string) *AppError {
	return &AppError{Code: 409, Msg: message, ErrText: "Conflict"}
}

func RespondError(c *gin.Context, err *AppError) {
	c.JSON(err.Code, gin.H{
		"statusCode": err.Code,
		"message":    err.Msg,
		"error":      err.ErrText,
	})
}

// RespondAppError writes the appropriate HTTP response for any service-layer error.
// AppErrors produce their own status code; all other errors produce a 500.
func RespondAppError(c *gin.Context, err error) {
	var appErr *AppError
	if errors.As(err, &appErr) {
		RespondError(c, appErr)
		return
	}
	RespondInternalError(c)
}

// ErrorResponse is the standard error envelope returned by all endpoints.
type ErrorResponse struct {
	StatusCode int         `json:"statusCode"`
	Message    interface{} `json:"message"`
	Error      string      `json:"error"`
}

// MessageResponse is returned by delete endpoints.
type MessageResponse struct {
	Message string `json:"message"`
}

func RespondInternalError(c *gin.Context) {
	c.JSON(500, gin.H{
		"statusCode": 500,
		"message":    "Internal server error",
		"error":      "Internal Server Error",
	})
}

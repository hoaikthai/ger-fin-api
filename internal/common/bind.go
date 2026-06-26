package common

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

// BindJSON binds the request body and responds with NestJS-style validation
// errors if binding fails. Returns false if binding failed (response already written).
func BindJSON(c *gin.Context, dst interface{}) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		var ve validator.ValidationErrors
		if errors.As(err, &ve) {
			messages := make([]string, len(ve))
			for i, fe := range ve {
				messages[i] = formatFieldError(fe)
			}
			RespondError(c, NewBadRequest(messages))
		} else {
			RespondError(c, NewBadRequest(err.Error()))
		}
		return false
	}
	return true
}

func formatFieldError(fe validator.FieldError) string {
	field := strings.ToLower(fe.Field()[:1]) + fe.Field()[1:]
	switch fe.Tag() {
	case "required":
		return fmt.Sprintf("%s should not be empty", field)
	case "email":
		return fmt.Sprintf("%s must be an email", field)
	case "min":
		return fmt.Sprintf("%s must be longer than or equal to %s characters", field, fe.Param())
	case "max":
		return fmt.Sprintf("%s must be shorter than or equal to %s characters", field, fe.Param())
	case "uuid":
		return fmt.Sprintf("%s must be a UUID", field)
	case "oneof":
		return fmt.Sprintf("%s must be one of the following values: %s", field, fe.Param())
	case "len":
		return fmt.Sprintf("%s must be exactly %s characters long", field, fe.Param())
	case "gte":
		return fmt.Sprintf("%s must be greater than or equal to %s", field, fe.Param())
	case "lte":
		return fmt.Sprintf("%s must be less than or equal to %s", field, fe.Param())
	default:
		return fmt.Sprintf("%s is invalid", field)
	}
}

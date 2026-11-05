//go:build !no_bilibili && (!explicit_sites || site_bilibili)

package bili

import (
	"errors"
	"fmt"
)

var (
	ErrAuth        = errors.New("bilibili: authentication required")
	ErrRiskControl = errors.New("bilibili: risk control triggered")
)

type APIError struct {
	Code    int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("bilibili: code %d: %s", e.Code, e.Message)
}

func IsAuth(err error) bool { return errors.Is(err, ErrAuth) }
func IsRisk(err error) bool { return errors.Is(err, ErrRiskControl) }

func codeError(code int, msg string) error {
	switch code {
	case -101:
		return fmt.Errorf("%w: %s", ErrAuth, msg)
	case -352, -412, -509:
		return fmt.Errorf("%w: code %d %s", ErrRiskControl, code, msg)
	default:
		return &APIError{Code: code, Message: msg}
	}
}

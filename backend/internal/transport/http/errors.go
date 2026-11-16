package http

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	nethttp "net/http"
	"strings"

	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/domain"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/integration/access"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/integration/ml"
	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/service"
)

// apiError is the error envelope of contracts/api.yaml.
type apiError struct {
	ErrorCode       string         `json:"error_code"`
	Message         string         `json:"message"`
	OperationID     string         `json:"operation_id,omitempty"`
	OperationStatus string         `json:"operation_status,omitempty"`
	Details         map[string]any `json:"details,omitempty"`
}

const maxBodyBytes = 1 << 20

func writeJSON(log *slog.Logger, w nethttp.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Error("write response", "error", err)
	}
}

// decodeJSON rejects unknown fields and trailing payloads: a client mistake
// must not silently change the meaning of a trusted request.
func decodeJSON(w nethttp.ResponseWriter, r *nethttp.Request, dst any) error {
	r.Body = nethttp.MaxBytesReader(w, r.Body, maxBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return &domain.ValidationError{Field: "body", Message: "invalid json: " + err.Error()}
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return &domain.ValidationError{Field: "body", Message: "must contain exactly one JSON object"}
	}
	return nil
}

// mapError is the single translation from service/domain failures to the API
// error catalog. It intentionally never exposes internal error text to clients.
func mapError(log *slog.Logger, err error) (int, apiError) {
	var upstream *service.UpstreamError
	if errors.As(err, &upstream) {
		out := apiError{
			Message:         "Сервис доступов не подтвердил результат. Проверьте статус операции.",
			OperationID:     string(upstream.Operation),
			OperationStatus: string(upstream.Status),
		}
		if errors.Is(upstream.Kind, service.ErrUpstreamTimeout) {
			out.ErrorCode = "UPSTREAM_TIMEOUT"
			out.Message = "Сервис доступов не ответил вовремя. Заявка не отправляется повторно до сверки."
			return nethttp.StatusGatewayTimeout, out
		}
		out.ErrorCode = "UPSTREAM_ERROR"
		if errors.Is(upstream.Cause, access.ErrUnavailable) {
			return nethttp.StatusServiceUnavailable, out
		}
		return nethttp.StatusBadGateway, out
	}

	var draft *domain.DraftVersionConflictError
	if errors.As(err, &draft) {
		return nethttp.StatusConflict, apiError{
			ErrorCode: "DRAFT_VERSION_CONFLICT",
			Message:   "Черновик изменился. Проверьте актуальные параметры и подтвердите заново.",
			Details:   map[string]any{"expected": draft.Expected, "actual": draft.Actual},
		}
	}

	var denied *domain.AccessDeniedError
	if errors.As(err, &denied) {
		denial := service.RbacDenialFrom(denied)
		return nethttp.StatusForbidden, apiError{
			ErrorCode: "RBAC_ACCESS_DENIED",
			Message:   denial.Message,
		}
	}

	var validation *domain.ValidationError
	if errors.As(err, &validation) {
		return nethttp.StatusBadRequest, apiError{
			ErrorCode: "VALIDATION_FAILED",
			Message:   validation.Error(),
			Details:   map[string]any{"field": validation.Field},
		}
	}

	switch {
	case errors.Is(err, service.ErrTaskNotFound):
		return nethttp.StatusNotFound, apiError{
			ErrorCode: "OPERATION_NOT_FOUND",
			Message:   "Операция не найдена для текущего пользователя.",
		}
	case errors.Is(err, service.ErrIdempotencyInProgress):
		return nethttp.StatusConflict, apiError{
			ErrorCode: "VALIDATION_FAILED",
			Message:   "Запрос с этим Idempotency-Key уже выполняется.",
		}
	case errors.Is(err, service.ErrIdempotencyKeyReused):
		return nethttp.StatusConflict, apiError{
			ErrorCode: "VALIDATION_FAILED",
			Message:   "Idempotency-Key уже использован для другого черновика.",
		}
	case errors.Is(err, service.ErrOperationAlreadyFinished):
		return nethttp.StatusConflict, apiError{
			ErrorCode: "VALIDATION_FAILED",
			Message:   "Операция уже отправлена или завершена.",
		}
	case errors.Is(err, ml.ErrUnavailable):
		return nethttp.StatusGatewayTimeout, apiError{
			ErrorCode: "UPSTREAM_TIMEOUT",
			Message:   "AI-агент недоступен. Повторите сообщение позже.",
		}
	case errors.Is(err, ml.ErrBadRequest):
		return nethttp.StatusBadGateway, apiError{
			ErrorCode: "UPSTREAM_ERROR",
			Message:   "AI-агент отклонил запрос.",
		}
	case errors.Is(err, access.ErrTimeout):
		return nethttp.StatusGatewayTimeout, apiError{
			ErrorCode: "UPSTREAM_TIMEOUT",
			Message:   "Сервис доступов не ответил вовремя.",
		}
	case errors.Is(err, access.ErrUnavailable):
		return nethttp.StatusServiceUnavailable, apiError{
			ErrorCode: "UPSTREAM_ERROR",
			Message:   "Сервис доступов недоступен.",
		}
	case errors.Is(err, access.ErrRejected):
		return nethttp.StatusBadGateway, apiError{
			ErrorCode: "UPSTREAM_ERROR",
			Message:   "Сервис доступов отклонил заявку.",
		}
	case errors.Is(err, domain.ErrNotFound):
		return nethttp.StatusNotFound, apiError{
			ErrorCode: "OPERATION_NOT_FOUND",
			Message:   "Операция не найдена.",
		}
	case errors.Is(err, domain.ErrForbidden):
		return nethttp.StatusForbidden, apiError{
			ErrorCode: "RBAC_ACCESS_DENIED",
			Message:   "Доступ запрещён ролевой политикой.",
		}
	case errors.Is(err, domain.ErrConflict):
		return nethttp.StatusConflict, apiError{
			ErrorCode: "VALIDATION_FAILED",
			Message:   strings.TrimPrefix(err.Error(), "domain: conflict: "),
		}
	case errors.Is(err, domain.ErrValidation):
		return nethttp.StatusBadRequest, apiError{
			ErrorCode: "VALIDATION_FAILED",
			Message:   err.Error(),
		}
	}

	log.Error("unhandled request error", "error", err)
	return nethttp.StatusInternalServerError, apiError{
		ErrorCode: "INTERNAL_ERROR",
		Message:   "Внутренняя ошибка сервиса.",
	}
}

func respondError(w nethttp.ResponseWriter, log *slog.Logger, err error) {
	status, payload := mapError(log, err)
	writeJSON(log, w, status, payload)
}

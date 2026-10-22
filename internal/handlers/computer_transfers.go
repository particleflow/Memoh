package handlers

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/apperror"
	"github.com/felinics/memoh/internal/computertask"
)

const ComputerTransferPrefix = "/internal/computer-transfers"

// ExecutionTransferVerifier verifies a purpose-specific credential and its
// current revocation state. Implementations must not accept account JWTs,
// upstream Agent/OAuth tokens or self-reported request identity. No production
// implementation or credential issuer is supplied by this candidate.
type ExecutionTransferVerifier interface {
	Verify(context.Context, string) (computertask.VerifiedTransfer, error)
	Revalidate(context.Context, computertask.VerifiedTransfer) error
}

// ComputerTransferHandler mounts only existing file/association handlers behind
// a verified task principal. Application registration is explicitly opt-in.
// A missing verifier yields 503; it never falls back to the account JWT path.
type ComputerTransferHandler struct {
	verifier       ExecutionTransferVerifier
	deviceVerifier DeviceTransferVerifier
	base           *computertask.DeliveryService
	async          *computertask.AsyncDeliveryService
	files          *MessageHandler
}

func NewComputerTransferHandler(ctx context.Context, base *computertask.DeliveryService, files *MessageHandler, verifier ExecutionTransferVerifier, log *slog.Logger) (*ComputerTransferHandler, error) {
	h := &ComputerTransferHandler{base: base, files: files, verifier: verifier}
	if verifier == nil || base == nil || files == nil {
		return h, nil // Explicitly disabled, including when a valid account JWT exists.
	}
	async, err := computertask.NewAsyncDeliveryService(ctx, base, h.checkIdentity, log)
	if err != nil {
		return nil, err
	}
	h.async = async
	return h, nil
}

func (h *ComputerTransferHandler) Close() {
	if h != nil && h.async != nil {
		h.async.Close()
	}
}

func (h *ComputerTransferHandler) checkIdentity(ctx context.Context, id computertask.DeliveryIdentity) error {
	p, ok := computertask.VerifiedTransferFromContext(ctx)
	if !ok || p.Identity != id || !p.Valid(time.Now()) || h.verifier == nil && h.deviceVerifier == nil {
		return computertask.ErrDeliveryBinding
	}
	if err := h.revalidate(ctx, p); err != nil {
		if errors.Is(err, ErrTransferVerificationUnavailable) {
			return err
		}
		return computertask.ErrDenied // No raw verifier errors or credentials in HTTP/logs.
	}
	return h.base.CheckTransfer(ctx, false)
}

type transferHTTPKey struct{}

type transferHTTPContext struct {
	handler   *ComputerTransferHandler
	principal computertask.VerifiedTransfer
}

func taskTransfer(c echo.Context) *transferHTTPContext {
	scope, _ := c.Request().Context().Value(transferHTTPKey{}).(*transferHTTPContext)
	return scope
}

// Recheck before exposing a result/stream. An expired/revoked response can follow
// a durable file mutation; receipt/operation queries must reconcile that outcome.
func checkTaskTransferResponse(c echo.Context) error {
	if scope := taskTransfer(c); scope != nil {
		if err := scope.handler.checkIdentity(c.Request().Context(), scope.principal.Identity); err != nil {
			return deliveryHTTPError(err)
		}
	}
	return nil
}

func (h *ComputerTransferHandler) Register(e *echo.Echo) {
	e.Pre(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			p := echo.GetPath(c.Request())
			if p == ComputerTransferPrefix || strings.HasPrefix(p, ComputerTransferPrefix+"/") {
				c.Response().Header().Set(echo.HeaderCacheControl, "no-store")
			}
			return next(c)
		}
	})
	g := e.Group(ComputerTransferPrefix + "/:task_operation_id/:task_delivery_id")
	// Inner routes retain the frozen file protocol and existing client paths.
	file := "/bots/:bot_id/media"
	g.GET(file+"/limits", h.guard("limits", func(c echo.Context) error { return h.files.MediaUploadLimits(c) }))
	g.POST(file+"/uploads", h.guard("create", func(c echo.Context) error { return h.files.CreateMediaUpload(c) }))
	g.GET(file+"/uploads/:upload_id", h.guard("status", func(c echo.Context) error { return h.files.GetMediaUpload(c) }))
	g.PATCH(file+"/uploads/:upload_id", h.guard("append", func(c echo.Context) error { return h.files.AppendMediaUpload(c) }))
	g.POST(file+"/uploads/:upload_id/complete", h.guard("complete", func(c echo.Context) error { return h.files.CompleteMediaUpload(c) }))
	g.DELETE(file+"/uploads/:upload_id", h.guard("cancel-upload", func(c echo.Context) error { return h.files.CancelMediaUpload(c) }))
	g.GET(file+"/:content_hash", h.guard("read", func(c echo.Context) error { return h.files.ServeMedia(c) }))
	g.HEAD(file+"/:content_hash", h.guard("read", func(c echo.Context) error { return h.files.ServeMedia(c) }))
	delivery := "/internal/computer-deliveries/:operation_id/:delivery_id"
	association := NewAsyncComputerDeliveryHandler(h.async)
	g.POST(delivery+"/commit", h.guard("commit", association.commit))
	g.POST(delivery+"/resolve", h.guard("resolve", association.resolve))
}

func (h *ComputerTransferHandler) guard(action string, next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if h == nil || h.verifier == nil && h.deviceVerifier == nil || h.base == nil || h.async == nil || h.files == nil || h.files.mediaService == nil {
			return apperror.New(apperror.CodeHTTPServiceUnavailable, nil)
		}
		values := c.Request().Header.Values("Authorization")
		if len(values) != 1 || len(values[0]) > 4096 || !strings.HasPrefix(values[0], "Memoh-Transfer ") {
			return apperror.New(apperror.CodeHTTPUnauthorized, nil)
		}
		credential := strings.TrimPrefix(values[0], "Memoh-Transfer ")
		if credential == "" || strings.ContainsAny(credential, " \t\r\n") || c.QueryParam("token") != "" {
			return apperror.New(apperror.CodeHTTPUnauthorized, nil)
		}
		p, err := h.authenticate(c, credential)
		if errors.Is(err, ErrTransferVerificationUnavailable) {
			return apperror.New(apperror.CodeHTTPServiceUnavailable, nil)
		}
		if err != nil || !p.Valid(time.Now()) {
			return apperror.New(apperror.CodeHTTPUnauthorized, nil)
		}
		if p.Identity.OperationID != c.Param("task_operation_id") || p.DeliveryID != c.Param("task_delivery_id") ||
			c.Param("bot_id") != "" && c.Param("bot_id") != p.Identity.Binding.BotID ||
			c.Param("operation_id") != "" && c.Param("operation_id") != p.Identity.OperationID ||
			c.Param("delivery_id") != "" && c.Param("delivery_id") != p.DeliveryID {
			return deliveryHTTPError(computertask.ErrDeliveryBinding)
		}
		if action == "read" {
			if p.Reference.Direction != "download" || c.Param("content_hash") != p.Reference.ContentHash {
				return deliveryHTTPError(computertask.ErrDeliveryBinding)
			}
		} else if action != "commit" && action != "resolve" && action != "limits" && p.Reference.Direction != "upload" {
			return deliveryHTTPError(computertask.ErrDeliveryBinding)
		}
		bound, err := computertask.BindVerifiedTransfer(c.Request().Context(), p)
		if err != nil {
			return deliveryHTTPError(err)
		}
		if err := h.revalidate(bound, p); err != nil {
			if errors.Is(err, ErrTransferVerificationUnavailable) {
				return deliveryHTTPError(err)
			}
			return deliveryHTTPError(computertask.ErrDenied)
		}
		if err := h.base.ReserveTransfer(bound); err != nil {
			return deliveryHTTPError(err)
		}
		if err := h.base.CheckTransfer(bound, action == "read" || action == "create" || action == "append" || action == "complete"); err != nil {
			return deliveryHTTPError(err)
		}
		if upload := c.Param("upload_id"); upload != "" {
			id, err := h.base.TransferUploadID(bound)
			if err != nil || id != upload {
				return deliveryHTTPError(computertask.ErrDeliveryBinding)
			}
		}
		if action == "commit" && p.Reference.Direction == "upload" {
			id, err := h.base.TransferUploadID(bound)
			uploads := h.files.mediaService.Uploads()
			if err != nil || id == "" || uploads == nil {
				return deliveryHTTPError(computertask.ErrDeliveryBinding)
			}
			receipt, err := uploads.Get(bound, p.Identity.Binding.UserID, p.Identity.Binding.BotID, id)
			if err != nil || receipt.State != "ready" || receipt.ContentHash != p.Reference.ContentHash || receipt.SizeBytes != p.Reference.SizeBytes {
				return deliveryHTTPError(computertask.ErrDeliveryAsset)
			}
		}
		c.SetRequest(c.Request().WithContext(context.WithValue(bound, transferHTTPKey{}, &transferHTTPContext{handler: h, principal: p})))
		return next(c)
	}
}

func taskTransferCreate(c echo.Context, size int64, hash, mime string) (string, error) {
	scope := taskTransfer(c)
	if scope == nil {
		return "", nil
	}
	ref := scope.principal.Reference
	if ref.Direction != "upload" || ref.SizeBytes != size || ref.ContentHash != hash || ref.Mime != mime {
		return "", deliveryHTTPError(computertask.ErrDeliveryBinding)
	}
	id, err := scope.handler.base.ClaimTransferUpload(c.Request().Context())
	if errors.Is(err, computertask.ErrTransferCreateUnknown) {
		return "", apperror.New(apperror.CodeHTTPServiceUnavailable, nil)
	}
	if err != nil {
		return "", deliveryHTTPError(err)
	}
	return id, nil
}

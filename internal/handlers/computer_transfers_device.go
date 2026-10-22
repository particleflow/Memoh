package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/computertask"
)

var ErrTransferVerificationUnavailable = errors.New("device verification unavailable")

// DeviceTransferVerifier authenticates a reusable device credential, independent
// of any file or operation. The application uses the existing runtime registry.
// ConnectionID belongs to the existing authorized Agenvo connection; its token
// is NOT accepted as a Memoh credential. Revalidate must check current revocation.
type DeviceTransferVerifier interface {
	VerifyDevice(context.Context, string) (computertask.VerifiedTransferDevice, error)
	RevalidateDevice(context.Context, computertask.VerifiedTransferDevice) error
}

func NewDeviceComputerTransferHandler(ctx context.Context, base *computertask.DeliveryService, files *MessageHandler, verifier DeviceTransferVerifier, log *slog.Logger) (*ComputerTransferHandler, error) {
	h := &ComputerTransferHandler{base: base, files: files, deviceVerifier: verifier}
	if verifier == nil || base == nil || files == nil {
		return h, nil
	}
	var err error
	h.async, err = computertask.NewAsyncDeliveryService(ctx, base, h.checkIdentity, log)
	return h, err
}

// These are untrusted lookup selectors, never authenticated claims. The existing
// operation row, live target permissions, attempt/generation and reference CAS
// must all agree before any file action runs. They contain no credential.
type deviceTransferSelection struct {
	Identity  computertask.DeliveryIdentity `json:"identity"`
	Reference computertask.DeliveryRequest  `json:"reference"`
}

func (h *ComputerTransferHandler) authenticate(c echo.Context, credential string) (computertask.VerifiedTransfer, error) {
	ctx := c.Request().Context()
	if h.deviceVerifier == nil {
		return h.verifier.Verify(ctx, credential)
	}
	device, err := h.deviceVerifier.VerifyDevice(ctx, credential)
	if errors.Is(err, ErrTransferVerificationUnavailable) {
		return computertask.VerifiedTransfer{}, err
	}
	if err != nil || !device.Valid() {
		return computertask.VerifiedTransfer{}, computertask.ErrDenied
	}
	values := c.Request().Header.Values("Memoh-Transfer-Context")
	if len(values) != 1 || len(values[0]) > 6144 {
		return computertask.VerifiedTransfer{}, computertask.ErrDeliveryBinding
	}
	raw, err := base64.RawURLEncoding.DecodeString(values[0])
	if err != nil {
		return computertask.VerifiedTransfer{}, computertask.ErrDeliveryBinding
	}
	var selection deviceTransferSelection
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&selection) != nil || decoder.Decode(new(any)) != io.EOF {
		return computertask.VerifiedTransfer{}, computertask.ErrDeliveryBinding
	}
	selection.Identity.DeviceID, selection.Identity.InstanceID = device.DeviceID, device.InstanceID
	now := time.Now()
	return computertask.VerifiedTransfer{
		Device: device, Identity: selection.Identity, DeliveryID: c.Param("task_delivery_id"), Reference: selection.Reference,
		Audience: computertask.TransferAudience, CredentialID: device.CredentialID, IssuedAt: now, ExpiresAt: now.Add(15 * time.Minute),
	}, nil
}

func (h *ComputerTransferHandler) revalidate(ctx context.Context, p computertask.VerifiedTransfer) error {
	if h.deviceVerifier != nil {
		return h.deviceVerifier.RevalidateDevice(ctx, p.Device)
	}
	return h.verifier.Revalidate(ctx, p)
}

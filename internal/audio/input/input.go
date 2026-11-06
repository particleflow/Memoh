// Package input describes optional same-session text ingress without changing
// Twilight's fixed-text SpeechProvider interface. Providers opt in explicitly.
package input

import (
	"context"
	"errors"

	sdk "github.com/felinics/twilight/sdk"
)

var ErrUnsupported = errors.New("configured speech protocol does not support incremental input")

// Frame offsets count Unicode code points in the exact spoken-text projection.
// Sequence and Offset both start at zero. Final closes input, not audio output.
// Unknown or rejected writes must never be retried as another synthesis.
type Frame struct {
	Sequence uint64 `json:"sequence"`
	Offset   uint64 `json:"offset"`
	Text     string `json:"text"`
	Final    bool   `json:"final"`
}

type Session interface {
	Append(context.Context, Frame) error
	Audio() *sdk.SpeechStreamResult
	Cancel()
}

type Provider interface {
	OpenInput(context.Context, sdk.SpeechParams) (Session, error)
	InputContentType(sdk.SpeechParams) (string, error)
}

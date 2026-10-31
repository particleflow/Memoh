// Package agenvo holds the shared Windows computer task ledger and Herdr/Pi
// adapters used by native Memoh Runtime. The package name and legacy wire
// decoders remain for historical compatibility; production has no MCP caller.
package agenvo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrInvalidTarget = errors.New("invalid computer target")
	ErrUnavailable   = errors.New("computer target unavailable")
	ErrUnsupported   = errors.New("operation not supported by computer target")
	ErrRejected      = errors.New("computer operation rejected")
	ErrUnknown       = errors.New("computer operation outcome is unknown; do not repeat the operation")
	ErrTargetChanged = errors.New("computer target authorization changed")
)

// Target pins the selected Runtime and private native execution profile.
// Historical JSON fields retain their original names and fingerprint identity.
type Target struct {
	ConnectionID    string          `json:"connection_id"`
	DeviceID        string          `json:"device_id"`
	InstanceID      string          `json:"instance_id"`
	Session         string          `json:"session"`
	WorkDir         string          `json:"work_dir"`
	BashPath        string          `json:"bash_path"`
	Agent           AgentProfile    `json:"agent,omitempty,omitzero"`
	NativeRuntimeID string          `json:"native_runtime_id,omitempty"`
	HerdrCLI        string          `json:"herdr_cli,omitempty"`
	HerdrConfigRoot string          `json:"herdr_config_root,omitempty"`
	Producer        ProducerLocator `json:"producer,omitempty,omitzero"`
}

// AgentProfile is private deployment configuration, never model-supplied.
// No provider default is inferred and no credential is stored here.
type AgentProfile struct {
	// Empty is decoded only for historical profiles. Native configuration
	// requires pi-json-v1; no protocol fallback occurs.
	ExecutionMode string `json:"execution_mode,omitempty"`
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	ConfigDir     string `json:"config_dir"`
	Path          string `json:"path,omitempty"`
}

func (p AgentProfile) Validate() error {
	if p.ExecutionMode != "" && p.ExecutionMode != "pi-json-v1" {
		return ErrInvalidTarget
	}
	if p.Provider == "" || p.Model == "" || !windowsAbsolute.MatchString(p.ConfigDir) || len(p.ConfigDir) > 4096 || len(p.Provider) > 128 || len(p.Model) > 256 {
		return ErrInvalidTarget
	}
	if len(p.Path) > 4096 {
		return ErrInvalidTarget
	}
	for _, s := range []string{p.Provider, p.Model, p.ConfigDir, p.Path} {
		if strings.ContainsAny(s, "\x00\r\n") || strings.HasPrefix(s, "-") {
			return ErrInvalidTarget
		}
	}
	return nil
}

// Scope is always supplied by the authenticated Memoh execution boundary.
// TargetID is a Bot mount ID, not a user-supplied Agenvo device ID.
type Scope struct{ BotID, TargetID, UserID string }

// Resolve must check both the Bot grant and the caller's execution permission.
// It is called before every native operation, including reads and cleanup.
type Resolve func(context.Context, Scope) (Target, error)

// NativeCaller uses the selected, grant-checked Runtime's Exec transport. It
// returns a native receipt, never business completion or a retry instruction.
type NativeCaller func(context.Context, Scope, Target, string, map[string]any) (Receipt, error)

type Client struct {
	resolve    Resolve
	callNative NativeCaller
}

func NewClient(resolve Resolve, call NativeCaller) *Client {
	return &Client{resolve: resolve, callNative: call}
}

type Receipt struct {
	Execution string          `json:"execution"`
	RequestID string          `json:"requestId,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *struct {
		Code   string          `json:"code"`
		Native json.RawMessage `json:"native,omitempty"`
	} `json:"error,omitempty"`
}

func (c *Client) resolveTarget(ctx context.Context, scope Scope) (Target, error) {
	if c == nil || c.resolve == nil || scope.BotID == "" || scope.TargetID == "" {
		return Target{}, ErrInvalidTarget
	}
	t, err := c.resolve(ctx, scope)
	if err != nil {
		return Target{}, err
	}
	if t.ConnectionID == "" || t.DeviceID == "" || t.InstanceID == "" || t.Session == "" {
		return Target{}, ErrInvalidTarget
	}
	return t, nil
}

// native issues exactly one native call. A network failure is an unknown
// outcome, even for a canceled HTTP request. There is deliberately no retry.
func (c *Client) native(ctx context.Context, scope Scope, expected Target, method string, params map[string]any) (Receipt, error) {
	receipt, err := c.relay(ctx, scope, expected, method, params)
	if err != nil {
		return receipt, err
	}
	if receipt.Execution != "accepted" {
		return receipt, ErrUnknown
	}
	var envelope struct {
		NativeError json.RawMessage `json:"nativeError"`
	}
	if json.Unmarshal(receipt.Result, &envelope) != nil || (len(envelope.NativeError) > 0 && string(envelope.NativeError) != "null") {
		return receipt, ErrUnknown
	}
	return receipt, nil
}

func (c *Client) relay(ctx context.Context, scope Scope, expected Target, method string, params map[string]any) (Receipt, error) {
	current, err := c.resolveTarget(ctx, scope)
	if err != nil {
		return Receipt{}, err
	}
	if current != expected {
		return Receipt{}, ErrTargetChanged
	}
	if current.NativeRuntimeID == "" {
		return Receipt{}, ErrLegacyRetired
	}
	if c.callNative == nil {
		return Receipt{}, ErrUnavailable
	}
	return c.callNative(ctx, scope, current, method, params)
}

// Probe verifies the approved instance and the selected native service without
// creating a pane or accepting a stale search advertisement as proof of health.
func (c *Client) Probe(ctx context.Context, scope Scope) error {
	target, err := c.resolveTarget(ctx, scope)
	if err != nil {
		return err
	}
	receipt, err := c.native(ctx, scope, target, "workspace.list", map[string]any{"session": target.Session})
	if err != nil {
		return err
	}
	var listed struct {
		Workspaces []json.RawMessage `json:"workspaces"`
	}
	if resultBody(receipt, &listed) != nil || listed.Workspaces == nil {
		return ErrUnknown
	}
	return nil
}

func resultBody(receipt Receipt, out any) error {
	// Herdr CLI results have their own {result: ...} envelope inside the native
	// outcome. Do not search recursively for a convenient success field.
	var native struct {
		Result json.RawMessage `json:"result"`
	}
	if json.Unmarshal(receipt.Result, &native) != nil || len(native.Result) == 0 || string(native.Result) == "null" {
		return ErrUnknown
	}
	if err := json.Unmarshal(native.Result, out); err != nil {
		return fmt.Errorf("%w: invalid native result shape", ErrUnknown)
	}
	return nil
}

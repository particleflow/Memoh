package agenvo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/google/uuid"

	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/toolcontext"
)

const (
	Provider                 = "agenvo"
	CapabilityForegroundExec = "exec_foreground"
)

var ErrForbidden = errors.New("computer target access denied")

var ErrLegacyRetired = errors.New("legacy Agenvo targets are retired; use a native Memoh Runtime")

var ErrComputerTaskRequired = errors.New("computer commands require computer_task_submit so unresolved operations cannot be bypassed with exec")

type PermissionChecker interface {
	HasBotPermission(context.Context, string, string, string) (bool, error)
}

type Service struct {
	store       dbstore.AgenvoRuntimeStore
	permissions PermissionChecker
	callNative  NativeCaller
}

func (s *Service) SetNativeCaller(call NativeCaller) { s.callNative = call }

// NewService resolves native execution targets through the existing Bot mounts.
func NewService(store dbstore.AgenvoRuntimeStore, permissions PermissionChecker, call NativeCaller) *Service {
	return &Service{store: store, permissions: permissions, callNative: call}
}

func DecodeTarget(raw []byte) (Target, error) {
	var target Target
	if json.Unmarshal(raw, &target) != nil {
		return Target{}, ErrInvalidTarget
	}
	// Only the provider-specific decoder may select a native Runtime. An MCP
	// registration cannot inject a Runtime ID to switch authentication paths.
	if target.NativeRuntimeID != "" || target.HerdrCLI != "" || target.HerdrConfigRoot != "" || target.Producer != (ProducerLocator{}) {
		return Target{}, ErrInvalidTarget
	}
	return target, validateTarget(target)
}

// ValidateWorkDir permits host reservation only after the adapter's path
// contract is satisfied. It does not probe or mutate a remote filesystem.
func ValidateWorkDir(root, requested string) error {
	_, err := resolveWorkDir(root, requested)
	return err
}

var (
	sessionName     = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
	windowsAbsolute = regexp.MustCompile(`^(?:[A-Za-z]:[\\/]|\\\\[^\\]+\\[^\\]+)`)
	windowsBash     = regexp.MustCompile(`(?i)^[A-Z]:[\\/].*[\\/]bash\.exe$`)
)

func validateTarget(t Target) error {
	if t.Agent != (AgentProfile{}) {
		if err := t.Agent.Validate(); err != nil {
			return err
		}
	}
	if _, err := uuid.Parse(t.ConnectionID); err != nil {
		return ErrInvalidTarget
	}
	if len(t.DeviceID) == 0 || len(t.DeviceID) > 128 || len(t.InstanceID) == 0 || len(t.InstanceID) > 128 || !sessionName.MatchString(t.Session) {
		return ErrInvalidTarget
	}
	if !windowsAbsolute.MatchString(t.WorkDir) || len(t.WorkDir) > 4096 {
		return ErrInvalidTarget
	}
	if !windowsBash.MatchString(t.BashPath) || len(t.BashPath) > 4096 {
		return ErrInvalidTarget
	}
	for _, value := range []string{t.DeviceID, t.InstanceID, t.WorkDir, t.BashPath} {
		if strings.ContainsRune(value, 0) {
			return ErrInvalidTarget
		}
	}
	return nil
}

type registrationPin struct {
	Scope       Scope
	Fingerprint string
}
type registrationPinKey struct{}

// PinRegistration adds a ledger-owned constraint, never authorization. Every
// subsequent resolve still checks current grants, owner and connection access.
func PinRegistration(ctx context.Context, scope Scope, fingerprint string) context.Context {
	return context.WithValue(ctx, registrationPinKey{}, registrationPin{Scope: scope, Fingerprint: fingerprint})
}

func TargetFingerprint(t Target) string {
	raw, _ := json.Marshal(t)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// HistoricalTargetMatches compares a ledger snapshot with a currently authorized
// registration. Native profile changes do not invalidate persisted metadata on
// the same Runtime/instance. This is never an execution or filesystem permit.
// Legacy rows without a snapshot retain the exact fingerprint requirement.
func HistoricalTargetMatches(current Target, snapshot *Target, fingerprint string) bool {
	if fingerprint == "" {
		return false
	}
	if snapshot == nil {
		return TargetFingerprint(current) == fingerprint
	}
	if TargetFingerprint(*snapshot) != fingerprint {
		return false
	}
	if current.NativeRuntimeID == "" || snapshot.NativeRuntimeID == "" {
		return TargetFingerprint(current) == fingerprint
	}
	return current.NativeRuntimeID == snapshot.NativeRuntimeID && current.ConnectionID == snapshot.ConnectionID && current.DeviceID == snapshot.DeviceID && current.InstanceID == snapshot.InstanceID
}

// ResolveTarget exposes only a grant-checked registration to the host service.
func (s *Service) ResolveTarget(ctx context.Context, scope Scope) (Target, error) {
	t, _, err := s.resolve(ctx, scope)
	return t, err
}

// ResolveHistoricalTarget authorizes a stored identity without enabling execution.
func (s *Service) ResolveHistoricalTarget(ctx context.Context, scope Scope) (Target, error) {
	t, _, err := s.resolveForHistory(ctx, scope, true)
	return t, err
}

// AuthorizeOperationHistory checks the current Bot permission and selected scope.
// Callers must independently load the original owner/session/team-scoped ledger
// row. This grants no registration, filesystem, credential or execution access.
func (s *Service) AuthorizeOperationHistory(ctx context.Context, scope Scope) error {
	if err := toolcontext.CheckDelegationTarget(ctx, scope.BotID, scope.TargetID, "workspace_exec"); err != nil {
		return err
	}
	if err := toolcontext.CheckComputerTarget(ctx, scope.TargetID); err != nil {
		return err
	}
	if s == nil || s.permissions == nil || scope.UserID == "" {
		return ErrForbidden
	}
	allowed, err := s.permissions.HasBotPermission(ctx, scope.BotID, scope.UserID, "workspace_exec")
	if err != nil || !allowed {
		return ErrForbidden
	}
	return nil
}

func (s *Service) Client(ctx context.Context, scope Scope) (*Client, Target, error) {
	target, owner, err := s.resolve(ctx, scope)
	if err != nil {
		return nil, Target{}, err
	}
	c := NewClient(func(ctx context.Context, scope Scope) (Target, error) {
		t, currentOwner, err := s.resolve(ctx, scope)
		if err == nil && currentOwner != owner {
			return Target{}, ErrTargetChanged
		}
		return t, err
	}, s.callNative)
	return c, target, nil
}

func (s *Service) resolve(ctx context.Context, scope Scope) (Target, string, error) {
	return s.resolveForHistory(ctx, scope, false)
}

func (s *Service) resolveForHistory(ctx context.Context, scope Scope, history bool) (Target, string, error) {
	if err := toolcontext.CheckDelegationTarget(ctx, scope.BotID, scope.TargetID, "workspace_exec"); err != nil {
		return Target{}, "", err
	}
	if err := toolcontext.CheckComputerTarget(ctx, scope.TargetID); err != nil {
		return Target{}, "", err
	}
	if s == nil || s.store == nil || s.permissions == nil || scope.UserID == "" {
		return Target{}, "", ErrForbidden
	}
	allowed, err := s.permissions.HasBotPermission(ctx, scope.BotID, scope.UserID, "workspace_exec")
	if err != nil || !allowed {
		return Target{}, "", ErrForbidden
	}
	record, err := s.store.GetMount(ctx, scope.BotID, scope.TargetID)
	if err != nil || record.BotID != scope.BotID || record.ID != scope.TargetID || record.RuntimeRevoked || record.RuntimeUserID != record.BotOwnerUserID {
		return Target{}, "", ErrForbidden
	}
	// Historical registrations retain their stored identity, but cannot execute.
	if record.RuntimeProvider == Provider && !history {
		return Target{}, "", ErrLegacyRetired
	}
	target, err := DecodeRuntimeTarget(record.RuntimeProvider, record.RuntimeID, record.RuntimeProviderConfig)
	if err != nil {
		return Target{}, "", err
	}
	if record.RuntimeProvider == Provider {
		owned, ok := s.store.(interface {
			OwnedHTTPMCPConnectionBot(context.Context, string, string) (string, error)
		})
		if !ok || record.RuntimeID == "" || record.RuntimeUserID == "" {
			return Target{}, "", ErrForbidden
		}
		if _, err := owned.OwnedHTTPMCPConnectionBot(ctx, record.RuntimeUserID, target.ConnectionID); err != nil {
			return Target{}, "", ErrForbidden
		}
	}
	if pin, ok := ctx.Value(registrationPinKey{}).(registrationPin); ok && (pin.Scope != scope || pin.Fingerprint == "" || pin.Fingerprint != TargetFingerprint(target)) {
		return Target{}, "", ErrTargetChanged
	}
	return target, record.RuntimeUserID, nil
}

func (s *Service) Execute(ctx context.Context, req Request) (Result, error) {
	client, _, err := s.Client(ctx, req.Scope)
	if err != nil {
		return Result{State: "not_started"}, err
	}
	return NewExecutor(client).Execute(ctx, req)
}

package agenvo

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/toolcontext"
)

const (
	Provider                 = "agenvo"
	CapabilityForegroundExec = "exec_foreground"
)

var ErrForbidden = errors.New("agenvo target access denied")

// SourceCaller resolves the existing MCP connection under ownerID and uses its
// existing OAuth transport. A target never supplies an endpoint or a token.
type (
	SourceCaller      func(context.Context, string, string, string, map[string]any) (json.RawMessage, error)
	PermissionChecker interface {
		HasBotPermission(context.Context, string, string, string) (bool, error)
	}
)

type Service struct {
	store       dbstore.AgenvoRuntimeStore
	permissions PermissionChecker
	call        SourceCaller
}

// DiscoveredInstance contains only the fields needed to choose a target. It
// deliberately excludes instance context, provider credentials and scope data.
type DiscoveredInstance struct {
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
	InstanceID string `json:"instance_id"`
	Name       string `json:"name"`
	Online     bool   `json:"online"`
}

func (s *Service) Discover(ctx context.Context, ownerID, connectionID string) ([]DiscoveredInstance, error) {
	if s == nil || s.store == nil || s.call == nil {
		return nil, ErrUnavailable
	}
	if _, err := s.store.OwnedHTTPMCPConnectionBot(ctx, ownerID, connectionID); err != nil {
		return nil, ErrForbidden
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	raw, err := s.call(ctx, ownerID, connectionID, "search", map[string]any{"query": ""})
	if err != nil {
		return nil, ErrUnavailable
	}
	value, err := decodeToolResult(raw)
	if err != nil {
		return nil, err
	}
	var response struct {
		Execution string          `json:"execution"`
		Error     json.RawMessage `json:"error"`
		Result    struct {
			Items []struct {
				DeviceID    string `json:"deviceId"`
				DeviceLabel string `json:"deviceLabel"`
				InstanceID  string `json:"instanceId"`
				Label       string `json:"label"`
				Kind        string `json:"kind"`
				Online      bool   `json:"online"`
				Available   bool   `json:"available"`
			} `json:"items"`
		} `json:"result"`
	}
	if json.Unmarshal(value, &response) != nil || response.Execution != "accepted" || (len(response.Error) > 0 && string(response.Error) != "null") {
		return nil, ErrUnknown
	}
	items := []DiscoveredInstance{}
	for _, item := range response.Result.Items {
		if item.Kind == "herdr" {
			items = append(items, DiscoveredInstance{DeviceID: item.DeviceID, DeviceName: item.DeviceLabel, InstanceID: item.InstanceID, Name: item.Label, Online: item.Online && item.Available})
		}
	}
	return items, nil
}

func NewService(store dbstore.AgenvoRuntimeStore, permissions PermissionChecker, call SourceCaller) *Service {
	return &Service{store: store, permissions: permissions, call: call}
}

func DecodeTarget(raw []byte) (Target, error) {
	var target Target
	if json.Unmarshal(raw, &target) != nil {
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

// ResolveTarget exposes only a grant-checked registration to the host service.
func (s *Service) ResolveTarget(ctx context.Context, scope Scope) (Target, error) {
	t, _, err := s.resolve(ctx, scope)
	return t, err
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
	}, func(ctx context.Context, id, name string, args map[string]any) (json.RawMessage, error) {
		return s.call(ctx, owner, id, name, args)
	})
	return c, target, nil
}

func (s *Service) Register(ctx context.Context, ownerID, name string, target Target) (dbstore.UserRuntimeRecord, error) {
	if s == nil || s.store == nil || s.call == nil {
		return dbstore.UserRuntimeRecord{}, ErrUnavailable
	}
	if err := validateTarget(target); err != nil {
		return dbstore.UserRuntimeRecord{}, err
	}
	// Verify ownership before contacting the relay. Existing Bot membership or
	// administrator status alone does not authorize sharing somebody else's MCP.
	if _, err := s.store.OwnedHTTPMCPConnectionBot(ctx, ownerID, target.ConnectionID); err != nil {
		return dbstore.UserRuntimeRecord{}, ErrForbidden
	}
	raw, err := json.Marshal(target)
	if err != nil {
		return dbstore.UserRuntimeRecord{}, ErrInvalidTarget
	}
	if err := s.Probe(ctx, ownerID, raw); err != nil {
		return dbstore.UserRuntimeRecord{}, err
	}
	return s.store.CreateAgenvoRuntime(ctx, ownerID, name, raw)
}

func (s *Service) Probe(ctx context.Context, ownerID string, raw []byte) error {
	if s == nil || s.store == nil || s.call == nil {
		return ErrUnavailable
	}
	target, err := DecodeTarget(raw)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	client := NewClient(func(ctx context.Context, _ Scope) (Target, error) {
		if _, err := s.store.OwnedHTTPMCPConnectionBot(ctx, ownerID, target.ConnectionID); err != nil {
			return Target{}, ErrForbidden
		}
		return target, nil
	}, func(ctx context.Context, id, name string, args map[string]any) (json.RawMessage, error) {
		return s.call(ctx, ownerID, id, name, args)
	})
	return client.Probe(ctx, Scope{BotID: "account-probe", TargetID: "account-probe", UserID: ownerID})
}

func (s *Service) resolve(ctx context.Context, scope Scope) (Target, string, error) {
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
	if err != nil || record.BotID != scope.BotID || record.ID != scope.TargetID || record.RuntimeRevoked || record.RuntimeUserID != record.BotOwnerUserID || record.RuntimeProvider != Provider {
		return Target{}, "", ErrForbidden
	}
	target, err := DecodeTarget(record.RuntimeProviderConfig)
	if err != nil {
		return Target{}, "", err
	}
	if _, err := s.store.OwnedHTTPMCPConnectionBot(ctx, record.RuntimeUserID, target.ConnectionID); err != nil {
		return Target{}, "", ErrForbidden
	}
	return target, record.RuntimeUserID, nil
}

func (s *Service) Execute(ctx context.Context, req Request) (Result, error) {
	_, ownerID, err := s.resolve(ctx, req.Scope)
	if err != nil {
		return Result{State: "not_started"}, err
	}
	client := NewClient(func(ctx context.Context, scope Scope) (Target, error) {
		target, owner, err := s.resolve(ctx, scope)
		if err == nil && owner != ownerID {
			return Target{}, ErrTargetChanged
		}
		return target, err
	}, func(ctx context.Context, id, name string, args map[string]any) (json.RawMessage, error) {
		return s.call(ctx, ownerID, id, name, args)
	})
	return NewExecutor(client).Execute(ctx, req)
}

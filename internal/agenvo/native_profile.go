package agenvo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

var ErrNativeProfileConflict = errors.New("native Herdr profile changed; read current configuration")

// NativeHerdrProfile contains only owner-managed local program/config references.
// The Runtime identity is derived from its existing registry row, never supplied
// by a model or copied from an MCP connection. No credentials belong here.
type NativeHerdrProfile struct {
	InstanceID      string          `json:"instance_id" validate:"required"`
	Session         string          `json:"session" validate:"required"`
	WorkDir         string          `json:"work_dir" validate:"required"`
	BashPath        string          `json:"bash_path" validate:"required"`
	HerdrCLI        string          `json:"herdr_cli" validate:"required"`
	HerdrConfigRoot string          `json:"herdr_config_root" validate:"required"`
	Agent           AgentProfile    `json:"agent,omitempty,omitzero"`
	Producer        ProducerLocator `json:"producer,omitempty,omitzero"`
}

// ProducerLocator references the existing finite producer CLI and its official
// configuration/state directory pair. It contains neither credentials nor a
// new queue identity; the existing target fingerprint pins it to each task.
type ProducerLocator struct {
	ProgramPath string `json:"program_path" validate:"required"`
	ConfigDir   string `json:"config_dir" validate:"required"`
	StateDir    string `json:"state_dir" validate:"required"`
}

func (p ProducerLocator) validate() error {
	for _, path := range []string{p.ProgramPath, p.ConfigDir, p.StateDir} {
		if validateNativeLocalPath(path) != nil {
			return ErrInvalidTarget
		}
		// The existing producer rejects paths that Windows filepath.Clean
		// would rewrite. Reject these before accepting owner configuration.
		if strings.Contains(path, "/") || strings.Contains(path[3:], `\\`) || len(path) > 3 && strings.HasSuffix(path, `\`) {
			return ErrInvalidTarget
		}
	}
	if !strings.HasSuffix(strings.ToLower(p.ProgramPath), ".exe") {
		return ErrInvalidTarget
	}
	return nil
}

func validateNativeLocalPath(path string) error {
	if len(path) < 3 || len(path) > 4096 || !utf8.ValidString(path) || ((path[0] < 'A' || path[0] > 'Z') && (path[0] < 'a' || path[0] > 'z')) || path[1] != ':' || (path[2] != '\\' && path[2] != '/') || strings.TrimSpace(path) != path {
		return ErrInvalidTarget
	}
	for _, ch := range path[2:] {
		if ch < 32 || strings.ContainsRune(`<>:"|?*`, ch) {
			return ErrInvalidTarget
		}
	}
	for _, part := range strings.Split(strings.ReplaceAll(path, "/", `\`), `\`) {
		if part == "." || part == ".." {
			return ErrInvalidTarget
		}
	}
	return nil
}

type nativeProfileStore interface {
	OwnedNativeHerdrRuntime(context.Context, string, string) (dbstore.UserRuntimeRecord, error)
	ConfigureNativeHerdrRuntime(context.Context, string, string, dbstore.JSON, dbstore.JSON) (dbstore.UserRuntimeRecord, error)
}

func (p NativeHerdrProfile) target(runtime string) (Target, error) {
	id, err := uuid.Parse(runtime)
	if err != nil || id == uuid.Nil || id.String() != runtime {
		return Target{}, ErrInvalidTarget
	}
	t := Target{
		ConnectionID: runtime, DeviceID: runtime, NativeRuntimeID: runtime,
		InstanceID: p.InstanceID, Session: p.Session, WorkDir: p.WorkDir, BashPath: p.BashPath,
		HerdrCLI: p.HerdrCLI, HerdrConfigRoot: p.HerdrConfigRoot, Agent: p.Agent, Producer: p.Producer,
	}
	if err = validateTarget(t); err != nil {
		return Target{}, err
	}
	// Native CLI agent.start is synchronous and is not the old relay's starting
	// contract. Only the existing finite Pi receipt protocol is enabled here.
	if p.Agent != (AgentProfile{}) && p.Agent.ExecutionMode != "pi-json-v1" {
		return Target{}, ErrInvalidTarget
	}
	for _, path := range []string{p.HerdrCLI, p.HerdrConfigRoot} {
		// Match the Exec adapter's local Win32 executable/path contract before
		// a task can claim its durable slot. UNC and shell wrappers are not used.
		if validateNativeLocalPath(path) != nil {
			return Target{}, ErrInvalidTarget
		}
	}
	if p.Producer != (ProducerLocator{}) && p.Producer.validate() != nil {
		return Target{}, ErrInvalidTarget
	}
	if !strings.HasSuffix(strings.ToLower(p.HerdrCLI), ".exe") {
		return Target{}, ErrInvalidTarget
	}
	rootParts := strings.Split(strings.TrimRight(strings.ReplaceAll(p.HerdrConfigRoot, "/", `\`), `\`), `\`)
	if !strings.EqualFold(rootParts[len(rootParts)-1], "herdr") {
		return Target{}, ErrInvalidTarget
	}
	return t, nil
}

func DecodeNativeTarget(runtime string, raw []byte) (Target, error) {
	var config struct {
		Herdr json.RawMessage `json:"herdr"`
	}
	if json.Unmarshal(raw, &config) != nil || len(config.Herdr) == 0 || string(config.Herdr) == "null" {
		return Target{}, ErrInvalidTarget
	}
	var p NativeHerdrProfile
	d := json.NewDecoder(bytes.NewReader(config.Herdr))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil {
		return Target{}, ErrInvalidTarget
	}
	return p.target(runtime)
}

func DecodeRuntimeTarget(provider, runtime string, raw []byte) (Target, error) {
	switch provider {
	case Provider:
		return DecodeTarget(raw)
	case "memoh":
		return DecodeNativeTarget(runtime, raw)
	default:
		return Target{}, ErrInvalidTarget
	}
}

// ConfigureNativeHerdr updates only an already activated, owned Runtime. It
// never enrolls credentials, changes grants, probes a different PC, or starts
// Herdr. The compare-and-swap preserves concurrent configuration and pause gates.
func (s *Service) ConfigureNativeHerdr(ctx context.Context, owner, id string, p NativeHerdrProfile) (dbstore.UserRuntimeRecord, error) {
	if s == nil {
		return dbstore.UserRuntimeRecord{}, ErrUnavailable
	}
	store, ok := s.store.(nativeProfileStore)
	if !ok {
		return dbstore.UserRuntimeRecord{}, ErrUnavailable
	}
	if _, err := p.target(id); err != nil {
		return dbstore.UserRuntimeRecord{}, err
	}
	row, err := store.OwnedNativeHerdrRuntime(ctx, owner, id)
	if err != nil {
		return dbstore.UserRuntimeRecord{}, ErrForbidden
	}
	raw, err := replaceRegistrationField(row.ProviderConfig, "herdr", p)
	if err != nil {
		return dbstore.UserRuntimeRecord{}, err
	}
	updated, err := store.ConfigureNativeHerdrRuntime(ctx, owner, id, row.ProviderConfig, raw)
	if errors.Is(err, db.ErrNotFound) {
		return dbstore.UserRuntimeRecord{}, ErrNativeProfileConflict
	}
	return updated, err
}

package userruntime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path"
	"strings"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

var (
	ErrWorkspaceStorageDisabled  = errors.New("NAS workspace storage is not configured")
	ErrWorkspaceShareUnavailable = errors.New("existing LAN share is not available to this computer")
	ErrWorkspaceStoragePath      = errors.New("invalid NAS workspace directory")
	ErrWorkspaceStorageConflict  = errors.New("computer configuration changed")
)

type workspaceStorage struct {
	root, share string
}

type workspaceStorageStore interface {
	OwnedNativeHerdrRuntime(context.Context, string, string) (dbstore.UserRuntimeRecord, error)
	ConfigureNativeHerdrRuntime(context.Context, string, string, dbstore.JSON, dbstore.JSON) (dbstore.UserRuntimeRecord, error)
}

type WorkspaceDirectory struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type WorkspaceStorage struct {
	Path         string               `json:"path"`
	SelectedPath string               `json:"selected_path"`
	WindowsPath  string               `json:"windows_path"`
	Directories  []WorkspaceDirectory `json:"directories"`
}

type WorkspaceStorageRequest struct {
	Path string `json:"path"`
}

// SetWorkspaceStorage configures a mapping to an already existing LAN share.
// It never discovers, mounts, authenticates or creates a share. The destination
// is supplied by server configuration, never by a model or directory request.
// Readability of this configured LAN share is checked by the selected computer;
// it is not a claim about a public connection's physical network route.
func (s *Service) SetWorkspaceStorage(root, share string) {
	s.workspaceStorage = nil
	share = strings.TrimRight(strings.ReplaceAll(strings.TrimSpace(share), "/", `\`), `\`)
	parts := strings.Split(strings.TrimPrefix(share, `\\`), `\`)
	if strings.TrimSpace(root) == "" || !strings.HasPrefix(share, `\\`) || len(parts) < 2 || parts[1] == "" {
		return
	}
	if strings.ContainsAny(parts[0], ":\x00\r\n") || parts[0] == "" || parts[0] == "." || parts[0] == ".." || parts[0] == "?" {
		return
	}
	for _, part := range parts[1:] {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, ":\x00\r\n") {
			return
		}
	}
	if agenvo.ValidateWorkDir(share, ".") != nil {
		return
	}
	s.workspaceStorage = &workspaceStorage{root: root, share: share}
}

func storageRelative(raw string) (string, error) {
	if raw == "" || raw == "." {
		return ".", nil
	}
	if path.IsAbs(raw) || strings.ContainsAny(raw, `\:`+"\x00\r\n") || path.Clean(raw) != raw || raw == ".." || strings.HasPrefix(raw, "../") {
		return "", ErrWorkspaceStoragePath
	}
	return raw, nil
}

func (s *Service) storageOwner(ctx context.Context, owner, id string) (dbstore.UserRuntimeRecord, workspaceStorageStore, error) {
	if _, err := uuid.Parse(id); err != nil {
		return dbstore.UserRuntimeRecord{}, nil, ErrInvalidInput
	}
	store, ok := s.store.(workspaceStorageStore)
	if !ok {
		return dbstore.UserRuntimeRecord{}, nil, ErrWorkspaceStorageDisabled
	}
	row, err := store.OwnedNativeHerdrRuntime(ctx, owner, id)
	if err != nil {
		return dbstore.UserRuntimeRecord{}, nil, err
	}
	if row.ID != id || row.UserID != owner || row.Provider != "memoh" {
		return dbstore.UserRuntimeRecord{}, nil, db.ErrNotFound
	}
	return row, store, nil
}

// storageConnection checks the current owner's existing reverse RPC only.
// Online alone is insufficient: the existing private share must be readable.
func (s *Service) storageConnection(ctx context.Context, id string) (*Connection, error) {
	c, err := s.storageRuntimeConnection(id)
	if err != nil {
		return nil, err
	}
	entry, err := c.Client.Stat(ctx, s.workspaceStorage.share)
	if err != nil || entry == nil || !entry.GetIsDir() {
		return nil, ErrWorkspaceShareUnavailable
	}
	return c, nil
}

func (s *Service) storageRuntimeConnection(id string) (*Connection, error) {
	if s.workspaceStorage == nil {
		return nil, ErrWorkspaceStorageDisabled
	}
	c, ok := s.Connection(id)
	if !ok || c.Client == nil || c.Info.OS != "win32" {
		return nil, ErrWorkspaceShareUnavailable
	}
	return c, nil
}

func (s *Service) storageWindowsPath(id, relative string) string {
	result := s.workspaceStorage.share + `\` + id
	if relative != "." {
		result += `\` + strings.ReplaceAll(relative, "/", `\`)
	}
	return result
}

func (s *Service) validateStoragePath(id, relative string) error {
	if s.workspaceStorage == nil {
		return ErrWorkspaceStorageDisabled
	}
	if agenvo.ValidateWorkDir(s.workspaceStorage.share, s.storageWindowsPath(id, relative)) != nil {
		return ErrWorkspaceStoragePath
	}
	return nil
}

// Open one computer's directory, anchoring descendants to that directory's
// file descriptor. A link at the computer boundary cannot select another root.
func (s *Service) storageRoot(id string, create bool) (*os.Root, error) {
	root, err := os.OpenRoot(s.workspaceStorage.root)
	if err != nil {
		return nil, ErrWorkspaceStorageDisabled
	}
	defer func() { _ = root.Close() }()
	if create {
		if err = root.Mkdir(id, 0o750); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
	}
	before, err := root.Lstat(id)
	if err != nil {
		return nil, err
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, ErrWorkspaceStoragePath
	}
	computer, err := root.OpenRoot(id)
	if err != nil {
		return nil, err
	}
	after, err := computer.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		_ = computer.Close()
		return nil, ErrWorkspaceStoragePath
	}
	return computer, nil
}

func storageSelection(raw dbstore.JSON) (string, error) {
	var value struct {
		WorkspaceStorage WorkspaceStorageRequest `json:"workspace_storage"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return "", ErrWorkspaceStoragePath
	}
	return storageRelative(value.WorkspaceStorage.Path)
}

// ConfiguredWorkspaceDirectory reads an explicitly saved owned NAS default.
// It changes no task/profile root and performs no duplicate connection probe.
// An absent selection preserves existing local defaults.
func (s *Service) ConfiguredWorkspaceDirectory(ctx context.Context, owner, id string) (string, error) {
	row, _, err := s.storageOwner(ctx, owner, id)
	if err != nil {
		return "", err
	}
	if len(row.ProviderConfig) == 0 || string(row.ProviderConfig) == "null" {
		return "", nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(row.ProviderConfig, &fields) != nil {
		return "", ErrWorkspaceStoragePath
	}
	if _, selected := fields["workspace_storage"]; !selected {
		return "", nil
	}
	relative, err := storageSelection(row.ProviderConfig)
	if err != nil {
		return "", err
	}
	if err = s.validateStoragePath(id, relative); err != nil {
		return "", err
	}
	return s.storageWindowsPath(id, relative), nil
}

// SelectedWorkspaceDirectory verifies the existing share at workspace resolution,
// not again for each byte transfer or once more in the task-default resolver.
func (s *Service) SelectedWorkspaceDirectory(ctx context.Context, owner, id string) (string, error) {
	selected, err := s.ConfiguredWorkspaceDirectory(ctx, owner, id)
	if err != nil || selected == "" {
		return selected, err
	}
	c, err := s.storageRuntimeConnection(id)
	if err != nil {
		return "", err
	}
	entry, err := c.Client.Stat(ctx, selected)
	if err != nil || entry == nil || !entry.GetIsDir() {
		return "", ErrWorkspaceShareUnavailable
	}
	if current, ok := s.Connection(id); !ok || current != c {
		return "", ErrRuntimeConnectionNotReady
	}
	return selected, nil
}

// WorkspaceDirectories is read-only. A missing default directory is returned
// empty and is not created by opening settings. No Bot context is synthesized.
func (s *Service) WorkspaceDirectories(ctx context.Context, owner, id, raw string) (WorkspaceStorage, error) {
	row, _, err := s.storageOwner(ctx, owner, id)
	if err != nil {
		return WorkspaceStorage{}, err
	}
	selected, err := storageSelection(row.ProviderConfig)
	if err != nil {
		return WorkspaceStorage{}, err
	}
	relative, err := storageRelative(raw)
	if err != nil {
		return WorkspaceStorage{}, err
	}
	if err = s.validateStoragePath(id, relative); err != nil {
		return WorkspaceStorage{}, err
	}
	if _, err = s.storageConnection(ctx, id); err != nil {
		return WorkspaceStorage{}, err
	}
	out := WorkspaceStorage{Path: relative, SelectedPath: selected, WindowsPath: s.storageWindowsPath(id, selected), Directories: []WorkspaceDirectory{}}
	root, err := s.storageRoot(id, false)
	if errors.Is(err, os.ErrNotExist) && relative == "." {
		return out, nil
	}
	if err != nil {
		return WorkspaceStorage{}, err
	}
	defer func() { _ = root.Close() }()
	dir, err := root.Open(relative)
	if err != nil {
		return WorkspaceStorage{}, err
	}
	defer func() { _ = dir.Close() }()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return WorkspaceStorage{}, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			out.Directories = append(out.Directories, WorkspaceDirectory{Name: entry.Name(), Path: path.Join(relative, entry.Name())})
		}
	}
	return out, nil
}

// CreateWorkspaceDirectory creates below this computer's existing NAS root;
// it never changes a task, session, Herdr profile, local default or credential.
func (s *Service) CreateWorkspaceDirectory(ctx context.Context, owner, id, raw string) error {
	if _, _, err := s.storageOwner(ctx, owner, id); err != nil {
		return err
	}
	return s.createWorkspaceDirectory(ctx, id, raw)
}

func (s *Service) createWorkspaceDirectory(ctx context.Context, id, raw string) error {
	relative, err := storageRelative(raw)
	if err != nil {
		return err
	}
	if err = s.validateStoragePath(id, relative); err != nil {
		return err
	}
	connection, err := s.storageConnection(ctx, id)
	if err != nil {
		return err
	}
	root, err := s.storageRoot(id, true)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if relative != "." {
		if err = root.Mkdir(relative, 0o750); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	info, err := root.Stat(relative)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return ErrWorkspaceStoragePath
	}
	entry, err := connection.Client.Stat(ctx, s.storageWindowsPath(id, relative))
	if err != nil || entry == nil || !entry.GetIsDir() {
		return ErrWorkspaceShareUnavailable
	}
	current, ok := s.Connection(id)
	if !ok || current != connection {
		return ErrRuntimeConnectionNotReady
	}
	return nil
}

func (s *Service) SelectWorkspaceDirectory(ctx context.Context, owner, id, raw string) error {
	row, store, err := s.storageOwner(ctx, owner, id)
	if err != nil {
		return err
	}
	relative, err := storageRelative(raw)
	if err != nil {
		return err
	}
	if err = s.createWorkspaceDirectory(ctx, id, relative); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(row.ProviderConfig, &fields) != nil || fields == nil {
		return ErrWorkspaceStoragePath
	}
	fields["workspace_storage"], err = json.Marshal(WorkspaceStorageRequest{Path: relative})
	if err != nil {
		return err
	}
	next, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	_, err = store.ConfigureNativeHerdrRuntime(ctx, owner, id, row.ProviderConfig, next)
	if errors.Is(err, db.ErrNotFound) {
		return ErrWorkspaceStorageConflict
	}
	return err
}

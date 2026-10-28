//go:build linux

package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/felinics/memoh/internal/config"
	ctr "github.com/felinics/memoh/internal/container"
)

const runtimeDirectoryTestBotID = "12345678-1234-4234-8234-123456789abc"

type runtimeDirectoryDeleteService struct {
	legacyRouteTestService
	deleteErr error
}

func (s *runtimeDirectoryDeleteService) DeleteContainer(ctx context.Context, id string, opts *ctr.DeleteContainerOptions) error {
	if s.deleteErr != nil {
		s.deleteCalls++
		return s.deleteErr
	}
	return s.legacyRouteTestService.DeleteContainer(ctx, id, opts)
}

func newRuntimeDirectoryFixture(t *testing.T) (*Manager, *runtimeDirectoryDeleteService, string) {
	t.Helper()
	svc := &runtimeDirectoryDeleteService{}
	m := newLegacyRouteTestManager(t, svc, config.WorkspaceConfig{DataRoot: t.TempDir()})
	dir := m.socketDir(runtimeDirectoryTestBotID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	return m, svc, dir
}

func requireRuntimePath(t *testing.T, path string, present bool) {
	t.Helper()
	_, err := os.Lstat(path)
	if present && err != nil || !present && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("path %q: present=%t, err=%v", path, present, err)
	}
}

func TestCleanupBotContainerRemovesOnlyItsEmptyRuntimeDirectory(t *testing.T) {
	m, _, dir := newRuntimeDirectoryFixture(t)
	sibling := m.socketDir("87654321-4321-4321-8321-cba987654321")
	if err := os.Mkdir(sibling, 0o750); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(m.dataRoot(), "backups", runtimeDirectoryTestBotID+".tar.gz")
	if err := os.MkdirAll(filepath.Dir(backup), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("preserved data"), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := m.CleanupBotContainer(context.Background(), runtimeDirectoryTestBotID, false); err != nil {
			t.Fatal(err)
		}
	}
	requireRuntimePath(t, dir, false)
	for _, path := range []string{m.dataRoot(), filepath.Dir(dir), sibling, backup} {
		requireRuntimePath(t, path, true)
	}
}

func TestCleanupBotContainerMissingRuntimeStillCleansDirectory(t *testing.T) {
	m, svc, dir := newRuntimeDirectoryFixture(t)
	svc.deleteErr = ctr.ErrNotFound
	for range 2 {
		if err := m.CleanupBotContainer(context.Background(), runtimeDirectoryTestBotID, false); err != nil {
			t.Fatal(err)
		}
	}
	requireRuntimePath(t, dir, false)
}

func TestCleanupBotContainerRetainsNonemptyDirectoryAndRetries(t *testing.T) {
	m, svc, dir := newRuntimeDirectoryFixture(t)
	svc.deleteErr = ctr.ErrNotFound
	file := filepath.Join(dir, "retained.txt")
	if err := os.WriteFile(file, []byte("do not delete"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A not-found runtime must not mask a failure to remove the host directory.
	if err := m.CleanupBotContainer(context.Background(), runtimeDirectoryTestBotID, false); !errors.Is(err, unix.ENOTEMPTY) {
		t.Fatalf("cleanup = %v, want ENOTEMPTY", err)
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "do not delete" {
		t.Fatalf("unexpected data change: %q, %v", data, err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := m.CleanupBotContainer(context.Background(), runtimeDirectoryTestBotID, false); err != nil {
		t.Fatal(err)
	}
	requireRuntimePath(t, dir, false)
}

func TestCleanupBotContainerRuntimeFailureKeepsDirectory(t *testing.T) {
	m, svc, dir := newRuntimeDirectoryFixture(t)
	svc.deleteErr = ctr.ErrUnavailable
	if err := m.CleanupBotContainer(context.Background(), runtimeDirectoryTestBotID, false); !errors.Is(err, ctr.ErrUnavailable) {
		t.Fatalf("cleanup = %v, want runtime failure", err)
	}
	requireRuntimePath(t, dir, true)
	svc.deleteErr = nil
	if err := m.CleanupBotContainer(context.Background(), runtimeDirectoryTestBotID, false); err != nil {
		t.Fatal(err)
	}
	requireRuntimePath(t, dir, false)
}

func TestCleanupBotContainerPreserveFailureKeepsDirectory(t *testing.T) {
	svc := &unreadableBridgeService{}
	svc.created = true
	svc.container = ctr.ContainerInfo{ID: ContainerPrefix + runtimeDirectoryTestBotID}
	m := newLegacyRouteTestManager(t, svc, config.WorkspaceConfig{DataRoot: t.TempDir()})
	dir := m.socketDir(runtimeDirectoryTestBotID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := m.CleanupBotContainer(context.Background(), runtimeDirectoryTestBotID, true); !errors.Is(err, errPreserveData) {
		t.Fatalf("cleanup = %v, want preserve failure", err)
	}
	if svc.deleteCalls != 0 {
		t.Fatal("runtime deleted after failed data export")
	}
	requireRuntimePath(t, dir, true)
}

func TestRuntimeDirectoryCleanupRefusesSymlinks(t *testing.T) {
	for _, location := range []string{"leaf", "run", "data-root", "ancestor"} {
		t.Run(location, func(t *testing.T) {
			m, _, dir := newRuntimeDirectoryFixture(t)
			victim := filepath.Join(t.TempDir(), "untouched")
			if err := os.Mkdir(victim, 0o750); err != nil {
				t.Fatal(err)
			}
			link := dir
			switch location {
			case "run":
				link = filepath.Dir(dir)
				if err := os.Mkdir(filepath.Join(victim, runtimeDirectoryTestBotID), 0o750); err != nil {
					t.Fatal(err)
				}
			case "data-root":
				link = m.dataRoot()
				if err := os.MkdirAll(filepath.Join(victim, "run", runtimeDirectoryTestBotID), 0o750); err != nil {
					t.Fatal(err)
				}
			case "ancestor":
				link = filepath.Join(t.TempDir(), "alias")
				victim = m.dataRoot()
				m.cfg.DataRoot = filepath.Join(link, "nested")
				if err := os.MkdirAll(filepath.Join(victim, "nested", "run", runtimeDirectoryTestBotID), 0o750); err != nil {
					t.Fatal(err)
				}
			}
			if location != "ancestor" {
				if err := os.Rename(link, link+"-original"); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(victim, link); err != nil {
				t.Fatal(err)
			}
			if err := m.cleanupBotRunDirectory(runtimeDirectoryTestBotID); err == nil {
				t.Fatal("accepted symlink in the runtime directory path")
			}
			info, err := os.Lstat(link)
			if err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("symlink was changed: %v", err)
			}
			requireRuntimePath(t, victim, true)
			if location == "run" {
				requireRuntimePath(t, filepath.Join(victim, runtimeDirectoryTestBotID), true)
			} else if location == "data-root" {
				requireRuntimePath(t, filepath.Join(victim, "run", runtimeDirectoryTestBotID), true)
			} else if location == "ancestor" {
				requireRuntimePath(t, filepath.Join(victim, "nested", "run", runtimeDirectoryTestBotID), true)
			}
		})
	}
}

func TestRuntimeDirectoryCleanupRefusesFilesAndInvalidIDs(t *testing.T) {
	for _, kind := range []string{"file", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			m, _, dir := newRuntimeDirectoryFixture(t)
			if err := os.Remove(dir); err != nil {
				t.Fatal(err)
			}
			var err error
			if kind == "file" {
				err = os.WriteFile(dir, []byte("keep"), 0o600)
			} else {
				err = unix.Mkfifo(dir, 0o600)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := m.cleanupBotRunDirectory(runtimeDirectoryTestBotID); err == nil {
				t.Fatal("removed a non-directory")
			}
			requireRuntimePath(t, dir, true)
		})
	}
	m, _, dir := newRuntimeDirectoryFixture(t)
	for _, id := range []string{"", ".", "..", "../" + runtimeDirectoryTestBotID, "run", "bot-name", "12345678-1234-4234-8234-123456789ABC", "00000000-0000-0000-0000-000000000000"} {
		if err := m.cleanupBotRunDirectory(id); !errors.Is(err, ctr.ErrInvalidArgument) {
			t.Fatalf("ID %q: cleanup = %v", id, err)
		}
	}
	requireRuntimePath(t, dir, true)
	m.cfg.DataRoot = string(filepath.Separator)
	if err := m.cleanupBotRunDirectory(runtimeDirectoryTestBotID); !errors.Is(err, ctr.ErrInvalidArgument) {
		t.Fatalf("shared filesystem root accepted: %v", err)
	}
}

func TestRuntimeDirectoryCleanupMissingParentsIsIdempotent(t *testing.T) {
	for _, root := range []string{t.TempDir(), filepath.Join(t.TempDir(), "missing")} {
		m := newLegacyRouteTestManager(t, &legacyRouteTestService{}, config.WorkspaceConfig{DataRoot: root})
		for range 2 {
			if err := m.cleanupBotRunDirectory(runtimeDirectoryTestBotID); err != nil {
				t.Fatal(err)
			}
		}
		requireRuntimePath(t, filepath.Join(root, "run"), false)
	}
}

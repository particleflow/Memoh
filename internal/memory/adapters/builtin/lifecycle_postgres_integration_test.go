package builtin

// These tests use only synthetic fixtures and a local bridge rooted in t.TempDir.
// TEST_POSTGRES_DSN must point to a disposable database with the current schema.
import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	dbpkg "github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/memory/adapters"
	"github.com/felinics/memoh/internal/memory/storefs"
	"github.com/felinics/memoh/internal/memory/wikistore"
	"github.com/felinics/memoh/internal/team"
	"github.com/felinics/memoh/internal/workspace/bridge"
	pb "github.com/felinics/memoh/internal/workspace/bridgepb"
	"github.com/felinics/memoh/internal/workspace/bridgesvc"
)

type lifecyclePGBridge struct{ clients map[string]*bridge.Client }

func (b *lifecyclePGBridge) MCPClient(_ context.Context, bot string) (*bridge.Client, error) {
	c, ok := b.clients[bot]
	if !ok {
		return nil, fmt.Errorf("unknown synthetic bot %q", bot)
	}
	return c, nil
}

type lifecyclePGFixture struct {
	pool        *pgxpool.Pool
	store       *wikistore.PostgresStore
	fs          *storefs.Service
	rt          *graphRuntime
	bots, users []string
	roots       map[string]string
}

func lifecyclePGFS(t *testing.T, roots map[string]string, options ...grpc.ServerOption) *storefs.Service {
	t.Helper()
	b := &lifecyclePGBridge{clients: map[string]*bridge.Client{}}
	for bot, root := range roots {
		lis := bufconn.Listen(1 << 20)
		srv := grpc.NewServer(options...)
		pb.RegisterContainerServiceServer(srv, bridgesvc.New(bridgesvc.Options{WorkspaceRoot: root, DataMount: "/data", DefaultWorkDir: "/data"}))
		go func() { _ = srv.Serve(lis) }()
		conn, err := grpc.NewClient("passthrough:///lifecyclePG", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }))
		if err != nil {
			t.Fatal(err)
		}
		b.clients[bot] = bridge.NewClientFromConn(conn)
		t.Cleanup(func() { _ = conn.Close(); srv.Stop(); _ = lis.Close() })
	}
	return storefs.New(nil, b)
}

func newLifecyclePG(t *testing.T) *lifecyclePGFixture {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is not set")
	}
	p, err := dbpkg.OpenPostgresDSN(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	f := &lifecyclePGFixture{pool: p, store: wikistore.NewPostgres(dbsqlc.New(p)), roots: map[string]string{}}
	for range 2 {
		f.users = append(f.users, uuid.NewString())
		f.bots = append(f.bots, uuid.NewString())
	}
	t.Cleanup(func() {
		for _, b := range f.bots {
			_, _ = p.Exec(context.Background(), "DELETE FROM bots WHERE id=$1", b)
		}
		for _, u := range f.users {
			_, _ = p.Exec(context.Background(), "DELETE FROM team_members WHERE user_id=$1", u)
			_, _ = p.Exec(context.Background(), "DELETE FROM users WHERE id=$1", u)
		}
		p.Close()
	})
	for i, u := range f.users {
		_, err = p.Exec(t.Context(), "INSERT INTO users(id,username,email) VALUES($1,$2,$3)", u, "synthetic-"+u, u+"@example.test")
		if err != nil {
			t.Fatal(err)
		}
		_, err = p.Exec(t.Context(), "INSERT INTO team_members(team_id,user_id,role) VALUES($1,$2,'member')", team.DefaultTeamID, u)
		if err != nil {
			t.Fatal(err)
		}
		_, err = p.Exec(t.Context(), "INSERT INTO bots(id,team_id,owner_user_id,name,type) VALUES($1,$2,$3,$4,'personal')", f.bots[i], team.DefaultTeamID, u, "synthetic-"+f.bots[i])
		if err != nil {
			t.Fatal(err)
		}
		f.roots[f.bots[i]] = t.TempDir()
	}
	f.fs = lifecyclePGFS(t, f.roots)
	f.rt = NewGraphRuntime(nil, f.store, f.fs)
	t.Cleanup(func() { _ = f.rt.Close() })
	return f
}

func (f *lifecyclePGFixture) add(t *testing.T, bot, user, body string) adapters.MemoryItem {
	t.Helper()
	r, err := f.rt.Add(t.Context(), adapters.AddRequest{BotID: bot, Message: body, Metadata: adapters.BuildProfileMetadata(user, "", "王晨")})
	if err != nil {
		t.Fatal(err)
	}
	return r.Results[0]
}

func lifecyclePGFind(t *testing.T, rt *graphRuntime, bot, query string) []adapters.MemoryItem {
	t.Helper()
	r, e := rt.Search(t.Context(), adapters.SearchRequest{BotID: bot, Query: query, Limit: 50})
	if e != nil {
		t.Fatal(e)
	}
	return r.Results
}

func lifecyclePGHas(items []adapters.MemoryItem, id string) bool {
	for _, x := range items {
		if x.ID == id {
			return true
		}
	}
	return false
}

func lifecyclePGPaths(t *testing.T, root, id string) []string {
	t.Helper()
	out := []string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() && strings.HasSuffix(p, ".md") && filepath.Base(p) != "MEMORY.md" {
			b, e := os.ReadFile(p) // #nosec G304 -- walking only the synthetic t.TempDir fixture.
			if e != nil {
				return e
			}
			if strings.Contains(string(b), id) {
				q, _ := filepath.Rel(root, p)
				out = append(out, q)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

type lifecyclePGFailSync struct {
	memoryStore
	fail bool
}

func (s *lifecyclePGFailSync) RebuildFiles(ctx context.Context, b string, x []storefs.MemoryItem, m map[string]any) error {
	if s.fail {
		return errors.New("synthetic workspace outage")
	}
	return s.memoryStore.RebuildFiles(ctx, b, x, m)
}

func TestPostgresMemoryLifecycleDeleteHealthyRebuild(t *testing.T) {
	f := newLifecyclePG(t)
	bot := f.bots[0]
	x := f.add(t, bot, f.users[0], "请忘记合成暗号银色海豚")
	if _, e := f.rt.Delete(t.Context(), bot, x.ID); e != nil {
		t.Fatal(e)
	}
	_ = f.rt.Close()
	f.rt = NewGraphRuntime(nil, f.store, f.fs)
	for range 2 {
		if _, e := f.rt.Rebuild(t.Context(), bot); e != nil {
			t.Fatal(e)
		}
	}
	if lifecyclePGHas(lifecyclePGFind(t, f.rt, bot, "银色海豚"), x.ID) {
		t.Fatal("healthy delete resurrected")
	}
	if len(lifecyclePGPaths(t, f.roots[bot], x.ID)) != 0 {
		t.Fatal("deleted projection remains")
	}
	t.Log("PASS healthy delete, runtime recreation and two rebuilds leave no node or projection")
}

func TestPostgresMemoryLifecycleDeleteOutageMustNotResurrect(t *testing.T) {
	f := newLifecyclePG(t)
	bot := f.bots[0]
	x := f.add(t, bot, f.users[0], "合成暗号金色鲸鱼需要忘记")
	faulty := &lifecyclePGFailSync{memoryStore: f.fs, fail: true}
	f.rt = NewGraphRuntime(nil, f.store, faulty)
	if _, e := f.rt.Delete(t.Context(), bot, x.ID); e == nil {
		t.Fatal("delete hid the projection failure")
	}
	if _, e := f.store.GetNode(t.Context(), bot, x.ID); !errors.Is(e, wikistore.ErrNodeNotFound) {
		t.Fatalf("PG not deleted: %v", e)
	}
	if len(lifecyclePGPaths(t, f.roots[bot], x.ID)) != 1 {
		t.Fatal("fault did not preserve stale file")
	}
	_ = f.rt.Close()
	f.rt = NewGraphRuntime(nil, f.store, f.fs)
	if _, e := f.rt.Rebuild(t.Context(), bot); e != nil {
		t.Fatal(e)
	}
	if lifecyclePGHas(lifecyclePGFind(t, f.rt, bot, "金色鲸鱼"), x.ID) {
		t.Error("FAIL acknowledged delete + failed Markdown sync + restart/rebuild resurrects exact deleted ID from stale projection")
	}
}

func TestPostgresMemoryLifecycleAuthoredMarkdownForget(t *testing.T) {
	f := newLifecyclePG(t)
	bot := f.bots[0]
	root := f.roots[bot]
	if e := os.MkdirAll(filepath.Join(root, "memory"), 0o700); e != nil {
		t.Fatal(e)
	}
	raw := "---\ntype: note\nid: authored-code\nlayer: note\n---\n合成暗号蓝色松鼠需要忘记\n"
	if e := os.WriteFile(filepath.Join(root, "memory", "authored.md"), []byte(raw), 0o600); e != nil {
		t.Fatal(e)
	}
	if _, e := f.rt.IngestMarkdownFiles(t.Context(), bot); e != nil {
		t.Fatal(e)
	}
	id := bot + ":authored-code"
	if _, e := f.store.GetNode(t.Context(), bot, id); e != nil {
		t.Fatal(e)
	}
	if _, e := f.rt.Delete(t.Context(), bot, id); e != nil {
		t.Fatal(e)
	}
	_ = f.rt.Close()
	f.rt = NewGraphRuntime(nil, f.store, f.fs)
	if _, e := f.rt.Rebuild(t.Context(), bot); e != nil {
		t.Fatal(e)
	}
	if lifecyclePGHas(lifecyclePGFind(t, f.rt, bot, "蓝色松鼠"), id) {
		t.Fatal("rebuild re-imported the deleted authored memory")
	}
	contents, err := os.ReadFile(filepath.Join(root, "memory", "authored.md")) // #nosec G304 -- root is a synthetic t.TempDir fixture.
	if err != nil || string(contents) != raw {
		t.Fatalf("original modified: %q, %v", contents, err)
	}
	for range 2 {
		if _, err := f.rt.IngestMarkdownFiles(t.Context(), bot); err != nil {
			t.Fatal(err)
		}
		if n, err := f.store.CountNodes(t.Context(), bot); err != nil || n != 1 {
			t.Fatalf("explicit import count=%d, %v", n, err)
		}
	}
	if _, err := f.rt.DeleteAll(t.Context(), adapters.DeleteAllRequest{BotID: bot}); err != nil {
		t.Fatal(err)
	}
	contents, err = os.ReadFile(filepath.Join(root, "memory", "authored.md")) // #nosec G304 -- root is a synthetic t.TempDir fixture.
	if err != nil || string(contents) != raw {
		t.Fatalf("delete all modified original: %q, %v", contents, err)
	}
	if _, err := f.rt.Rebuild(t.Context(), bot); err != nil {
		t.Fatal(err)
	}
	if n, err := f.store.CountNodes(t.Context(), bot); err != nil || n != 0 {
		t.Fatalf("delete all/rebuild count=%d, %v", n, err)
	}
}

// A listed file can become unreadable before cleanup. Preserve the unknown
// document, but do not report a completed delete while its contents are unknown.
func TestPostgresMemoryLifecycleUnreadableProjection(t *testing.T) {
	f := newLifecyclePG(t)
	bot := f.bots[0]
	item := f.add(t, bot, f.users[0], "合成不可读投影")
	files := lifecyclePGFS(t, f.roots, grpc.StreamInterceptor(func(_ any, _ grpc.ServerStream, _ *grpc.StreamServerInfo, _ grpc.StreamHandler) error {
		return errors.New("synthetic read failure")
	}))
	faulty := NewGraphRuntime(nil, f.store, files)
	defer func() { _ = faulty.Close() }()
	if response, err := faulty.Delete(t.Context(), bot, item.ID); err == nil || response.Message != "" {
		t.Fatalf("unreadable projection cleanup reported success: %+v, %v", response, err)
	}
	if paths := lifecyclePGPaths(t, f.roots[bot], item.ID); len(paths) == 0 {
		t.Fatal("unreadable file must remain untouched")
	}
	if _, err := f.rt.Rebuild(t.Context(), bot); err != nil {
		t.Fatal(err)
	}
	if hits := lifecyclePGFind(t, f.rt, bot, "不可读投影"); len(hits) != 0 {
		t.Fatalf("resurrected: %+v", hits)
	}
}

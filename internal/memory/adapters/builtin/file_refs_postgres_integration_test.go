package builtin

// Real PostgreSQL, the server's migrated schema, the real Markdown store over
// a local bridge, and canonical originals. All fixtures are synthetic.
import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	dbpkg "github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/dbtest"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/mcp"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/memory/adapters"
	"github.com/felinics/memoh/internal/memory/storefs"
	"github.com/felinics/memoh/internal/memory/wikistore"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
	"github.com/felinics/memoh/internal/team"
	"github.com/felinics/memoh/internal/workspace/bridge"
	pb "github.com/felinics/memoh/internal/workspace/bridgepb"
	"github.com/felinics/memoh/internal/workspace/bridgesvc"
)

type fileRefPGBridge struct{ clients map[string]*bridge.Client }

func (b *fileRefPGBridge) MCPClient(_ context.Context, bot string) (*bridge.Client, error) {
	c, ok := b.clients[bot]
	if !ok {
		return nil, fmt.Errorf("unknown synthetic bot %q", bot)
	}
	return c, nil
}

type fileRefPGFixture struct {
	pool        *pgxpool.Pool
	store       *wikistore.PostgresStore
	fs          *storefs.Service
	rt          *graphRuntime
	bots, users []string
	roots       map[string]string
}

func fileRefPGFS(t *testing.T, roots map[string]string, options ...grpc.ServerOption) *storefs.Service {
	t.Helper()
	b := &fileRefPGBridge{clients: map[string]*bridge.Client{}}
	for bot, root := range roots {
		lis := bufconn.Listen(1 << 20)
		srv := grpc.NewServer(options...)
		pb.RegisterContainerServiceServer(srv, bridgesvc.New(bridgesvc.Options{WorkspaceRoot: root, DataMount: "/data", DefaultWorkDir: "/data"}))
		go func() { _ = srv.Serve(lis) }()
		conn, err := grpc.NewClient("passthrough:///fileRefPG", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }))
		if err != nil {
			t.Fatal(err)
		}
		b.clients[bot] = bridge.NewClientFromConn(conn)
		t.Cleanup(func() { _ = conn.Close(); srv.Stop(); _ = lis.Close() })
	}
	return storefs.New(nil, b)
}

func newFileRefPG(t *testing.T) *fileRefPGFixture {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		if os.Getenv("MEMOH_TEST_MEMORY_FILES_REQUIRED") == "1" {
			t.Fatal("TEST_POSTGRES_DSN is required")
		}
		t.Skip("TEST_POSTGRES_DSN is not set")
	}
	if os.Getenv("TEST_POSTGRES_BOOTSTRAP_SCHEMA") == "1" {
		if err := dbtest.MigratePostgresUp(dsn); err != nil {
			t.Fatal(err)
		}
	}
	p, err := dbpkg.OpenPostgresDSN(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	f := &fileRefPGFixture{pool: p, store: wikistore.NewPostgres(dbsqlc.New(p)), roots: map[string]string{}}
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
		_, err = p.Exec(t.Context(), "INSERT INTO bots(id,team_id,owner_user_id,name) VALUES($1,$2,$3,$4)", f.bots[i], team.DefaultTeamID, u, "synthetic-"+f.bots[i])
		if err != nil {
			t.Fatal(err)
		}
		f.roots[f.bots[i]] = t.TempDir()
	}
	f.fs = fileRefPGFS(t, f.roots)
	f.rt = NewGraphRuntime(nil, f.store, f.fs)
	t.Cleanup(func() { _ = f.rt.Close() })
	return f
}

func TestPostgresMemoryFileReferenceLifecycle(t *testing.T) {
	f := newFileRefPG(t)
	bot := f.bots[0]
	originalsRoot := t.TempDir()
	files := media.NewService(nil, localfs.NewOriginals(originalsRoot))
	files.SetRequirePersistence(true)
	const bytes = "synthetic original bytes; never copied into memory"
	asset, err := files.Ingest(t.Context(), media.IngestInput{BotID: bot, Mime: "text/plain", Reader: strings.NewReader(bytes)})
	if err != nil {
		t.Fatal(err)
	}
	noInfer := false
	added, err := f.rt.Add(t.Context(), adapters.AddRequest{BotID: bot, Message: "合成项目附件引用", Infer: &noInfer, Metadata: map[string]any{"content_hash": asset.ContentHash, "owner": f.bots[1], "url": "https://SECRET.invalid"}})
	if err != nil {
		t.Fatal(err)
	}
	id := added.Results[0].ID
	if _, err = f.rt.Rebuild(t.Context(), bot); err != nil {
		t.Fatal(err)
	}
	// Recreate runtime, connection pool and originals service: no process cache
	// or new ingest is needed to resolve this existing reference.
	_ = f.rt.Close()
	pool, err := dbpkg.OpenPostgresDSN(t.Context(), os.Getenv("TEST_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	f.rt = NewGraphRuntime(nil, wikistore.NewPostgres(dbsqlc.New(pool)), f.fs)
	if _, err = f.rt.Rebuild(t.Context(), bot); err != nil {
		t.Fatal(err)
	}
	files = media.NewService(nil, localfs.NewOriginals(originalsRoot))
	files.SetRequirePersistence(true)
	p := NewBuiltinProvider(nil, f.rt)
	p.SetFileResolver(files)
	for _, mode := range []string{"search", "list"} {
		var response adapters.SearchResponse
		if mode == "search" {
			response, err = p.Search(t.Context(), adapters.SearchRequest{BotID: bot, Query: "合成项目附件引用", NoStats: true})
		} else {
			response, err = p.GetAll(t.Context(), adapters.GetAllRequest{Filters: map[string]any{"bot_id": bot}, NoStats: true})
		}
		if err != nil || len(response.Results) != 1 {
			t.Fatalf("%s: %+v %v", mode, response, err)
		}
		item := response.Results[0]
		if item.ID != id || len(item.FileRefs) != 1 || item.FileRefs[0].ContentHash != asset.ContentHash || item.FileRefs[0].Status != "located" {
			t.Fatal(item)
		}
	}
	result, err := p.CallTool(t.Context(), mcp.ToolSessionContext{BotID: bot}, adapters.ToolSearchMemory, map[string]any{"query": "合成项目附件引用"})
	if err != nil {
		t.Fatal(err)
	}
	payload := result["structuredContent"].(map[string]any)
	refs := payload["results"].([]map[string]any)[0]["file_refs"].([]adapters.MemoryFileRef)
	if len(refs) != 1 || refs[0].ContentHash != asset.ContentHash {
		t.Fatal(refs)
	}
	reader, _, err := files.Open(t.Context(), bot, refs[0].ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(got) != bytes {
		t.Fatalf("original changed: %v", err)
	}
	derived, err := f.fs.ReadAllMemoryFiles(t.Context(), bot)
	if err != nil || len(derived) != 1 || derived[0].Metadata["content_hash"] != asset.ContentHash || strings.Contains(derived[0].Memory, bytes) {
		t.Fatalf("derived reference: %+v %v", derived, err)
	}
	// A different Bot cannot make the reference accessible by claiming ownership.
	if _, err = p.Add(t.Context(), adapters.AddRequest{BotID: f.bots[1], Message: "合成项目附件引用", Infer: &noInfer, Metadata: map[string]any{"content_hash": asset.ContentHash, "bot_id": bot, "owner": bot}}); err != nil {
		t.Fatal(err)
	}
	other, err := p.Search(t.Context(), adapters.SearchRequest{BotID: f.bots[1], Query: "合成项目附件引用", NoStats: true})
	if err != nil || len(other.Results) != 1 || other.Results[0].FileRefs[0].Status != "unavailable" || other.Results[0].FileRefs[0].ContentHash != "" {
		t.Fatalf("cross-Bot leak: %+v %v", other, err)
	}
	t.Log("real PG/Markdown rebuild and fresh runtime retain a canonical reference; real bytes resolve only under their Bot")
}

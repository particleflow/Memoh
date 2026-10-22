//go:build integration

package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/agent/toolexec"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/dbtest"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/imagejob"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/models"
	"github.com/felinics/memoh/internal/settings"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
	"github.com/felinics/memoh/internal/team"
)

func TestImageToolPostgresParentChildEditReferences(t *testing.T) {
	ctx := context.Background()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is not set")
	}
	if os.Getenv("TEST_POSTGRES_BOOTSTRAP_SCHEMA") == "1" {
		if err := dbtest.MigratePostgresUp(dsn); err != nil {
			t.Fatal(err)
		}
	}
	pool, err := db.OpenPostgresDSN(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	user, bot, parent, child, other, run, turn, providerID, modelID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	root := t.TempDir()
	m := media.NewService(nil, localfs.NewOriginals(root))
	m.SetRequirePersistence(true)
	if err := m.EnableUploads(root); err != nil {
		t.Fatal(err)
	}
	defer m.Uploads().Close()
	journal, err := media.OpenImageTaskJournal(root)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	input := imageJobPNG(t, color.RGBA{R: 255, A: 255})
	output := imageJobPNG(t, color.RGBA{G: 255, A: 255})
	original, err := m.Ingest(ctx, media.IngestInput{BotID: bot, Mime: "image/png", Reader: bytes.NewReader(input)})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/images/edits" || r.Method != "POST" {
			t.Errorf("unexpected image endpoint")
			w.WriteHeader(400)
			return
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		defer r.MultipartForm.RemoveAll()
		f, _, err := r.FormFile("image[]")
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		data, _ := io.ReadAll(f)
		_ = f.Close()
		if !bytes.Equal(data, input) || r.FormValue("model") != "synthetic-edit-model" {
			t.Error("incorrect original/model")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(output)}}})
	}))
	defer fixture.Close()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM bots WHERE id=$1", bot)
		_, _ = pool.Exec(ctx, "DELETE FROM providers WHERE id=$1", providerID)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id=$1", user)
	})
	exec("INSERT INTO users(id,username,is_active) VALUES($1,$2,true)", user, "image-fixture-"+user)
	exec("INSERT INTO team_members(team_id,user_id,role) VALUES($1,$2,'member')", team.DefaultTeamID, user)
	exec("INSERT INTO bots(id,owner_user_id,name,status) VALUES($1,$2,'image-fixture','ready')", bot, user)
	conf, _ := json.Marshal(map[string]any{"base_url": fixture.URL, "api_key": "synthetic-not-a-credential"})
	exec("INSERT INTO providers(id,name,client_type,config) VALUES($1,$2,'openai-completions',$3)", providerID, "image-fixture-"+providerID, conf)
	exec(`INSERT INTO models(id,provider_id,model_id,config) VALUES($1,$2,'synthetic-edit-model','{"compatibilities":["image-output","image-edit"]}')`, modelID, providerID)
	exec("UPDATE bots SET image_model_id=$1 WHERE id=$2", modelID, bot)
	for _, id := range []string{parent, other} {
		exec("INSERT INTO bot_sessions(id,bot_id,type,created_by_user_id) VALUES($1,$2,'chat',$3)", id, bot, user)
	}
	exec("INSERT INTO bot_sessions(id,bot_id,type,session_mode,parent_session_id,created_by_user_id) VALUES($1,$2,'subagent','subagent',$3,$4)", child, bot, parent, user)
	exec(`INSERT INTO session_runs(run_id,bot_id,session_id,invocation_id,turn_id,turn_position,state,input_json,input_fingerprint) VALUES($1,$2,$3,$5, $4,1,'running','{}','synthetic')`, run, bot, parent, turn, run)
	message := uuid.NewString()
	exec(`INSERT INTO bot_history_messages(id,bot_id,session_id,role,content) VALUES($1,$2,$3,'user','[]')`, message, bot, parent)
	exec(`INSERT INTO bot_history_message_assets(message_id,role,ordinal,content_hash,mime,size_bytes,storage_key) VALUES($1,'attachment',0,$2,'image/png',$3,$4)`, message, original.ContentHash, len(input), original.StorageKey)
	queries := postgresstore.NewQueriesWithPool(pool, sqlc.New(pool))
	store := postgresstore.NewWithPool(pool, sqlc.New(pool))
	authority := &imagejob.Authority{Queries: queries, Accounts: accounts.NewService(nil, store), Bots: bots.NewService(nil, queries), Sessions: thread.NewService(nil, queries, nil)}
	service := imagejob.New(m, journal, authority.Authorize)
	tool := NewImageGenProvider(nil, settings.NewService(slog.New(slog.DiscardHandler), queries, nil, nil), models.NewService(slog.New(slog.DiscardHandler), queries), queries, nil, "")
	tool.SetImageJobs(service, authority)
	ctx = imagejob.WithOrigin(ctx, imagejob.Origin{BotID: bot, UserID: user, SessionID: parent, RunID: run})
	var events []ToolStreamEvent
	parentSession := liveShortcutSession(func(e ToolStreamEvent) { events = append(events, e) })
	parentSession.BotID, parentSession.UserID, parentSession.SessionID = bot, user, parent
	invoke := func(callCtx context.Context, session string) (imagejob.Result, error) {
		available, err := tool.Tools(callCtx, SessionContext{BotID: bot, UserID: user, SessionID: session, SessionType: "subagent", IsSubagent: true})
		if err != nil || len(available) != 1 {
			return imagejob.Result{}, fmt.Errorf("registered tools unavailable: %w", err)
		}
		out, err := available[0].Execute(&toolexec.ToolExecContext{Context: callCtx, ToolCallID: uuid.NewString()}, toolexec.ArgumentsFromValue(map[string]any{"mode": "edit", "prompt": "change red to green", "input_refs": []string{original.ContentHash}}))
		if err != nil {
			return imagejob.Result{}, err
		}
		raw, _ := json.Marshal(toolexec.OutputValue(out))
		var result imagejob.Result
		err = json.Unmarshal(raw, &result)
		return result, err
	}
	var receipt imagejob.Result
	spawn := &SpawnProvider{logger: slog.New(slog.DiscardHandler), modelResolver: func(context.Context, SessionContext, string, string, string) (resolvedSubagentModel, error) {
		return resolvedSubagentModel{}, nil
	}}
	spawn.SetImageResults(tool.CollectImageResults)
	spawn.SetAgent(&mockSpawnAgent{generateFunc: func(c context.Context, _ SpawnRunConfig, _ func()) (*SpawnResult, error) {
		var err error
		receipt, err = invoke(c, child)
		if err != nil {
			return nil, err
		}
		if receipt.State != "ready" {
			return nil, fmt.Errorf("image state=%s code=%s", receipt.State, receipt.Code)
		}
		return &SpawnResult{Text: "synthetic edit complete", Persisted: true}, nil
	}})
	result := spawn.runSubagentTask(ctx, &agentRequest{taskID: "fixture-child-task", agentID: "image-editor", agentSessionID: child, message: "edit attached original", parentSession: parentSession})
	if result.Error != "" || len(result.Artifacts) != 1 || len(events) != 1 || calls.Load() != 1 {
		t.Fatalf("child result=%+v events=%d calls=%d", result, len(events), calls.Load())
	}
	sum := sha256.Sum256(output)
	want := hex.EncodeToString(sum[:])
	if result.Artifacts[0].ContentHash != want || events[0].Attachments[0].ContentHash != want || events[0].Attachments[0].URL != "" || events[0].Attachments[0].Path != "" {
		t.Fatal("parent did not receive only stable ready reference")
	}
	f, _, err := m.Open(ctx, bot, want)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(f)
	_ = f.Close()
	if !bytes.Equal(got, output) {
		t.Fatal("output readback mismatch")
	}
	// A new child under the same host business turn must return the old receipt.
	secondChild := uuid.NewString()
	exec("INSERT INTO bot_sessions(id,bot_id,type,session_mode,parent_session_id,created_by_user_id) VALUES($1,$2,'subagent','subagent',$3,$4)", secondChild, bot, parent, user)
	again, err := invoke(ctx, secondChild)
	if err != nil || again.TaskID != receipt.TaskID || calls.Load() != 1 {
		t.Fatalf("child replay=%+v err=%v calls=%d", again, err, calls.Load())
	}
	// Same Bot and known hash do not authorize another conversation.
	if _, err := invoke(ctx, other); err == nil || calls.Load() != 1 {
		t.Fatal("unrelated session dispatched")
	}
	exec(`UPDATE models SET config='{"compatibilities":["image-output"]}' WHERE id=$1`, modelID)
	if _, err := invoke(ctx, child); err == nil || calls.Load() != 1 {
		t.Fatal("unsupported edit fell back")
	}
	exec("UPDATE users SET is_active=false WHERE id=$1", user)
	if _, err := tool.CollectImageResults(ctx, parentSession, []string{receipt.TaskID}); err == nil || len(events) != 1 {
		t.Fatal("revoked parent received artifact")
	}
	raw, _ := json.Marshal(agentResultMap(result))
	if strings.Contains(string(raw), "b64_json") || strings.Contains(string(raw), fixture.URL) {
		t.Fatal("provider data leaked to parent")
	}
}

func imageJobPNG(t *testing.T, c color.RGBA) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			im.SetRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

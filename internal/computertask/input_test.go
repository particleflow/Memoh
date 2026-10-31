package computertask

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	"github.com/felinics/memoh/internal/agenvo"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/mcp"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
	"github.com/felinics/memoh/internal/toolcontext"
	"github.com/felinics/memoh/internal/workspace/bridge"
	pb "github.com/felinics/memoh/internal/workspace/bridgepb"
	"github.com/felinics/memoh/internal/workspace/bridgesvc"
)

type taskInputPeer struct {
	*bridgesvc.Server
	drop   atomic.Bool
	writes atomic.Int32
}

type taskInputStream struct {
	pb.ContainerService_WriteRawServer
	peer *taskInputPeer
}

func (s taskInputStream) SendAndClose(response *pb.WriteRawResponse) error {
	if s.peer.drop.Swap(false) {
		return status.Error(codes.Unavailable, "synthetic reply lost after atomic write")
	}
	return s.ContainerService_WriteRawServer.SendAndClose(response)
}

func (s *taskInputPeer) WriteRaw(stream pb.ContainerService_WriteRawServer) error {
	s.writes.Add(1)
	return s.Server.WriteRaw(taskInputStream{ContainerService_WriteRawServer: stream, peer: s})
}

// This translates only the declared Windows root into the Linux peer's physical
// fixture directory. Production receives real Windows paths, with no /data map.
type taskInputPathView struct {
	client    *bridge.Client
	root, win string
}

func (v taskInputPathView) path(path string) string {
	base := strings.TrimRight(v.win, `\/`)
	if path != v.win && !strings.HasPrefix(path, base+`\`) {
		panic("test attempted another Windows root")
	}
	return filepath.Join(v.root, filepath.FromSlash(strings.ReplaceAll(strings.TrimPrefix(path, base), `\`, "/")))
}

func (v taskInputPathView) Stat(ctx context.Context, path string) (*pb.FileEntry, error) {
	return v.client.Stat(ctx, v.path(path))
}

func (v taskInputPathView) Mkdir(ctx context.Context, path string) error {
	return v.client.Mkdir(ctx, v.path(path))
}

func (v taskInputPathView) ReadRaw(ctx context.Context, path string) (io.ReadCloser, error) {
	return v.client.ReadRaw(ctx, v.path(path))
}

func (v taskInputPathView) WriteRaw(ctx context.Context, path string, reader io.Reader) (int64, error) {
	return v.client.WriteRaw(ctx, v.path(path), reader)
}

func (v taskInputPathView) ListDir(ctx context.Context, path string, recursive bool, offset, limit, collapse int32) (*bridge.ListDirResult, error) {
	if path == `C:\` && v.win == `C:\Synthetic` {
		// Read the real fixture root's own directory entry (including symlink
		// mode), while presenting its declared Windows name to the adapter.
		page, err := v.client.ListDir(ctx, filepath.Dir(v.root), false, offset, limit, collapse)
		if page != nil {
			for _, entry := range page.Entries {
				if entry.Path == filepath.Base(v.root) {
					entry.Path = "Synthetic"
				}
			}
		}
		return page, err
	}
	return v.client.ListDir(ctx, v.path(path), recursive, offset, limit, collapse)
}

type taskInputAssets struct {
	*media.Service
	revoked      bool
	corrupt      bool
	revokeOnRead bool
}

type taskInputRevokeReader struct {
	media.ReadSeekCloser
	assets *taskInputAssets
}

func (r taskInputRevokeReader) Read(p []byte) (int, error) {
	n, err := r.ReadSeekCloser.Read(p)
	if n > 0 {
		r.assets.revoked = true
	}
	return n, err
}

type taskInputReadSeeker struct{ *bytes.Reader }

func (taskInputReadSeeker) Close() error { return nil }

func (a *taskInputAssets) OpenSeekable(ctx context.Context, bot, hash string) (media.ReadSeekCloser, media.Asset, error) {
	if a.revoked {
		return nil, media.Asset{}, ErrDenied
	}
	r, asset, err := a.Service.OpenSeekable(ctx, bot, hash)
	if err == nil && a.corrupt {
		size, seekErr := r.Seek(0, io.SeekEnd)
		_ = r.Close()
		if seekErr != nil {
			return nil, media.Asset{}, seekErr
		}
		return taskInputReadSeeker{bytes.NewReader(bytes.Repeat([]byte("x"), int(size)))}, asset, nil
	}
	if err == nil && a.revokeOnRead {
		return taskInputRevokeReader{ReadSeekCloser: r, assets: a}, asset, nil
	}
	return r, asset, err
}

func (a *taskInputAssets) Open(ctx context.Context, bot, hash string) (io.ReadCloser, media.Asset, error) {
	if a.revoked {
		return nil, media.Asset{}, ErrDenied
	}
	r, asset, err := a.Service.Open(ctx, bot, hash)
	if err == nil && a.corrupt {
		_ = r.Close()
		return io.NopCloser(strings.NewReader(strings.Repeat("x", int(asset.SizeBytes)))), asset, nil
	}
	return r, asset, err
}

type taskInputFixture struct {
	base    *nativeTaskFixture
	service *Service
	ctx     context.Context
	root    string
	assets  *taskInputAssets
	asset   media.Asset
	data    []byte
	peer    *taskInputPeer
	files   taskInputPathView
	nas     string
	address string
	ca      []byte
}

func newTaskInputFixture(t *testing.T) taskInputFixture {
	t.Helper()
	f, service, ctx := newNativeTaskFixture(t)
	root := t.TempDir()
	nas, pc := filepath.Join(root, "nas-originals"), filepath.Join(root, "pc-task-root")
	if err := os.MkdirAll(pc, 0o700); err != nil {
		t.Fatal(err)
	}
	assets := &taskInputAssets{Service: media.NewService(nil, localfs.New(nas))}
	data := make([]byte, 3*65536+117)
	for i := range data {
		data[i] = byte((i*37 + i/197) % 256)
	}
	asset, err := assets.Ingest(ctx, media.IngestInput{BotID: f.binding.BotID, Mime: "application/octet-stream", Reader: bytes.NewReader(data)})
	if err != nil {
		t.Fatal(err)
	}
	certServer := httptest.NewTLSServer(nil)
	cert := certServer.TLS.Certificates[0]
	certificate := certServer.Certificate()
	certServer.Close()
	pool := x509.NewCertPool()
	pool.AddCert(certificate)
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	// Match cmd/bridge's existing native-host absolute path policy. The test
	// adapter above still rejects every caller path outside its fixed Win root,
	// except the read-only ancestor listing needed to detect a root link.
	peer := &taskInputPeer{Server: bridgesvc.New(bridgesvc.Options{DefaultWorkDir: pc, WorkspaceRoot: pc, DataMount: pc, AllowHostAbsolute: true})}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})))
	pb.RegisterContainerServiceServer(server, peer)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{RootCAs: pool, ServerName: "example.com", MinVersion: tls.VersionTLS12})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	files := taskInputPathView{client: bridge.NewClientFromConn(conn).FilesOnly(), root: pc, win: f.profile.WorkDir}
	service.assets = assets
	service.WithInputFiles(func(_ context.Context, b toolcontext.ComputerBinding, targetID string, target agenvo.Target) (InputFileSystem, error) {
		if b != f.binding || targetID != f.binding.TargetID || target.WorkDir != f.profile.WorkDir || target.Producer != f.profile.Producer {
			t.Fatal("input resolver changed original target/root/config pair")
		}
		return files, nil
	})
	return taskInputFixture{base: f, service: service, ctx: ctx, root: pc, assets: assets, asset: asset, data: data, peer: peer, files: files, nas: nas, address: listener.Addr().String(), ca: certificate.Raw}
}

func (f taskInputFixture) request() Submit {
	return Submit{Kind: "herdr_pi_task", Task: "Read the supplied binary input and report its exact SHA256.", Inputs: []string{f.asset.ContentHash}}
}

func TestTaskInputSubmitStreamsAuthorizedOriginal(t *testing.T) {
	f := newTaskInputFixture(t)
	session := mcp.ToolSessionContext{BotID: f.base.binding.BotID, UserID: f.base.binding.UserID, SessionID: f.base.binding.SessionID, RunID: f.base.binding.RunID, RunContext: f.ctx}
	response, err := NewSource(f.service).CallTool(f.ctx, session, "computer_task_submit", map[string]any{"kind": f.request().Kind, "task": f.request().Task, "inputs": []any{f.asset.ContentHash}})
	wire, _ := json.Marshal(response["structuredContent"])
	result := parseResult(t, wire)
	if err != nil || result.State != "submitted" || result.Inputs == nil || result.Inputs.State != "verified" || len(result.Inputs.Files) != 1 || f.peer.writes.Load() != 1 || f.base.batch.runs != 1 {
		t.Fatal("formal input/dispatch did not finish", result.State, result.Phase, err)
	}
	r, d, err := f.service.load(f.ctx, f.base.binding, result.ID)
	if err != nil {
		t.Fatal(err)
	}
	p := d.Batch.Progress.Inputs
	path := filepath.Join(f.root, filepath.FromSlash(p.Path(p.Files[0])))
	actual, err := os.ReadFile(path) // #nosec G304 -- Test-owned fixture file; subprocess paths are passed only by this test parent.
	if err != nil || !bytes.Equal(actual, f.data) || p.Task.OperationID != result.ID || p.Task.SessionID != f.base.binding.SessionID || p.Task.Attempt != d.Batch.Progress.Attempt || p.Task.Generation != d.Batch.Progress.Generation || p.Task.RootHash != deliverymeta.RootHash(f.base.profile.WorkDir) || r.Fingerprint != fingerprint(Submit{TargetID: f.base.binding.TargetID, Kind: f.request().Kind, Task: f.request().Task, WorkDir: ".", Inputs: f.request().Inputs}) {
		t.Fatal("actual PC bytes or original task identity differ", err)
	}
	prior := f.peer.writes.Load()
	duplicate, err := f.service.submit(f.ctx, f.base.binding, f.request())
	if err != nil || duplicate.ID != result.ID || f.peer.writes.Load() != prior || f.base.batch.runs != 1 {
		t.Fatal("duplicate submit repeated file or Pi work", err)
	}
	nas, _, err := f.assets.Open(f.ctx, f.base.binding.BotID, f.asset.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	kept, err := io.ReadAll(nas)
	_ = nas.Close()
	if err != nil || !bytes.Equal(kept, f.data) {
		t.Fatal("NAS original changed")
	}
	t.Logf("actual_NAS_to_PC_bytes=%d sha256=%s TLS=true file_RPC_writes=1 original_task=true original_RootHash=true duplicate_submit_Pi=0", len(actual), f.asset.ContentHash)
}

func TestTaskInputUnknownWriteResumesOriginalBeforePi(t *testing.T) {
	f := newTaskInputFixture(t)
	f.peer.drop.Store(true)
	first, err := f.service.submit(f.ctx, f.base.binding, f.request())
	if !errors.Is(err, ErrDeliveryAsset) || first.ID == "" || first.Phase != "pi_inputs_unavailable" || f.base.batch.runs != 0 {
		t.Fatal("unknown file response dispatched Pi", first.Phase, err)
	}
	_, before, err := f.service.load(f.ctx, f.base.binding, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	admitted := &resumeOperationStore{nativeTaskStore: f.base.store}
	f.service.store = admitted // New host service, same serialized operation and native target.
	in := f.request()
	in.ResumeOperationID = first.ID
	recovered, err := f.service.submit(f.ctx, f.base.binding, in)
	if err != nil || recovered.ID != first.ID || recovered.State != "submitted" || f.peer.writes.Load() != 1 || f.base.batch.runs != 1 {
		t.Fatal("original input recovery failed/replayed bytes", recovered.Phase, err)
	}
	_, after, err := f.service.load(f.ctx, f.base.binding, first.ID)
	if err != nil || after.Batch.Progress.Inputs.Task != before.Batch.Progress.Inputs.Task || after.Batch.Progress.RequestHash != before.Batch.Progress.RequestHash {
		t.Fatal("input recovery changed original identity", err)
	}
	for range 2 {
		if _, err := f.service.submit(f.ctx, f.base.binding, in); err != nil || f.base.batch.runs != 1 || f.peer.writes.Load() != 1 {
			t.Fatal("known submission resumed again", err)
		}
	}
}

func TestTaskInputRejectsUnauthorizedCorruptAndConflictingBytes(t *testing.T) {
	for _, kind := range []string{"NAS-revoked", "revoked-during-stream", "corrupt-source", "root-symlink", "ancestor-symlink", "conflicting-input", "target-changed", "bad-hash", "duplicate-hash"} {
		t.Run(kind, func(t *testing.T) {
			f := newTaskInputFixture(t)
			in := f.request()
			switch kind {
			case "NAS-revoked":
				f.assets.revoked = true
			case "corrupt-source":
				f.assets.corrupt = true
			case "revoked-during-stream":
				f.assets.revokeOnRead = true
			case "ancestor-symlink":
				if err := os.Symlink(t.TempDir(), filepath.Join(f.root, ".memoh-computer")); err != nil {
					t.Fatal(err)
				}
			case "root-symlink":
				if err := os.Rename(f.root, f.root+"-original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(f.root+"-original", f.root); err != nil {
					t.Fatal(err)
				}
			case "bad-hash":
				in.Inputs = []string{"../original"}
			case "duplicate-hash":
				in.Inputs = append(in.Inputs, in.Inputs[0])
			case "target-changed":
				old := f.service.inputFiles
				f.service.inputFiles = func(ctx context.Context, b toolcontext.ComputerBinding, id string, target agenvo.Target) (InputFileSystem, error) {
					files, err := old(ctx, b, id, target)
					f.base.denied = true
					return files, err
				}
			case "conflicting-input":
				old := f.service.inputFiles
				f.service.inputFiles = func(ctx context.Context, b toolcontext.ComputerBinding, id string, target agenvo.Target) (InputFileSystem, error) {
					files, err := old(ctx, b, id, target)
					for _, r := range f.base.store.records {
						var d Details
						_ = json.Unmarshal(r.Details, &d)
						if d.Batch != nil && d.Batch.Progress.Inputs != nil {
							p := d.Batch.Progress.Inputs
							path := filepath.Join(f.root, filepath.FromSlash(p.Path(p.Files[0])))
							if os.MkdirAll(filepath.Dir(path), 0o700) != nil || os.WriteFile(path, []byte("preserved-conflict"), 0o600) != nil {
								t.Fatal("conflict fixture")
							}
						}
					}
					return files, err
				}
			}
			result, err := f.service.submit(f.ctx, f.base.binding, in)
			if err == nil || f.base.batch.runs != 0 || f.peer.writes.Load() > 1 {
				t.Fatal("invalid input reached Pi", err)
			}
			if kind != "corrupt-source" && kind != "revoked-during-stream" && f.peer.writes.Load() != 0 {
				t.Fatal("invalid input reached file write")
			}
			if result.Inputs != nil && len(result.Inputs.Files) != 0 {
				path := filepath.Join(f.root, filepath.FromSlash(result.Inputs.Files[0].Path))
				kept, readErr := os.ReadFile(path) // #nosec G304 -- Test-owned fixture file; subprocess paths are passed only by this test parent.
				if kind == "conflicting-input" && (readErr != nil || string(kept) != "preserved-conflict") {
					t.Fatal("conflicting input overwritten", readErr)
				}
				if (kind == "corrupt-source" || kind == "revoked-during-stream") && !os.IsNotExist(readErr) {
					t.Fatal("corrupt/revoked bytes were committed", readErr)
				}
			}
		})
	}
}

func unpackTaskInputScript(t *testing.T, command string) []byte {
	t.Helper()
	match := regexp.MustCompile(`FromBase64String\('([A-Za-z0-9+/=]+)'\)`).FindStringSubmatch(command)
	if len(match) != 2 {
		t.Fatal("missing production packed launcher")
	}
	raw, err := base64.StdEncoding.DecodeString(match[1])
	if err != nil {
		t.Fatal(err)
	}
	r, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	script, err := io.ReadAll(r)
	sum := sha256.Sum256(script)
	if err != nil || !strings.Contains(command, hex.EncodeToString(sum[:])) {
		t.Fatal("production launcher hash mismatch", err)
	}
	return script
}

func TestTaskInputProductionEnvelopeReadsRealInput(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux generated-envelope/physical-file peer; native Windows acceptance is separate")
	}
	f := newTaskInputFixture(t)
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	// The peer translates Win32 filesystem calls only. No data or task identity
	// is invented; this is Linux generator/consumer evidence, not Windows/Pi QA.
	mapper := filepath.Join(bin, "win32-fixture.cjs")
	mapping := `const fs=require('fs'),path=require('path');
const root=process.env.SYNTH_PC_ROOT,win=process.env.SYNTH_WIN_ROOT;
function map(p){return typeof p==='string' && (p===win||p.startsWith(win+'\\'))?root+p.slice(win.length).replace(/\\/g,'/'):p;}
for(const name of ['readFileSync','lstatSync','openSync']){const original=fs[name];fs[name]=function(p,...args){return original.call(fs,map(p),...args);};}
const real=fs.realpathSync.native;fs.realpathSync.native=function(p){const value=real(map(p));return typeof p==='string'&&p.startsWith(win)&&value.startsWith(root)?win+value.slice(root.length).replace(/\//g,'\\'):value;};
const normalize=path.normalize,join=path.join;
path.normalize=p=>typeof p==='string'&&p.startsWith(win)?path.win32.normalize(p):normalize(p);
path.join=(...p)=>typeof p[0]==='string'&&p[0].startsWith(win)?path.win32.join(...p):join(...p);
`
	pi := `const fs=require('fs'),crypto=require('crypto'),path=require('path');
const p=JSON.parse(fs.readFileSync(process.env.MEMOH_TASK_INPUT_CONTEXT_FILE,'utf8'));
const f=p.files[0],data=fs.readFileSync(f.path);
const hash=crypto.createHash('sha256').update(data).digest('hex');
if(hash!==f.content_hash||data.length!==f.size_bytes)process.exit(17);
fs.writeFileSync(process.env.SYNTH_PC_ROOT+'/pi-input-proof.json',JSON.stringify({task:p.task,sha256:hash,size:data.length}));
fs.appendFileSync(process.env.SYNTH_PC_ROOT+'/pi-count','1');
`
	for path, raw := range map[string][]byte{mapper: []byte(mapping), filepath.Join(bin, "read-input.cjs"): []byte(pi), filepath.Join(bin, "uname"): []byte("#!/bin/sh\nprintf MINGW64_NT-SYNTHETIC\\n\n"), filepath.Join(bin, "cygpath"): []byte("#!/bin/sh\nif [ \"$2\" = \"$SYNTH_WIN_ROOT\" ]; then printf '%s' \"$SYNTH_PC_ROOT\"; else printf '%s' \"$2\"; fi\n"), filepath.Join(bin, "pi"): []byte("#!/bin/sh\ncat > \"$SYNTH_PC_ROOT/task-prompt\"\nnode \"$SYNTH_PI_READER\"\n")} {
		if err := os.WriteFile(path, raw, 0o700); err != nil { // #nosec G306 -- Owner-only executable fixture under t.TempDir is required by the production envelope test.
			t.Fatal(err)
		}
	}
	f.service.relay.SetNativeCaller(func(ctx context.Context, scope agenvo.Scope, target agenvo.Target, method string, args map[string]any) (agenvo.Receipt, error) {
		if method == "pane.run" && args["paneId"] == "job" {
			script := unpackTaskInputScript(t, args["command"].(string))
			cmd := exec.CommandContext(ctx, "/bin/bash", "--noprofile", "--norc", "-p") //nolint:gosec // Actual production generated envelope; isolated fixture peer, no production device/model.
			cmd.Stdin = bytes.NewReader(script)
			cmd.Dir = f.root
			cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "SYNTH_PC_ROOT="+f.root, "SYNTH_WIN_ROOT="+f.base.profile.WorkDir, "SYNTH_PI_READER="+filepath.Join(bin, "read-input.cjs"), "NODE_OPTIONS=--require="+mapper)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("actual generated input envelope: %v %s", err, out)
			}
		}
		return f.base.invoke(ctx, scope, target, method, args)
	})
	result, err := f.service.submit(f.ctx, f.base.binding, f.request())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(f.root, "pi-input-proof.json"))
	var proof struct {
		Task deliverymeta.Task `json:"task"`
		Hash string            `json:"sha256"`
		Size int               `json:"size"`
	}
	if err != nil || json.Unmarshal(raw, &proof) != nil || proof.Task.OperationID != result.ID || proof.Task.BotID != f.base.binding.BotID || proof.Task.SessionID != f.base.binding.SessionID || proof.Task.RootHash != deliverymeta.RootHash(f.base.profile.WorkDir) || proof.Hash != f.asset.ContentHash || proof.Size != len(f.data) {
		t.Fatal("Pi process did not read exact original input/identity", err)
	}
	count, err := os.ReadFile(filepath.Join(f.root, "pi-count"))
	if err != nil || string(count) != "1" {
		t.Fatal("Pi fixture repeated")
	}
	t.Logf("formal submit -> TLS file RPC -> production envelope -> actual child reads %d bytes/%s; same operation/attempt/generation/root; synthetic Pi/Win32 mapping only", proof.Size, proof.Hash)
}

type taskInputProcessConfig struct {
	Binding          toolcontext.ComputerBinding
	Profile          agenvo.NativeHerdrProfile
	Mounts           map[string]dbstore.BotRemoteRuntimeBindingRecord
	Records          map[string]dbstore.ComputerOperationRecord
	Request          Submit
	NAS, PC, Address string
	CA               []byte
}

type taskInputProcessResult struct {
	PID    int    `json:"pid"`
	Result Result `json:"result"`
	Pi     int    `json:"pi"`
	Failed bool   `json:"failed"`
}

func TestTaskInputProcessChild(t *testing.T) {
	path := os.Getenv("MEMOH_TASK_INPUT_PROCESS_FIXTURE")
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path) // #nosec G304 G703 -- This subprocess configuration path is supplied only by the isolated test parent.
	var config taskInputProcessConfig
	if err != nil || json.Unmarshal(raw, &config) != nil {
		t.Fatal("isolated child configuration", err)
	}
	f, _, _ := newNativeTaskFixture(t)
	f.binding, f.profile = config.Binding, config.Profile
	f.store.mounts, f.store.records = config.Mounts, config.Records
	if f.store.records == nil {
		f.store.records = make(map[string]dbstore.ComputerOperationRecord)
	}
	certificate, err := x509.ParseCertificate(config.CA)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(certificate)
	conn, err := grpc.NewClient(config.Address, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{RootCAs: pool, ServerName: "example.com", MinVersion: tls.VersionTLS12})))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	files := taskInputPathView{client: bridge.NewClientFromConn(conn).FilesOnly(), root: config.PC, win: config.Profile.WorkDir}
	relay := agenvo.NewService(f.store, f, f.invoke)
	store := &resumeOperationStore{nativeTaskStore: f.store}
	s := NewService(store, f.store, relay, media.NewService(nil, localfs.New(config.NAS)), func(context.Context, toolcontext.ComputerBinding, string, string, string) error { return nil })
	s.WithInputFiles(func(context.Context, toolcontext.ComputerBinding, string, agenvo.Target) (InputFileSystem, error) {
		return files, nil
	})
	ctx, err := toolcontext.WithComputerBinding(t.Context(), f.binding)
	if err != nil {
		t.Fatal(err)
	}
	result, runErr := s.submit(ctx, f.binding, config.Request)
	config.Records = f.store.records
	raw, _ = json.Marshal(config)
	if err := os.WriteFile(path, raw, 0o600); err != nil { // #nosec G703 -- Journal path is supplied by the isolated test parent only.
		t.Fatal(err)
	}
	out, _ := json.Marshal(taskInputProcessResult{PID: os.Getpid(), Result: result, Pi: f.batch.runs, Failed: runErr != nil})
	_, _ = os.Stdout.Write(append(out, '\n'))
}

func TestTaskInputRealProcessRestartKeepsOriginalAttempt(t *testing.T) {
	f := newTaskInputFixture(t)
	config := taskInputProcessConfig{Binding: f.base.binding, Profile: f.base.profile, Mounts: f.base.store.mounts, Request: f.request(), NAS: f.nas, PC: f.root, Address: f.address, CA: f.ca}
	path := filepath.Join(t.TempDir(), "isolated-process.json")
	write := func() {
		t.Helper()
		raw, _ := json.Marshal(config)
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	run := func() taskInputProcessResult {
		t.Helper()
		write()
		cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestTaskInputProcessChild$") //nolint:gosec // Current test executable, fixed selector and isolated metadata/paths.
		cmd.Env = append(os.Environ(), "MEMOH_TASK_INPUT_PROCESS_FIXTURE="+path)
		out, err := cmd.CombinedOutput()
		var result taskInputProcessResult
		line, _, _ := bytes.Cut(out, []byte{'\n'})
		if err != nil || json.Unmarshal(line, &result) != nil {
			t.Fatalf("input process: %v %s", err, out)
		}
		raw, err := os.ReadFile(path) // #nosec G304 -- Test-owned fixture file; subprocess paths are passed only by this test parent.
		if err != nil || json.Unmarshal(raw, &config) != nil {
			t.Fatal("process journal", err)
		}
		return result
	}
	f.peer.drop.Store(true)
	first := run()
	if !first.Failed || first.Pi != 0 || first.Result.Phase != "pi_inputs_unavailable" || f.peer.writes.Load() != 1 {
		t.Fatal("first process did not retain unknown write")
	}
	var before Details
	if json.Unmarshal(config.Records[first.Result.ID].Details, &before) != nil {
		t.Fatal("original record")
	}
	config.Binding.RunID = "same-session-after-process-restart"
	config.Request.ResumeOperationID = first.Result.ID
	recovered := run()
	var after Details
	if recovered.Failed || recovered.PID == first.PID || recovered.Pi != 1 || recovered.Result.ID != first.Result.ID || recovered.Result.State != "submitted" || f.peer.writes.Load() != 1 || json.Unmarshal(config.Records[first.Result.ID].Details, &after) != nil || after.Batch.Progress.Inputs.Task != before.Batch.Progress.Inputs.Task || after.Batch.Progress.RequestHash != before.Batch.Progress.RequestHash {
		t.Fatal("new process did not preserve original input/task")
	}
	replayed := run()
	if replayed.Failed || replayed.PID == recovered.PID || replayed.Pi != 0 || replayed.Result.ID != first.Result.ID || f.peer.writes.Load() != 1 {
		t.Fatal("later process repeated known task/bytes")
	}
	t.Logf("three real processes; new owning run retains original operation/session/attempt/generation/RootHash/request hash; actual_bytes=%d; TLS file writes=1; total Pi dispatch=1", len(f.data))
}

func TestTaskInputCachedBytesDoNotGrantResume(t *testing.T) {
	for _, kind := range []string{"NAS-revoked", "target-revoked", "changed-root", "changed-request", "canceled-task", "paused-admission", "wrong-user", "wrong-session", "canceled-context"} {
		t.Run(kind, func(t *testing.T) {
			f := newTaskInputFixture(t)
			f.peer.drop.Store(true)
			first, err := f.service.submit(f.ctx, f.base.binding, f.request())
			if !errors.Is(err, ErrDeliveryAsset) || f.peer.writes.Load() != 1 || f.base.batch.runs != 0 {
				t.Fatal("cached uncertain-completion fixture", err)
			}
			store := &resumeOperationStore{nativeTaskStore: f.base.store}
			f.service.store = store
			in, binding, ctx := f.request(), f.base.binding, f.ctx
			in.ResumeOperationID = first.ID
			switch kind {
			case "NAS-revoked":
				f.assets.revoked = true
			case "target-revoked":
				f.base.denied = true
			case "changed-root":
				profile := f.base.profile
				profile.WorkDir = `C:\Another-task-root`
				raw, _ := json.Marshal(map[string]any{"herdr": profile})
				mount := f.base.store.mounts[binding.TargetID]
				mount.RuntimeProviderConfig = raw
				f.base.store.mounts[binding.TargetID] = mount
			case "changed-request":
				in.Task += " changed"
			case "canceled-task":
				record := f.base.store.records[first.ID]
				record.CancelRequested = true
				f.base.store.records[first.ID] = record
			case "paused-admission":
				store.paused = true
			case "wrong-user":
				binding.UserID = "another-user"
			case "wrong-session":
				binding.SessionID = "another-session"
			case "canceled-context":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if _, err := f.service.submit(ctx, binding, in); err == nil || f.peer.writes.Load() != 1 || f.base.batch.runs != 0 {
				t.Fatal("cached input bypassed original grant/admission/identity", err)
			}
			var d Details
			if json.Unmarshal(f.base.store.records[first.ID].Details, &d) != nil || d.Batch.Progress.Inputs == nil {
				t.Fatal("lost original input declaration")
			}
			data, err := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(d.Batch.Progress.Inputs.Path(d.Batch.Progress.Inputs.Files[0]))))
			if err != nil || !bytes.Equal(data, f.data) {
				t.Fatal("rejected resume discarded original bytes", err)
			}
		})
	}
}

func TestTaskInputResumePreservesOpaqueIdentityAndDeliveryLedger(t *testing.T) {
	f := newTaskInputFixture(t)
	f.peer.drop.Store(true)
	in := f.request()
	in.Artifact = &deliverymeta.Artifact{SourceRef: "existing-flat-result", Mime: "text/plain"}
	first, err := f.service.submit(f.ctx, f.base.binding, in)
	if !errors.Is(err, ErrDeliveryAsset) {
		t.Fatal("uncertain input fixture", err)
	}
	record := f.base.store.records[first.ID]
	var details, batch, progress, input, history map[string]json.RawMessage
	if json.Unmarshal(record.Details, &details) != nil || json.Unmarshal(details["batch"], &batch) != nil || json.Unmarshal(batch["progress"], &progress) != nil || json.Unmarshal(progress["inputs"], &input) != nil || json.Unmarshal(progress["history"], &history) != nil {
		t.Fatal("original details")
	}
	opaque := json.RawMessage(`{"original_sequence":9007199254740993,"opaque":"preserve exactly"}`)
	details["future_task_identity"], details["deliveries"] = opaque, opaque
	batch["future_batch"], input["future_input"], history["future_history"] = opaque, opaque, opaque
	progress["inputs"], _ = json.Marshal(input)
	progress["history"], _ = json.Marshal(history)
	batch["progress"], _ = json.Marshal(progress)
	details["batch"], _ = json.Marshal(batch)
	record.Details, _ = json.Marshal(details)
	f.base.store.records[first.ID] = record
	wantInput, wantProducer := progress["inputs"], progress["producer"]
	f.service.store = &resumeOperationStore{nativeTaskStore: f.base.store}
	in.ResumeOperationID = first.ID
	recovered, err := f.service.submit(f.ctx, f.base.binding, in)
	if err != nil || recovered.State != "submitted" || f.peer.writes.Load() != 1 || f.base.batch.runs != 1 {
		t.Fatal("input continuation", err)
	}
	if json.Unmarshal(f.base.store.records[first.ID].Details, &details) != nil || json.Unmarshal(details["batch"], &batch) != nil || json.Unmarshal(batch["progress"], &progress) != nil || json.Unmarshal(progress["history"], &history) != nil {
		t.Fatal("continued details")
	}
	for _, actual := range []json.RawMessage{details["future_task_identity"], details["deliveries"], batch["future_batch"], history["future_history"]} {
		if !bytes.Equal(actual, opaque) {
			t.Fatal("opaque ledger/large numeric identity changed", string(actual))
		}
	}
	if !bytes.Equal(progress["inputs"], wantInput) || !bytes.Equal(progress["producer"], wantProducer) {
		t.Fatal("original input/output declarations changed")
	}
}

func TestTaskInputActivePreparationCannotAcquireSecondWriter(t *testing.T) {
	f := newTaskInputFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var resolves atomic.Int32
	resolve := f.service.inputFiles
	f.service.inputFiles = func(ctx context.Context, b toolcontext.ComputerBinding, id string, target agenvo.Target) (InputFileSystem, error) {
		if resolves.Add(1) == 1 {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return resolve(ctx, b, id, target)
	}
	f.service.store = &resumeOperationStore{nativeTaskStore: f.base.store}
	type answer struct {
		result Result
		err    error
	}
	finished := make(chan answer, 1)
	go func() {
		result, err := f.service.submit(f.ctx, f.base.binding, f.request())
		finished <- answer{result, err}
	}()
	select {
	case <-entered:
	case <-t.Context().Done():
		t.Fatal("preparation did not reach actual file resolver")
	}
	f.base.store.mu.Lock()
	var id string
	for key := range f.base.store.records {
		id = key
	}
	f.base.store.mu.Unlock()
	in := f.request()
	in.ResumeOperationID = id
	_, resumeErr := f.service.submit(f.ctx, f.base.binding, in)
	close(release)
	first := <-finished
	if !errors.Is(resumeErr, agenvo.ErrUnknown) || first.err != nil || first.result.State != "submitted" || resolves.Load() != 1 || f.peer.writes.Load() != 1 || f.base.batch.runs != 1 {
		t.Fatal("live preparation acquired a second writer", resumeErr, first.err, resolves.Load(), f.peer.writes.Load(), f.base.batch.runs)
	}
}

func TestTaskInputExpiredPreparationResumesWithoutRewriting(t *testing.T) {
	f := newTaskInputFixture(t)
	f.peer.drop.Store(true)
	first, err := f.service.submit(f.ctx, f.base.binding, f.request())
	if !errors.Is(err, ErrDeliveryAsset) {
		t.Fatal("uncertain atomic input fixture", err)
	}
	record := f.base.store.records[first.ID]
	var d Details
	if json.Unmarshal(record.Details, &d) != nil {
		t.Fatal("input record")
	}
	// Simulate the persisted stage of a preparer whose bounded work process
	// ended before it could save the atomic writer's unknown response.
	d.InputPreparation, d.Phase = "preparing", "pi_inputs_preparing"
	d.InputPreparationUntil = time.Now().Add(-time.Minute)
	record.State = "dispatching"
	record.Details, _ = json.Marshal(d)
	f.base.store.records[first.ID] = record
	f.service.store = &resumeOperationStore{nativeTaskStore: f.base.store}
	in := f.request()
	in.ResumeOperationID = first.ID
	recovered, err := f.service.submit(f.ctx, f.base.binding, in)
	if err != nil || recovered.ID != first.ID || recovered.State != "submitted" || f.peer.writes.Load() != 1 || f.base.batch.runs != 1 {
		t.Fatal("expired original preparation was rewritten or replayed", err)
	}
}

func TestTaskInputDriveRootRemainsAbsolute(t *testing.T) {
	f := newTaskInputFixture(t)
	f.base.profile.WorkDir = `C:\`
	mount := f.base.store.mounts[f.base.binding.TargetID]
	mount.RuntimeProviderConfig, _ = json.Marshal(map[string]any{"herdr": f.base.profile})
	f.base.store.mounts[f.base.binding.TargetID] = mount
	f.files.win = f.base.profile.WorkDir
	f.service.WithInputFiles(func(_ context.Context, _ toolcontext.ComputerBinding, _ string, target agenvo.Target) (InputFileSystem, error) {
		if target.WorkDir != `C:\` {
			t.Fatal("selected drive root changed")
		}
		return f.files, nil
	})
	result, err := f.service.submit(f.ctx, f.base.binding, f.request())
	if err != nil || result.State != "submitted" || f.peer.writes.Load() != 1 || f.base.batch.runs != 1 {
		t.Fatal("drive root became drive-relative", result.Phase, err)
	}
	data, err := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(result.Inputs.Files[0].Path)))
	if err != nil || !bytes.Equal(data, f.data) {
		t.Fatal("drive root file differs", err)
	}
}

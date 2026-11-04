package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	pathpkg "path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	sdk "github.com/felinics/twilight/sdk"

	"github.com/felinics/memoh/internal/agent/background"
	"github.com/felinics/memoh/internal/agent/toolexec"
	"github.com/felinics/memoh/internal/computertarget"
	"github.com/felinics/memoh/internal/hooks"
	"github.com/felinics/memoh/internal/toolcontext"
	workspacepkg "github.com/felinics/memoh/internal/workspace"
	"github.com/felinics/memoh/internal/workspace/bridge"
	pb "github.com/felinics/memoh/internal/workspace/bridgepb"
)

// blockedSleepPattern matches standalone `sleep N` where N >= 2.
// Does not match sleep inside pipelines, subshells, or scripts.
var blockedSleepPattern = regexp.MustCompile(`^sleep\s+(\d+(?:\.\d+)?)(?:\s*[;&]|$)`)

const (
	defaultContainerExecWorkDir  = "/data"
	remoteBackgroundOutputLogDir = "/data/.memoh/background"
	minCommandExecBudgetSeconds  = 30
)

// containerOpTimeout is the maximum time allowed for individual file
// operations (read, write, list, edit). Exec has its own timeout.
const containerOpTimeout = 30 * time.Second

// largeFileThreshold defines the size above which file operations use
// streaming (async chunked I/O) instead of loading fully into memory.
// Files <= this threshold use the simpler synchronous gRPC calls.
const largeFileThreshold = 512 * 1024 // 512 KB

// diffPreReadTimeout bounds the best-effort read of a file's previous content
// for the UI diff in execWrite. It must stay short: the read is display
// sugar and runs on its own context so it cannot starve the real write.
const diffPreReadTimeout = 5 * time.Second

type ContainerProvider struct {
	clients     bridge.Provider
	bgManager   *background.Manager
	hookService workspaceHookService
	execWorkDir string
	logger      *slog.Logger
	transcriber ReadTranscriber
}

type workspaceHookService interface {
	Run(ctx context.Context, req hooks.Request, runner hooks.ToolRunner) (hooks.Result, error)
}

func NewContainerProvider(log *slog.Logger, clients bridge.Provider, bgManager *background.Manager, execWorkDir string, hookServices ...*hooks.Service) *ContainerProvider {
	if log == nil {
		log = slog.Default()
	}
	wd := strings.TrimSpace(execWorkDir)
	if wd == "" {
		wd = defaultContainerExecWorkDir
	}
	var hookService *hooks.Service
	if len(hookServices) > 0 {
		hookService = hookServices[0]
	}
	return &ContainerProvider{clients: clients, bgManager: bgManager, hookService: hookService, execWorkDir: wd, logger: log.With(slog.String("tool", "container"))}
}

func (p *ContainerProvider) SetHookService(h *hooks.Service) {
	p.hookService = h
}

func (*ContainerProvider) Usage(_ context.Context, session SessionContext, available AvailableTools) string {
	var parts []string
	locationRef, hasLocationTool := available.Ref(ToolListExecutionLocations())
	if hasLocationTool {
		parts = append(parts,
			locationRef+": lists the Server Workspace and connected computers authorized for this Bot. Inspect these locations before claiming no computer is configured (including an unnamed Windows PC).",
			"Check `provider`, `capabilities`, `available`, `status`. Authorization is not availability: an offline target is authorized but unreachable; a missing target is not exposed to this Bot. Never substitute another location after a target fails.",
			"`exec_foreground` routes foreground Bash through `computer_task_submit(kind=foreground_bash)`, not ordinary `exec`. This capability alone does not promise filesystem or desktop support; use only advertised capabilities; prefer `.` or root-relative working directories. Do not launch an interactive coding agent through foreground exec.",
		)
		if skillRef, ok := available.Ref(ToolUseSkill()); ok {
			parts = append(parts, "For connected-computer or local-agent tasks, use "+skillRef+" when a skill covers the workflow; skills explain usage but grant no target or connector access.")
		}
	}
	if targetID := strings.TrimSpace(session.WorkspaceTargetID); targetID != "" {
		targetDescription := "request-selected execution location"
		if sessionUsesRemoteWorkspaceTarget(session) {
			targetDescription = "request-selected connected computer"
		} else if strings.EqualFold(strings.TrimSpace(session.WorkspaceTargetKind), workspacepkg.WorkspaceTargetNative) || strings.EqualFold(targetID, workspacepkg.WorkspaceTargetNative) {
			targetDescription = "native Server Workspace"
		}
		if name := strings.TrimSpace(session.WorkspaceTargetName); name != "" {
			targetDescription += " " + strconv.Quote(name)
		}
		text := "File and command tools default to the " + targetDescription + " for this turn"
		if sessionUsesRemoteWorkspaceTarget(session) {
			text += ". This PC conversation pins every computer tool to this location; other computers cannot be used"
		} else {
			text += ". An explicit `target_id` still takes precedence"
		}
		parts = append(parts, text+".")
	}
	if workDir := strings.TrimSpace(session.WorkdirPath); workDir != "" {
		parts = append(parts, "This chat's working directory is "+strconv.Quote(workDir)+". Relative file paths resolve inside it, and commands run there by default.")
	}
	if ref, ok := available.Ref(ToolRead()); ok {
		text := ref + ": read file content"
		if session.SupportsImageInput {
			text += " (also supports images: PNG, JPEG, GIF, WebP)"
		}
		parts = append(parts, text)
	}
	if ref, ok := available.Ref(ToolWrite()); ok {
		parts = append(parts, ref+": write file content")
	}
	if ref, ok := available.Ref(ToolList()); ok {
		parts = append(parts, ref+": list directory entries")
	}
	if ref, ok := available.Ref(ToolEdit()); ok {
		parts = append(parts, ref+": replace exact text in a file")
	}
	if ref, ok := available.Ref(ToolApplyPatch()); ok {
		parts = append(parts, ref+": apply a patch to files")
	}
	if ref, ok := available.Ref(ToolExec()); ok {
		parts = append(parts, ref+": execute command")
	}
	if hasLocationTool && len(available.Refs(ToolRead(), ToolWrite(), ToolList(), ToolEdit(), ToolApplyPatch(), ToolExec())) > 0 {
		if sessionUsesRemoteWorkspaceTarget(session) {
			parts = append(parts, "Use "+locationRef+" to check this conversation's selected PC. Omit `target_id` or pass only that PC's returned ID; other PCs and the Server Workspace are outside this conversation's scope.")
		} else {
			defaultDescription := "the Bot's default"
			if strings.TrimSpace(session.WorkspaceTargetID) != "" {
				defaultDescription = "the request-selected default for this turn"
			}
			parts = append(parts, "Use "+locationRef+" when the user asks for a computer, a non-default location fits, availability may have changed, or a target call fails. Pass `target_id` only to tools that location supports; omit it to use "+defaultDescription+". Listing does not switch locations or folders; if intent is ambiguous, clarify before executing.")
		}
	}
	if displayRefs := available.Refs(ToolBrowserObserve(), ToolBrowserAction(), ToolBrowserRemoteSession(), ToolComputerObserve(), ToolComputerAction()); len(displayRefs) > 0 {
		parts = append(parts, "Browser Use and Computer Use ("+strings.Join(displayRefs, ", ")+") control the Server Workspace desktop, not a user's PC; they are not a fallback for missing PC GUI support.")
	}
	// Workspace links are a Web/Desktop affordance, not a portable channel URL.
	// Other execution locations must not be advertised as the native workspace.
	// Any workspace that reports a native target gets these instructions, a
	// hosted sandbox included, so its file viewer and localhost preview must
	// open these links.
	if (session.CurrentPlatform == "local" || session.CurrentPlatform == "web") && !sessionUsesRemoteWorkspaceTarget(session) {
		if len(available.Refs(ToolRead(), ToolWrite(), ToolEdit(), ToolApplyPatch())) > 0 {
			parts = append(parts, "In Memoh Web/Desktop, link Server Workspace files as Markdown with the real absolute path, e.g. [View source](/data/project/index.html). Destinations start with / (relative cannot open); encode spaces; no emoji/icons in labels (Memoh renders icons); no Memoh site URL or file:// prefix; not for another computer's files. Links open the workspace file viewer, not a running page. Name unlinked files in backticks (`summary.md`).")
		}
		if _, ok := available.Ref(ToolExec()); ok {
			parts = append(parts, "For a delivered web app, link its verified localhost URL with explicit port, e.g. [Try it](http://localhost:5173/); Memoh previews it in-app, so the user does not need to configure port forwarding. Public HTTP(S) links open in the user's browser. Never invent a proxy address or claim a service is running without checking.")
		}
		if len(available.Refs(ToolRead(), ToolWrite(), ToolEdit(), ToolApplyPatch(), ToolExec())) > 0 {
			parts = append(parts, "Lead with what was delivered: one or two natural sentences, links labeled \"Try it\"/\"View source\"/\"Download report\" (not \"click here\" or bare paths); one deliverable's links stay in one sentence; a list only for multiple independent deliverables. Claim only checks actually performed.")
		}
	}
	return usageSection("Basic Tools", parts)
}

func (p *ContainerProvider) Tools(ctx context.Context, session SessionContext) ([]toolexec.Tool, error) {
	workspace := p.resolveToolWorkspace(ctx, session)
	// Tool descriptions tell the model where relative paths land; with a
	// workdir bound, that is the working directory.
	wd := workspace.defaultWorkDir
	if workspace.workdirPath != "" {
		wd = workspace.workdirPath
	}
	sess := session
	targetShape := toolexec.Describe("target_id", workspaceTargetDescription(sess))
	filePathShape := toolexec.Describe("path", fmt.Sprintf("File path (relative to %s or absolute %s)", wd, workspace.absolutePathDescription))

	readDesc := fmt.Sprintf("Read file content %s. Full file by default; line_offset and n_lines paginate. Files up to ~16 MB.", workspace.locationDescription)
	if sess.SupportsImageInput {
		readDesc += " Also supports reading image files (PNG, JPEG, GIF, WebP)."
	}
	readDesc += " Spreadsheet (.xlsx) and Word (.docx) files are converted to plain text (large files truncated)."
	readDesc += " Audio files are transcribed to text when a transcription model is configured."
	readDesc += " Video files return a reference (mime, size, content hash) instead of content; hand it to a specialist bot via delegate_to_bot attachments."

	toolList := []toolexec.Tool{
		toolexec.Define(ToolRead().String(), readDesc,
			func(ctx *toolexec.ToolExecContext, args readArgs) (sdk.ToolOutput, error) {
				return toolexec.OutputPair(p.execRead(ctx.Context, sess, args))
			},
			targetShape, filePathShape,
			toolexec.Minimum("line_offset", 1), toolexec.Default("line_offset", 1),
			toolexec.Minimum("n_lines", 1),
		),
		toolexec.Define(ToolWrite().String(), fmt.Sprintf("Write file content %s. Creates parent directories; any file size.", workspace.locationDescription),
			func(ctx *toolexec.ToolExecContext, args writeArgs) (sdk.ToolOutput, error) {
				return toolexec.OutputPair(p.execWrite(ctx.Context, sess, args))
			},
			targetShape, filePathShape,
		),
		toolexec.Define(ToolList().String(), fmt.Sprintf("List directory entries %s. Paginated; max %d entries per call. Recursive mode collapses subdirectories with more than %d items to a summary.", workspace.locationDescription, listMaxEntries, listCollapseThreshold),
			func(ctx *toolexec.ToolExecContext, args listArgs) (sdk.ToolOutput, error) {
				return toolexec.OutputPair(p.execList(ctx.Context, sess, args))
			},
			targetShape,
			toolexec.Describe("path", fmt.Sprintf("Directory path (relative to %s or absolute %s)", wd, workspace.absolutePathDescription)),
			toolexec.Minimum("offset", 0), toolexec.Default("offset", 0),
			toolexec.Describe("limit", fmt.Sprintf("Entries per call; default and max %d.", listMaxEntries)),
			toolexec.Range("limit", 1, listMaxEntries), toolexec.Default("limit", listMaxEntries),
		),
		toolexec.Define(ToolEdit().String(), fmt.Sprintf("Replace exact text in a file %s.", workspace.locationDescription),
			func(ctx *toolexec.ToolExecContext, args editArgs) (sdk.ToolOutput, error) {
				return toolexec.OutputPair(p.execEdit(ctx.Context, sess, args))
			},
			targetShape, filePathShape,
		),
		{
			Name: ToolApplyPatch().String(),
			Description: fmt.Sprintf(`Apply a structured patch %s — not a unified diff. For multi-file edits, structured changes, file creation, deletion, or moves.

Grammar:
- Starts with "*** Begin Patch", ends with "*** End Patch".
- "*** Add File: path": following content lines each start with "+".
- "*** Delete File: path".
- "*** Update File: path"; "*** Move to: new/path" directly after the header renames the file.
- In update hunks, "@@" or "@@ context line" precedes changed lines; line prefixes: " " unchanged, "-" removed, "+" added; "*** End of File" marks the end of the file.
- Paths are workspace-relative by default; absolute paths only when the backend allows.

Example:
*** Begin Patch
*** Update File: path
@@
-old line
+new line
*** End Patch
`, workspace.locationDescription),
			Parameters: toolexec.SchemaFor[applyPatchArgs](targetShape),
			// apply_patch keeps the raw arguments: a JSON string document
			// carrying the patch text is accepted as well as the object.
			Execute: func(ctx *toolexec.ToolExecContext, input sdk.ToolArguments) (sdk.ToolOutput, error) {
				return toolexec.OutputPair(p.execApplyPatch(ctx.Context, sess, input))
			},
		},
		toolexec.Define(ToolExec().String(), fmt.Sprintf(`Execute a %s command %s. Runs in %s by default.

# Instructions
%s
- For long commands (installs, builds, test suites) set run_in_background to true; the call returns a task ID immediately (no trailing '&').
- wait_until(task_id) returns a reason (completed/failed/killed/stalled/idle/timeout) plus the latest output_tail; get_background_status(task_id) inspects the result.
- For processes that never exit (dev servers, watch mode), run in background and wait_until(task_id): reason 'idle' with a ready message (e.g. a local URL) in output_tail means it is up — proceed, do not wait for completion.
- timeout (up to %d seconds) raises the default %d-second foreground wait; a command still running past it moves to the background.
- No delay loops: do not sleep between immediately runnable commands; do not retry failing commands in a loop — diagnose the cause.
%s`, workspace.shellDescription, workspace.locationDescription, wd, workspace.platformInstructions, background.MaxExecTimeout, background.DefaultExecTimeout, workspace.delayInstruction),
			func(ctx *toolexec.ToolExecContext, args execArgs) (sdk.ToolOutput, error) {
				return toolexec.OutputPair(p.execExec(ctx.Context, sess, args))
			},
			targetShape,
			toolexec.Describe("command", fmt.Sprintf("Command to run (e.g. %s)", workspace.commandExamples)),
			toolexec.Describe("work_dir", fmt.Sprintf("Working directory (default: %s)", wd)),
			toolexec.Describe("description", workspace.descriptionExamples),
			toolexec.Describe("timeout", fmt.Sprintf("Foreground wait in seconds (default %d, max %d); a still-running command then moves to the background, possibly earlier to preserve max_duration_seconds.", background.DefaultExecTimeout, background.MaxExecTimeout)),
			toolexec.Range("timeout", 1, float64(background.MaxExecTimeout)),
			toolexec.Range("max_duration_seconds", minCommandExecBudgetSeconds, float64(background.MaxBackgroundExecTimeout)),
			toolexec.EnumStrings("background_mode", []string{"task", "service"}),
		),
	}
	if resolver, ok := p.clients.(workspaceTargetResolver); ok {
		locationTool := toolexec.Tool{
			Name: ToolListExecutionLocations().String(),
			Description: "List this Bot's execution locations for file operations and command execution, with availability and status. " +
				"A PC conversation lists only its selected PC; a Server Workspace conversation may list its Bot-authorized PCs. " +
				"Check provider and capabilities before choosing a tool: exec_foreground means foreground Bash goes through computer_task_submit(kind=foreground_bash), not ordinary exec, and alone does not promise filesystem or desktop support. " +
				"default marks the current turn's default (request-selected target, else the Bot's Primary); available says whether it can currently be used. " +
				"Managed PC tasks go through computer_task_submit; ordinary exec cannot bypass it. Other workspace providers accept target_id on file/command tools. " +
				"This tool changes neither the default location nor the starting folder.",
			Parameters: toolexec.SchemaFromValue(emptyObjectSchema()),
			Execute: func(ctx *toolexec.ToolExecContext, _ sdk.ToolArguments) (sdk.ToolOutput, error) {
				ctx.Context = workspaceContextForSession(ctx.Context, sess)
				targets, err := resolver.ListWorkspaceTargets(ctx.Context, sess.BotID)
				if err != nil {
					return sdk.ToolOutput{}, fmt.Errorf("list execution locations: %w", err)
				}
				locations := make([]executionLocation, 0, len(targets))
				for _, target := range targets {
					if strings.TrimSpace(target.TargetID) == "" {
						continue
					}
					if toolcontext.CheckComputerTarget(ctx.Context, target.TargetID) != nil {
						continue
					}
					locations = append(locations, executionLocationFromTarget(target, sess.WorkspaceTargetID))
				}
				return toolexec.OutputFromValue(listExecutionLocationsResult{Locations: locations}), nil
			},
		}
		toolList = append([]toolexec.Tool{locationTool}, toolList...)
	}
	return toolList, nil
}

// Workspace tool arguments. target_id is described per session (see
// workspaceTargetDescription), path text names the working directory, so
// both are set by shape functions rather than tags.
type readArgs struct {
	TargetID   string `json:"target_id,omitempty"`
	Path       string `json:"path"`
	LineOffset *int   `json:"line_offset,omitempty" jsonschema:"First line to read (1-indexed); default 1."`
	NLines     *int   `json:"n_lines,omitempty" jsonschema:"Lines to read; default entire file."`
}

type writeArgs struct {
	TargetID string `json:"target_id,omitempty"`
	Path     string `json:"path"`
	Content  string `json:"content" jsonschema:"File content"`
}

type listArgs struct {
	TargetID  string `json:"target_id,omitempty"`
	Path      string `json:"path"`
	Recursive bool   `json:"recursive,omitempty" jsonschema:"List recursively"`
	Offset    *int   `json:"offset,omitempty" jsonschema:"Entry offset (0-indexed); default 0."`
	Limit     *int   `json:"limit,omitempty"`
}

type editArgs struct {
	TargetID string `json:"target_id,omitempty"`
	Path     string `json:"path"`
	OldText  string `json:"old_text" jsonschema:"Exact text to find"`
	NewText  string `json:"new_text" jsonschema:"Replacement text"`
}

type applyPatchArgs struct {
	TargetID string `json:"target_id,omitempty"`
	Patch    string `json:"patch" jsonschema:"Patch body in apply_patch format; paths workspace-relative by default."`
}

type execArgs struct {
	TargetID           string `json:"target_id,omitempty"`
	Command            string `json:"command"`
	WorkDir            string `json:"work_dir,omitempty"`
	Description        string `json:"description,omitempty"`
	Timeout            *int   `json:"timeout,omitempty"`
	MaxDurationSeconds *int   `json:"max_duration_seconds,omitempty" jsonschema:"Total budget including foreground time (min 30, default 7200, max 86400); backgrounding does not restart it."`
	BackgroundMode     string `json:"background_mode,omitempty" jsonschema:"task (default) uses the execution budget; service runs until stopped or the workspace closes (requires run_in_background=true, no max_duration_seconds)."`
	RunInBackground    bool   `json:"run_in_background,omitempty" jsonschema:"Run in background; returns a task ID immediately (no trailing '&'). For long commands (installs, builds, tests) and non-exiting processes (servers, watch mode). Inspect with wait_until(task_id), then get_background_status(task_id)."`
}

type toolWorkspace struct {
	defaultWorkDir string
	// workdirPath is the session's working directory. It changes where
	// relative tool paths land and what cwd exec defaults to; it never
	// replaces defaultWorkDir, which stays the workspace root that
	// normalizePath strips (the bridge server re-joins its own root, so
	// moving the strip prefix would silently relocate files).
	workdirPath             string
	locationDescription     string
	absolutePathDescription string
	shellDescription        string
	commandExamples         string
	descriptionExamples     string
	platformInstructions    string
	delayInstruction        string
	windows                 bool
}

type resolvedToolTarget struct {
	execute   func(context.Context, computertarget.Request) (computertarget.Result, error)
	id        string
	client    *bridge.Client
	info      bridge.WorkspaceInfo
	workspace toolWorkspace
}

func (t resolvedToolTarget) hookWorkspaceInfo(fallbackWorkDir string) hooks.WorkspaceInfo {
	info := hookWorkspaceInfoFromBridge(t.info, fallbackWorkDir)
	// Hooks observe the directory tools actually work in, which the
	// session's workdir overrides.
	if t.workspace.workdirPath != "" {
		info.CWD = t.workspace.workdirPath
	}
	return info
}

func (t resolvedToolTarget) backgroundOutputDir() string {
	if strings.EqualFold(strings.TrimSpace(t.info.Backend), bridge.WorkspaceBackendRemote) {
		return remoteBackgroundOutputLogDir
	}
	return background.OutputLogDir
}

type workspaceTargetResolver interface {
	ResolveWorkspaceTarget(ctx context.Context, botID, targetID string) (workspacepkg.ResolvedWorkspaceTarget, error)
	ListWorkspaceTargets(ctx context.Context, botID string) ([]workspacepkg.WorkspaceTarget, error)
}

type executionLocation struct {
	Provider     string   `json:"provider,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	TargetID     string   `json:"target_id"`
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Default      bool     `json:"default"`
	Available    bool     `json:"available"`
	Status       string   `json:"status"`
}

type listExecutionLocationsResult struct {
	Locations []executionLocation `json:"locations"`
}

func (p *ContainerProvider) resolveToolWorkspace(ctx context.Context, session SessionContext) toolWorkspace {
	return toolWorkspaceFromInfo(p.resolveToolWorkspaceInfo(ctx, session), p.execWorkDir, session.WorkdirPath)
}

func (p *ContainerProvider) resolveToolWorkspaceInfo(ctx context.Context, session SessionContext) bridge.WorkspaceInfo {
	ctx = workspaceContextForSession(ctx, session)
	info := bridge.WorkspaceInfo{
		Backend:        bridge.WorkspaceBackendContainer,
		DefaultWorkDir: p.execWorkDir,
	}
	if resolver, ok := p.clients.(bridge.WorkspaceInfoProvider); ok {
		if resolved, err := resolver.WorkspaceInfo(ctx, session.BotID); err == nil {
			info = resolved
		}
	}
	return info
}

func toolWorkspaceFromInfo(info bridge.WorkspaceInfo, fallbackWorkDir, workdirPath string) toolWorkspace {
	wd := strings.TrimSpace(info.DefaultWorkDir)
	if wd == "" {
		wd = fallbackWorkDir
	}
	workspace := toolWorkspace{
		defaultWorkDir:          wd,
		workdirPath:             strings.TrimSpace(workdirPath),
		locationDescription:     "inside the bot workspace",
		absolutePathDescription: "inside the workspace",
		shellDescription:        "shell",
		commandExamples:         "ls -la, npm install, python script.py",
		descriptionExamples:     `Short active-voice description of the command, e.g. ls -la → "List files with details".`,
		delayInstruction:        "  - sleep N (N >= 2) in foreground is blocked; keep genuine delays under 2 seconds.",
	}
	if strings.EqualFold(info.Backend, bridge.WorkspaceBackendRemote) {
		workspace.locationDescription = "on the connected remote machine"
		workspace.absolutePathDescription = "remote workspace path"
		if strings.EqualFold(info.OS, "win32") {
			workspace.windows = true
			workspace.shellDescription = "Windows Command Prompt (cmd.exe)"
			workspace.commandExamples = "dir, npm install, python script.py"
			workspace.descriptionExamples = `Short active-voice description of the command, e.g. dir → "List files and folders".`
			workspace.platformInstructions = `- This remote machine uses Windows Command Prompt (cmd.exe). Use Windows syntax (dir, type, where); POSIX commands (ls, cat, pwd, sleep) are not installed.
- Do not use start to detach a command. Use run_in_background so the process tree stays owned.`
			workspace.delayInstruction = "  - Do not use timeout /t or ping loops to wait for background work; use wait_until."
		}
	}
	return workspace
}

func workspaceTargetDescription(session SessionContext) string {
	description := "Exact target_id returned by list_execution_locations. Do not pass a location name, type, or runtime ID. Omit to use the default location for the current turn."
	if sessionUsesRemoteWorkspaceTarget(session) {
		description = "This PC conversation is restricted to its selected computer: omit this parameter or pass only that computer's target_id; other PCs and native are rejected. A target_id is a Memoh reference, never a transport deviceId or instanceId."
	} else if sessionIsWorkdirBound(session) && strings.TrimSpace(session.WorkspaceTargetID) == "" {
		description = "This chat has a fixed working directory without a resolved target: omit this parameter."
	}
	return description
}

// sessionIsWorkdirBound reports whether the session derives its working
// directory from a bot workdir. The resolved path is the binding's only
// per-session trace in tool land, so it doubles as the binding flag.
func sessionIsWorkdirBound(session SessionContext) bool {
	return strings.TrimSpace(session.WorkdirPath) != ""
}

// PC conversations cannot cross computers, even without a bound directory.
// Native conversations may use Bot-authorized PCs; workdirPathForTarget keeps
// their native directory from leaking into a different machine's path base.
func ensureWorkdirPinnedTarget(session SessionContext, requestedTargetID string) error {
	requested := strings.TrimSpace(requestedTargetID)
	pinned := strings.TrimSpace(session.WorkspaceTargetID)
	if requested != "" && sessionUsesRemoteWorkspaceTarget(session) && !strings.EqualFold(pinned, requested) {
		return toolcontext.ErrComputerScope
	}
	if requested != "" && pinned == "" && sessionIsWorkdirBound(session) {
		return errors.New("this chat has a fixed working directory, which pins its execution location; omit target_id")
	}
	return nil
}

// workdirPathForTarget returns the working directory only for the target the
// workdir actually lives on. A native chat can target an authorized PC,
// where its directory does not exist; carrying it there
// would resolve every relative path against a phantom directory.
func workdirPathForTarget(session SessionContext, resolvedTargetID string) string {
	workDir := strings.TrimSpace(session.WorkdirPath)
	pinned := strings.TrimSpace(session.WorkspaceTargetID)
	resolved := strings.TrimSpace(resolvedTargetID)
	if workDir == "" || pinned == "" || resolved == "" || strings.EqualFold(pinned, resolved) {
		return workDir
	}
	return ""
}

func executionLocationFromTarget(target workspacepkg.WorkspaceTarget, requestTargetID string) executionLocation {
	status := strings.TrimSpace(target.Status)
	if status == "" {
		if target.Online {
			status = workspacepkg.WorkspaceTargetStatusOnline
		} else {
			status = workspacepkg.WorkspaceTargetStatusOffline
		}
	}
	name := strings.TrimSpace(target.Name)
	targetType := "execution_location"
	switch target.Kind {
	case workspacepkg.WorkspaceTargetNative:
		targetType = "server_workspace"
		if name == "" {
			name = "Server Workspace"
		}
	case workspacepkg.WorkspaceTargetRemote:
		targetType = "connected_computer"
		if name == "" {
			name = "Unavailable connected computer"
		}
	}
	requestTargetID = strings.TrimSpace(requestTargetID)
	isDefault := target.Primary
	if requestTargetID != "" {
		isDefault = strings.TrimSpace(target.TargetID) == requestTargetID
	}
	return executionLocation{
		Provider:     target.Provider,
		Capabilities: target.Capabilities,
		TargetID:     strings.TrimSpace(target.TargetID),
		Name:         name,
		Type:         targetType,
		Default:      isDefault,
		Available:    status == workspacepkg.WorkspaceTargetStatusOnline,
		Status:       status,
	}
}

func (p *ContainerProvider) resolveToolTarget(ctx context.Context, session SessionContext, requestedTargetID string) (resolvedToolTarget, error) {
	ctx = workspaceContextForSession(ctx, session)
	targetID := strings.TrimSpace(requestedTargetID)
	if err := ensureWorkdirPinnedTarget(session, targetID); err != nil {
		return resolvedToolTarget{}, err
	}
	if err := toolcontext.CheckComputerTarget(ctx, targetID); err != nil {
		return resolvedToolTarget{}, err
	}
	if resolver, ok := p.clients.(workspaceTargetResolver); ok {
		resolved, err := resolver.ResolveWorkspaceTarget(ctx, session.BotID, targetID)
		if err != nil {
			if errors.Is(err, workspacepkg.ErrWorkspaceTargetNotFound) {
				return resolvedToolTarget{}, fmt.Errorf("execution location target_id is invalid or is no longer configured for this Bot; call %s and retry with a returned target_id: %w", ToolListExecutionLocations().String(), err)
			}
			return resolvedToolTarget{}, fmt.Errorf("workspace target is not reachable: %w", err)
		}
		if err := toolcontext.CheckComputerTarget(ctx, resolved.TargetID); err != nil {
			return resolvedToolTarget{}, err
		}
		if resolved.Client == nil {
			return resolvedToolTarget{}, errors.New("workspace target is not reachable: client is unavailable")
		}
		return resolvedToolTarget{
			execute:   resolved.Execute,
			id:        resolved.TargetID,
			client:    resolved.Client,
			info:      resolved.Info,
			workspace: toolWorkspaceFromInfo(resolved.Info, p.execWorkDir, workdirPathForTarget(session, resolved.TargetID)),
		}, nil
	}
	if targetID != "" {
		return resolvedToolTarget{}, errors.New("workspace target selection is not supported")
	}
	client, err := p.getClient(ctx, session.BotID)
	if err != nil {
		return resolvedToolTarget{}, err
	}
	info := p.resolveToolWorkspaceInfo(ctx, session)
	return resolvedToolTarget{
		client:    client,
		info:      info,
		workspace: toolWorkspaceFromInfo(info, p.execWorkDir, session.WorkdirPath),
	}, nil
}

func workspaceContextForSession(ctx context.Context, session SessionContext) context.Context {
	targetID := strings.TrimSpace(session.WorkspaceTargetID)
	if targetID == "" {
		return ctx
	}
	return toolcontext.WithComputerScope(workspacepkg.WithWorkspaceTarget(ctx, targetID), targetID)
}

func sessionUsesRemoteWorkspaceTarget(session SessionContext) bool {
	kind := strings.TrimSpace(session.WorkspaceTargetKind)
	if kind != "" {
		return strings.EqualFold(kind, workspacepkg.WorkspaceTargetRemote)
	}
	targetID := strings.TrimSpace(session.WorkspaceTargetID)
	return targetID != "" && !strings.EqualFold(targetID, workspacepkg.WorkspaceTargetNative)
}

func hookWorkspaceInfoFromBridge(info bridge.WorkspaceInfo, fallbackWorkDir string) hooks.WorkspaceInfo {
	cwd := strings.TrimSpace(info.DefaultWorkDir)
	if cwd == "" {
		cwd = strings.TrimSpace(fallbackWorkDir)
	}
	if cwd == "" {
		cwd = hooks.DefaultWorkDir
	}
	runtime := strings.TrimSpace(info.Backend)
	if runtime == "" {
		runtime = bridge.WorkspaceBackendContainer
	}
	return hooks.WorkspaceInfo{CWD: cwd, Runtime: runtime}
}

func (p *ContainerProvider) runWorkspaceToolHook(ctx context.Context, session SessionContext, workspace hooks.WorkspaceInfo, eventName string, extra map[string]any) (hooks.Result, error) {
	if p == nil || p.hookService == nil {
		return hooks.Result{Decision: hooks.DecisionAllow, RuntimeSupported: hooks.RuntimeSupported(eventName)}, nil
	}
	req := hooks.Request{
		Version:   1,
		Event:     eventName,
		BotID:     session.BotID,
		SessionID: session.SessionID,
		ChatID:    session.ChatID,
		Workspace: workspace,
		Extra:     extra,
	}
	return p.hookService.Run(ctx, req, nil)
}

func (p *ContainerProvider) logWorkspaceToolHookError(eventName, botID, sessionID string, err error) {
	if p == nil || p.logger == nil || err == nil {
		return
	}
	p.logger.Warn("workspace tool hook failed",
		slog.String("event", eventName),
		slog.String("bot_id", botID),
		slog.String("session_id", sessionID),
		slog.Any("error", err),
	)
}

// resolveToolPath is the tool-facing path resolver: a relative path lands in
// the session's working directory (when one is bound) before the usual
// workspace-root prefix stripping. normalizePath itself must stay untouched —
// it strips the root the bridge server re-joins, and pointing it at the
// working directory would silently relocate files outside it.
func (w toolWorkspace) resolveToolPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || w.workdirPath == "" || w.isAbsToolPath(value) {
		return w.normalizePath(value)
	}
	if w.windows {
		joined := strings.TrimRight(w.workdirPath, `\`) + `\` + strings.ReplaceAll(value, "/", `\`)
		return w.normalizePath(joined)
	}
	return w.normalizePath(pathpkg.Join(w.workdirPath, value))
}

// isAbsToolPath reports whether value is absolute for the workspace's OS.
// Windows treats drive-qualified (C:...) and UNC (\\host\share) paths as
// absolute; joining either onto a working directory would produce garbage.
func (w toolWorkspace) isAbsToolPath(value string) bool {
	if w.windows {
		normalized := strings.ReplaceAll(value, "/", `\`)
		if strings.HasPrefix(normalized, `\\`) || strings.HasPrefix(normalized, `\`) {
			return true
		}
		return len(normalized) >= 2 && normalized[1] == ':'
	}
	return strings.HasPrefix(value, "/")
}

func (w toolWorkspace) normalizePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	prefix := strings.TrimSpace(w.defaultWorkDir)
	if prefix == "" {
		prefix = defaultContainerExecWorkDir
	}
	if w.windows {
		valueForCompare := strings.ReplaceAll(value, `\`, "/")
		prefixForCompare := strings.TrimRight(strings.ReplaceAll(prefix, `\`, "/"), "/")
		if strings.EqualFold(valueForCompare, prefixForCompare) {
			return "."
		}
		if len(valueForCompare) > len(prefixForCompare) &&
			strings.EqualFold(valueForCompare[:len(prefixForCompare)], prefixForCompare) &&
			valueForCompare[len(prefixForCompare)] == '/' {
			return strings.TrimLeft(valueForCompare[len(prefixForCompare)+1:], "/")
		}
		return value
	}
	prefix = strings.TrimRight(prefix, "/")
	if value == prefix {
		return "."
	}
	if strings.HasPrefix(value, prefix+"/") {
		return strings.TrimLeft(strings.TrimPrefix(value, prefix+"/"), "/")
	}
	return value
}

func (p *ContainerProvider) getClient(ctx context.Context, botID string) (*bridge.Client, error) {
	botID = strings.TrimSpace(botID)
	if botID == "" {
		return nil, errors.New("bot_id is required")
	}
	client, err := p.clients.MCPClient(ctx, botID)
	if err != nil {
		return nil, fmt.Errorf("workspace is not reachable: %w", err)
	}
	return client, nil
}

func (p *ContainerProvider) execRead(ctx context.Context, session SessionContext, args readArgs) (any, error) {
	opCtx, opCancel := context.WithTimeout(ctx, containerOpTimeout)
	defer opCancel()

	target, err := p.resolveToolTarget(opCtx, session, args.TargetID)
	if err != nil {
		return nil, err
	}
	client := target.client
	filePath := target.workspace.resolveToolPath(strings.TrimSpace(args.Path))
	if filePath == "" {
		return nil, errors.New("path is required")
	}

	lineOffset := 1
	if args.LineOffset != nil {
		if *args.LineOffset < 1 {
			return nil, errors.New("line_offset must be >= 1")
		}
		lineOffset = *args.LineOffset
	}
	nLines := 0 // 0 = read entire file
	if args.NLines != nil && *args.NLines > 0 {
		nLines = *args.NLines
	}

	// Pre-check file size to avoid loading excessively large files into
	// memory. The gRPC transport is capped at 16 MB, so anything larger
	// would fail anyway; reject early with a clear message.
	const maxReadBytes = 16 * 1024 * 1024 // 16 MB
	if stat, err := client.Stat(opCtx, filePath); err == nil && stat != nil {
		if stat.GetSize() > maxReadBytes {
			return nil, fmt.Errorf("file is too large (%d bytes, limit %d bytes). The read tool cannot read files above this limit", stat.GetSize(), maxReadBytes)
		}
	}

	// Stream-read the full file content.
	reader, err := client.ReadRaw(opCtx, filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()

	// Probe for binary content.
	probe := make([]byte, 8*1024)
	probeN, probeErr := reader.Read(probe)
	if probeErr != nil && probeErr != io.EOF {
		return nil, fmt.Errorf("read probe: %w", probeErr)
	}
	// PDF before the generic binary probe: a PDF may carry no NUL byte in its
	// first 8KB and would otherwise be read out as garbled text.
	if bytes.HasPrefix(probe[:probeN], []byte("%PDF-")) || strings.HasSuffix(strings.ToLower(filePath), ".pdf") {
		if !session.SupportsFileInput {
			return nil, errors.New("file is a PDF document and the current model has no native PDF input (file-input capability). Ask the user to switch to a PDF-capable model; do not attempt to install PDF readers")
		}
		return ReadImageFromContainer(opCtx, client, filePath, defaultReadMediaMaxBytes), nil
	}
	// Video before the generic binary probe: the read tool does not decode
	// video, it returns a reference (mime, size, content hash) the model can
	// hand to a specialist bot. Delegating is the model's decision, not ours.
	if videoMime, isVideo, videoErr := readVideoContentType(probe[:probeN], filePath); isVideo || videoErr != nil {
		if videoErr != nil {
			return nil, videoErr
		}
		return readVideoReference(opCtx, client, strings.TrimSpace(args.Path), filePath, videoMime)
	}
	// Office containers before the generic binary probe: xlsx/docx are zip
	// binaries the image lane rejects; parse them into plain text instead.
	if officeKind, ok := readOfficeContentType(probe[:probeN], filePath); ok {
		data, readErr := readAllAfterProbe(reader, probe[:probeN])
		if readErr != nil {
			return nil, readErr
		}
		return readOfficeResult(filePath, officeKind, data)
	}
	// Audio before the generic binary probe: an mp3 may carry no NUL byte in
	// its first 8KB, and audio needs its own content-scaled budget rather than
	// the image lane. The probe stream stays for the deferred close; the audio
	// branch re-opens the file under its own context.
	if audioMime, ok := readAudioContentType(probe[:probeN], filePath); ok {
		return p.execReadAudio(ctx, client, filePath, audioMime, session)
	}
	if bytes.IndexByte(probe[:probeN], 0) >= 0 {
		if !session.SupportsImageInput {
			return nil, errors.New("file appears to be binary. Read tool only supports text files (image reading not available for this model)")
		}
		return ReadImageFromContainer(opCtx, client, filePath, defaultReadMediaMaxBytes), nil
	}

	// Read remaining content after probe.
	var buf strings.Builder
	buf.Write(probe[:probeN])
	if probeErr != io.EOF {
		remaining, readErr := io.ReadAll(reader)
		if readErr != nil {
			return nil, fmt.Errorf("read file: %w", readErr)
		}
		buf.Write(remaining)
	}

	fullContent := buf.String()
	lines := strings.Split(fullContent, "\n")
	totalLines := len(lines)

	// Apply line_offset and n_lines.
	start := lineOffset - 1 // convert to 0-based
	if start > totalLines {
		start = totalLines
	}
	end := totalLines
	if nLines > 0 && start+nLines < end {
		end = start + nLines
	}

	selectedLines := lines[start:end]
	content := strings.Join(selectedLines, "\n")
	if !strings.HasSuffix(content, "\n") && end < totalLines {
		content += "\n"
	}

	content = addLineNumbers(content, lineOffset)
	return map[string]any{"content": content, "total_lines": totalLines}, nil
}

func (p *ContainerProvider) execWrite(ctx context.Context, session SessionContext, args writeArgs) (any, error) {
	opCtx, opCancel := context.WithTimeout(ctx, containerOpTimeout)
	defer opCancel()

	target, err := p.resolveToolTarget(opCtx, session, args.TargetID)
	if err != nil {
		return nil, err
	}
	client := target.client
	filePath := target.workspace.resolveToolPath(strings.TrimSpace(args.Path))
	content := strings.TrimSpace(args.Content)
	if filePath == "" {
		return nil, errors.New("path is required")
	}

	data := []byte(content)
	// Capture the current content (if the file exists) before overwriting, so
	// the chat UI can render the write as a diff: a brand-new file diffs from
	// empty (every line an addition), an overwrite diffs against the previous
	// content. Best effort: the write must not fail because the diff input
	// could not be read. hasBefore stays false when the old content is
	// unknown (oversized, unreadable, stat failure) — rendering an all-add
	// diff for what was really an overwrite would mislead, so no diff is
	// attached in that case. The pre-read runs on its own short context so a
	// slow workspace cannot burn the write's opCtx; files above the diff size
	// limit are never read, and neither is an oversized write — either way
	// editContextDiff would produce no diff, so the read would be pure waste.
	before := ""
	hasBefore := false
	if len(data) <= largeFileThreshold {
		readCtx, readCancel := context.WithTimeout(ctx, diffPreReadTimeout)
		before, hasBefore = readFileForDiff(readCtx, client, filePath)
		readCancel()
	}
	if res, err := p.runWorkspaceToolHook(ctx, session, target.hookWorkspaceInfo(p.execWorkDir), hooks.EventBeforeFileWrite, map[string]any{
		"path":  filePath,
		"bytes": len(data),
	}); err != nil {
		if res.Decision == hooks.DecisionDeny {
			return nil, err
		}
		return nil, fmt.Errorf("before file write hook failed: %w", err)
	}
	if len(data) > largeFileThreshold {
		// Large content: use streaming WriteRaw to avoid loading everything
		// into a single gRPC message and to allow incremental transfer.
		if _, err := client.WriteRaw(opCtx, filePath, strings.NewReader(content)); err != nil {
			return nil, err
		}
	} else {
		// Small content: simple synchronous write.
		if err := client.WriteFile(opCtx, filePath, data); err != nil {
			return nil, err
		}
	}
	if _, err := p.runWorkspaceToolHook(context.WithoutCancel(ctx), session, target.hookWorkspaceInfo(p.execWorkDir), hooks.EventAfterFileWrite, map[string]any{
		"path":  filePath,
		"bytes": len(data),
	}); err != nil {
		p.logWorkspaceToolHookError(hooks.EventAfterFileWrite, session.BotID, session.SessionID, err)
	}
	result := map[string]any{"ok": true}
	// Same UI-only diff channel as edit (see execEdit): computed from the
	// content actually read and written, stripped before the model sees the
	// tool result, and best effort — a diff failure must not fail the write
	// that already succeeded. A no-op write (identical content) yields no
	// diff; large content is skipped by editContextDiff itself.
	if hasBefore {
		if diff, err := editContextDiff(filePath, before, content); err == nil && diff != "" {
			result[UIOutputMetadataKey] = map[string]any{"diff": diff}
		}
	}
	return result, nil
}

// readFileForDiff best-effort reads a file's current content so execWrite can
// diff against it. Returns hasBefore=false whenever the truth is unknown —
// oversized file, stat/read failure, or the file grew past the limit between
// Stat and the read — so callers never diff against a truncated or guessed
// "before". A confirmed-missing file is an exact empty before.
func readFileForDiff(ctx context.Context, client *bridge.Client, filePath string) (string, bool) {
	stat, err := client.Stat(ctx, filePath)
	if err != nil {
		// The bridge client maps gRPC NotFound to its ErrNotFound sentinel, so
		// check that, not status.Code.
		if errors.Is(err, bridge.ErrNotFound) {
			return "", true
		}
		return "", false
	}
	if stat == nil || stat.GetIsDir() || stat.GetSize() > largeFileThreshold {
		return "", false
	}
	reader, err := client.ReadRaw(ctx, filePath)
	if err != nil {
		return "", false
	}
	defer func() { _ = reader.Close() }()
	// Stat's size is a snapshot; cap the read so a file that grows in between
	// cannot pull an unbounded body into memory. Anything past the threshold
	// makes the diff moot anyway, so overflow is treated as unreadable rather
	// than truncated.
	raw, err := io.ReadAll(io.LimitReader(reader, largeFileThreshold+1))
	if err != nil || int64(len(raw)) > largeFileThreshold {
		return "", false
	}
	return string(raw), true
}

func (p *ContainerProvider) execList(ctx context.Context, session SessionContext, args listArgs) (any, error) {
	opCtx, opCancel := context.WithTimeout(ctx, containerOpTimeout)
	defer opCancel()

	target, err := p.resolveToolTarget(opCtx, session, args.TargetID)
	if err != nil {
		return nil, err
	}
	client := target.client
	dirPath := target.workspace.resolveToolPath(strings.TrimSpace(args.Path))
	if dirPath == "" {
		dirPath = "."
	}
	recursive := args.Recursive

	offset := int32(0)
	if args.Offset != nil {
		v := *args.Offset
		if v < 0 {
			return nil, errors.New("offset must be >= 0")
		}
		if v > math.MaxInt32 {
			return nil, errors.New("offset exceeds maximum")
		}
		offset = int32(v) //nolint:gosec // bounded above
	}

	limit := int32(listMaxEntries)
	if args.Limit != nil {
		v := *args.Limit
		if v < 1 {
			return nil, errors.New("limit must be >= 1")
		}
		if v > listMaxEntries {
			v = listMaxEntries
		}
		limit = int32(v) //nolint:gosec // bounded by listMaxEntries
	}

	var collapseThreshold int32
	if recursive {
		collapseThreshold = listCollapseThreshold
	}

	result, err := client.ListDir(opCtx, dirPath, recursive, offset, limit, collapseThreshold)
	if err != nil {
		return nil, err
	}

	entriesMaps := make([]map[string]any, 0, len(result.Entries))
	for _, e := range result.Entries {
		m := map[string]any{
			"path": e.GetPath(), "is_dir": e.GetIsDir(), "size": e.GetSize(),
			"mode": e.GetMode(), "mod_time": e.GetModTime(),
		}
		if s := e.GetSummary(); s != "" {
			m["summary"] = s
		}
		entriesMaps = append(entriesMaps, m)
	}

	return map[string]any{
		"path":        dirPath,
		"entries":     entriesMaps,
		"total_count": result.TotalCount,
		"truncated":   result.Truncated,
		"offset":      offset,
		"limit":       limit,
	}, nil
}

func (p *ContainerProvider) execEdit(ctx context.Context, session SessionContext, args editArgs) (any, error) {
	opCtx, opCancel := context.WithTimeout(ctx, containerOpTimeout)
	defer opCancel()

	target, err := p.resolveToolTarget(opCtx, session, args.TargetID)
	if err != nil {
		return nil, err
	}
	client := target.client
	filePath := target.workspace.resolveToolPath(strings.TrimSpace(args.Path))
	oldText := strings.TrimSpace(args.OldText)
	newText := strings.TrimSpace(args.NewText)
	if filePath == "" || oldText == "" {
		return nil, errors.New("path, old_text and new_text are required")
	}

	// Read file content via streaming RPC.
	reader, err := client.ReadRaw(opCtx, filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	raw, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	updated, err := applyEdit(string(raw), filePath, oldText, newText)
	if err != nil {
		return nil, err
	}

	updatedBytes := []byte(updated)
	if res, err := p.runWorkspaceToolHook(ctx, session, target.hookWorkspaceInfo(p.execWorkDir), hooks.EventBeforeFileWrite, map[string]any{
		"path":  filePath,
		"bytes": len(updatedBytes),
		"mode":  "edit",
	}); err != nil {
		if res.Decision == hooks.DecisionDeny {
			return nil, err
		}
		return nil, fmt.Errorf("before file write hook failed: %w", err)
	}
	if len(updatedBytes) > largeFileThreshold {
		// Large result: stream-write to avoid gRPC message size issues.
		if _, err := client.WriteRaw(opCtx, filePath, strings.NewReader(updated)); err != nil {
			return nil, err
		}
	} else {
		if err := client.WriteFile(opCtx, filePath, updatedBytes); err != nil {
			return nil, err
		}
	}
	if _, err := p.runWorkspaceToolHook(context.WithoutCancel(ctx), session, target.hookWorkspaceInfo(p.execWorkDir), hooks.EventAfterFileWrite, map[string]any{
		"path":  filePath,
		"bytes": len(updatedBytes),
		"mode":  "edit",
	}); err != nil {
		p.logWorkspaceToolHookError(hooks.EventAfterFileWrite, session.BotID, session.SessionID, err)
	}
	result := map[string]any{"ok": true}
	// The diff is computed from the content actually read and written, so the
	// chat UI can render the edit with its real surrounding context. Best
	// effort: a diff failure must not fail the edit that already succeeded.
	// It rides the reserved UI-only key — the runtime strips it before the
	// model ever sees the tool result.
	if diff, err := editContextDiff(filePath, string(raw), updated); err == nil && diff != "" {
		result[UIOutputMetadataKey] = map[string]any{"diff": diff}
	}
	return result, nil
}

func (p *ContainerProvider) execExec(ctx context.Context, session SessionContext, args execArgs) (any, error) {
	target, err := p.resolveToolTarget(ctx, session, args.TargetID)
	if err != nil {
		return nil, err
	}
	client := target.client
	command := strings.TrimSpace(args.Command)
	if command == "" {
		return nil, errors.New("command is required")
	}
	workDir := strings.TrimSpace(args.WorkDir)
	switch {
	case workDir != "":
		// Bridge paths are workspace-relative. Command-only adapters own
		// their path contract and resolve against their current target config.
		if target.execute == nil {
			workDir = target.workspace.resolveToolPath(workDir)
		}
	case target.workspace.workdirPath != "":
		workDir = target.workspace.workdirPath
	default:
		workDir = target.workspace.defaultWorkDir
	}
	description := strings.TrimSpace(args.Description)
	hookWorkspace := target.hookWorkspaceInfo(p.execWorkDir)
	backgroundOutputDir := target.backgroundOutputDir()

	// Parse timeout (default 30s, max 600s).
	timeout := background.DefaultExecTimeout
	if args.Timeout != nil {
		t := *args.Timeout
		if t < 1 {
			return nil, errors.New("timeout must be >= 1")
		}
		maxTimeout := int(background.MaxExecTimeout)
		if t > maxTimeout {
			t = maxTimeout
		}
		timeout = int32(t) //nolint:gosec // bounded above
	}

	if target.execute != nil {
		if args.RunInBackground || args.BackgroundMode != "" || args.MaxDurationSeconds != nil {
			return nil, computertarget.ErrUnsupported
		}
		if timeout > 300 {
			timeout = 300
		}
		return p.execWithWorkspaceHooks(ctx, session, hookWorkspace, command, workDir, timeout, false, func() (any, error) {
			result, err := target.execute(ctx, computertarget.Request{Scope: computertarget.Scope{UserID: session.UserID}, Command: command, WorkDir: workDir, Timeout: time.Duration(timeout) * time.Second})
			if err != nil && result.State == "not_started" {
				return nil, err
			}
			// Preserve uncertain receipts instead of converting them into a
			// generic retryable tool failure or fabricating an exit code.
			if err != nil {
				return map[string]any{"result": result, "error": "The command outcome is uncertain or rejected. Inspect the retained Windows workspace before retrying; stopping the wait does not terminate the command."}, nil
			}
			return result, nil
		})
	}

	// Block sleep N (N>=2) in foreground — nudge model toward run_in_background.
	runInBg := args.RunInBackground
	budget, err := execBudget(args.MaxDurationSeconds, args.BackgroundMode, runInBg)
	if err != nil {
		return nil, err
	}
	if !runInBg {
		if reason := detectBlockedSleep(command); reason != "" {
			return nil, fmt.Errorf("blocked: %s. Run blocking commands in the background with run_in_background: true, then use wait_until(task_id) and get_background_status(task_id). If you genuinely need a delay (rate limiting, deliberate pacing), keep it under 2 seconds", reason)
		}
	}

	// Background execution path.
	if runInBg && p.bgManager != nil {
		return p.execWithWorkspaceHooks(ctx, session, hookWorkspace, command, workDir, timeout, true, func() (any, error) {
			return p.execExecBackground(ctx, session, client, command, workDir, description, backgroundOutputDir, budget)
		})
	}

	// If we have a background manager, use streaming exec so we can flip
	// to background on timeout without killing the process.
	if p.bgManager != nil {
		return p.execWithWorkspaceHooks(ctx, session, hookWorkspace, command, workDir, timeout, false, func() (any, error) {
			return p.execExecWithFlip(ctx, session, client, command, workDir, description, backgroundOutputDir, timeout, budget)
		})
	}

	// Fallback: no background manager, plain synchronous exec.
	wrapped, err := p.execWithWorkspaceHooks(ctx, session, hookWorkspace, command, workDir, timeout, false, func() (any, error) {
		result, err := client.Exec(ctx, command, workDir, timeout)
		if err != nil {
			return nil, err
		}
		stdout := pruneToolOutputText(result.Stdout, "tool result (exec stdout)")
		stderr := pruneToolOutputText(result.Stderr, "tool result (exec stderr)")
		return map[string]any{"stdout": stdout, "stderr": stderr, "exit_code": result.ExitCode}, nil
	})
	if err != nil {
		return nil, err
	}
	return wrapped, nil
}

func (p *ContainerProvider) execWithWorkspaceHooks(ctx context.Context, session SessionContext, workspace hooks.WorkspaceInfo, command, workDir string, timeout int32, background bool, run func() (any, error)) (any, error) {
	if res, err := p.runWorkspaceToolHook(ctx, session, workspace, hooks.EventBeforeWorkspaceCommand, map[string]any{
		"command":           command,
		"work_dir":          workDir,
		"timeout_seconds":   timeout,
		"run_in_background": background,
	}); err != nil {
		if res.Decision == hooks.DecisionDeny {
			return nil, err
		}
		return nil, fmt.Errorf("before workspace command hook failed: %w", err)
	}
	result, runErr := run()
	extra := map[string]any{
		"command":           command,
		"work_dir":          workDir,
		"timeout_seconds":   timeout,
		"run_in_background": background,
	}
	if runErr != nil {
		extra["error"] = runErr.Error()
	}
	if _, err := p.runWorkspaceToolHook(context.WithoutCancel(ctx), session, workspace, hooks.EventAfterWorkspaceCommand, extra); err != nil {
		p.logWorkspaceToolHookError(hooks.EventAfterWorkspaceCommand, session.BotID, session.SessionID, err)
	}
	return result, runErr
}

const backgroundReplayBytes = 4096

type backgroundExecStreamReader struct {
	resultCh        chan background.AdoptResult
	logger          *slog.Logger
	command         string
	deadline        time.Time
	cancel          context.CancelFunc
	lastLiveness    time.Time
	livenessHandler func(time.Time)

	mu             sync.Mutex
	stdout         strings.Builder
	stderr         strings.Builder
	chunkHandler   func(stream, chunk string)
	outputRecorded bool
}

func startBackgroundExecStreamReader(log *slog.Logger, stream *bridge.ExecStream, cancel context.CancelFunc, command string) *backgroundExecStreamReader {
	reader := &backgroundExecStreamReader{
		resultCh: make(chan background.AdoptResult, 1),
		logger:   log,
		command:  command,
	}
	go reader.run(stream, cancel)
	return reader
}

func (r *backgroundExecStreamReader) run(stream *bridge.ExecStream, cancel context.CancelFunc) {
	defer cancel()
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				r.mu.Lock()
				last := r.lastLiveness
				r.mu.Unlock()
				if !last.IsZero() && time.Since(last) > 90*time.Second {
					cancel()
					return
				}
			}
		}
	}()
	var exitCode int32
	var exitReceived bool
	for {
		msg, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			r.logger.Warn("background exec stream recv error",
				slog.String("command", truncateStr(r.command, 80)),
				slog.Any("error", recvErr),
			)
			stdout, stderr, outputRecorded := r.snapshot()
			r.resultCh <- background.AdoptResult{
				Stdout:         stdout,
				Stderr:         stderr,
				ExitCode:       exitCode,
				ExitReceived:   exitReceived,
				Err:            recvErr,
				OutputRecorded: outputRecorded,
			}
			return
		}
		switch msg.GetStream() {
		case pb.ExecOutput_HEARTBEAT:
			r.mu.Lock()
			r.lastLiveness = time.Now()
			handler := r.livenessHandler
			at := r.lastLiveness
			r.mu.Unlock()
			if handler != nil {
				handler(at)
			}
		case pb.ExecOutput_STDOUT:
			r.appendChunk("stdout", string(msg.GetData()))
		case pb.ExecOutput_STDERR:
			r.appendChunk("stderr", string(msg.GetData()))
		case pb.ExecOutput_EXIT:
			exitCode = msg.GetExitCode()
			exitReceived = true
		}
	}

	stdout, stderr, outputRecorded := r.snapshot()
	r.resultCh <- background.AdoptResult{
		Stdout:         stdout,
		Stderr:         stderr,
		ExitCode:       exitCode,
		ExitReceived:   exitReceived,
		OutputRecorded: outputRecorded,
	}
}

func (r *backgroundExecStreamReader) snapshot() (stdout, stderr string, outputRecorded bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stdout.String(), r.stderr.String(), r.outputRecorded
}

func (r *backgroundExecStreamReader) appendChunk(stream, chunk string) {
	if chunk == "" {
		return
	}
	r.mu.Lock()
	switch stream {
	case "stderr":
		r.stderr.WriteString(chunk)
	default:
		r.stdout.WriteString(chunk)
	}
	handler := r.chunkHandler
	r.mu.Unlock()
	if handler != nil {
		handler(stream, chunk)
	}
}

func (r *backgroundExecStreamReader) SetChunkHandler(handler func(stream, chunk string)) {
	r.mu.Lock()
	r.chunkHandler = handler
	r.outputRecorded = handler != nil
	stdout := tailText(r.stdout.String(), backgroundReplayBytes)
	stderr := tailText(r.stderr.String(), backgroundReplayBytes)
	r.mu.Unlock()

	if handler == nil {
		return
	}
	if stdout != "" {
		handler("stdout", stdout)
	}
	if stderr != "" {
		handler("stderr", stderr)
	}
}

func (r *backgroundExecStreamReader) Result() <-chan background.AdoptResult {
	return r.resultCh
}

func tailText(value string, maxBytes int) string {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value
	}
	return value[len(value)-maxBytes:]
}

// execExecWithFlip runs a command via ExecStream with a client-side soft timeout.
// If the command finishes within the timeout, it returns the result normally.
// If the soft timeout fires first, the running stream is handed off to the
// background manager — the process keeps running in the container, and the
// agent gets an immediate "auto_backgrounded" response.
func (p *ContainerProvider) execExecWithFlip(
	ctx context.Context, session SessionContext, client *bridge.Client,
	command, workDir, description, outputDir string, softTimeout int32, budgets ...time.Duration,
) (any, error) {
	// Start streaming exec with a large container-side timeout so the process
	// keeps running even after we stop reading in the foreground.
	// Use a fully independent context (not derived from the agent request ctx)
	// so the gRPC stream is never cancelled when the foreground session ends.
	streamCtx, streamCancel := execBudgetContext(ctx, budgets)
	stream, err := client.ExecStreamWithOptions(streamCtx, command, workDir, -1, bridge.ExecOptions{ReportLiveness: true})
	if err != nil {
		streamCancel()
		return nil, err
	}
	reader := startBackgroundExecStreamReader(p.logger, stream, streamCancel, command)
	reader.deadline, _ = streamCtx.Deadline()
	reader.cancel = streamCancel

	// Leave time to hand the stream to the background manager before its hard
	// deadline. This also applies when an ancestor budget ends sooner.
	foregroundWait := foregroundWaitBeforeBudget(streamCtx, softTimeout)
	timer := time.NewTimer(foregroundWait)
	defer timer.Stop()

	select {
	case r := <-reader.Result():
		// Command finished within the soft timeout — return normally.
		if r.Err != nil {
			return nil, r.Err
		}
		stdout := pruneToolOutputText(r.Stdout, "tool result (exec stdout)")
		stderr := pruneToolOutputText(r.Stderr, "tool result (exec stderr)")
		return map[string]any{"stdout": stdout, "stderr": stderr, "exit_code": r.ExitCode}, nil

	case <-timer.C:
		// Foreground waiting ended, possibly early to preserve the hard budget.
		// The container process is still alive; we hand off the stream reader
		// goroutine to the background manager.
		return p.flipToBackground(ctx, session, client, reader, command, workDir, description, outputDir, foregroundWait)

	case <-ctx.Done():
		streamCancel()
		return nil, ctx.Err()
	}
}

// flipToBackground registers the already-running stream as a background task.
// The goroutine reading from the stream continues; its result feeds the task.
func (p *ContainerProvider) flipToBackground(
	ctx context.Context,
	session SessionContext, client *bridge.Client,
	reader *backgroundExecStreamReader,
	command, workDir, description, outputDir string, foregroundWait time.Duration,
) (any, error) {
	writeFn := func(ctx context.Context, path string, data []byte) error {
		return client.WriteFile(ctx, path, data)
	}

	taskID, outputFile := p.bgManager.SpawnAdopt(
		ctx,
		session.BotID, session.SessionID, command, workDir, description, outputDir,
		reader.Result(), writeFn, background.AdoptOptions{Deadline: reader.deadline, Cancel: reader.cancel},
	)
	// SetChunkHandler replays output collected during the foreground phase
	// into the task buffer, so the tail below already contains it.
	reader.SetLivenessHandler(func(at time.Time) { p.bgManager.RecordLiveness(taskID, at) })
	reader.SetChunkHandler(func(stream, chunk string) {
		p.bgManager.RecordOutput(taskID, stream, chunk)
	})

	p.logger.InfoContext(ctx, "foreground exec flipped to background",
		slog.String("task_id", taskID),
		slog.String("command", truncateStr(command, 120)),
		slog.Duration("foreground_wait", foregroundWait),
	)

	result := map[string]any{
		"status":      "auto_backgrounded",
		"task_id":     taskID,
		"output_file": outputFile,
		"message": fmt.Sprintf(
			"Command moved to the background after %s of foreground waiting with task ID: %s. "+
				"The process is still running — no work was lost; output collected so far is in output_tail. "+
				"Use wait_until(task_id) to keep observing: it returns when the task finishes, stalls, or goes quiet (reason 'idle') — for servers, a ready message in output_tail means it is up. "+
				"The full log is written to %s after the task ends. "+
				"For long-running commands, use run_in_background: true from the start to avoid this delay.",
			foregroundWait.Round(time.Second), taskID, outputFile,
		),
	}
	if task := p.bgManager.Get(taskID); task != nil {
		if tail := task.OutputTail(); tail != "" {
			result["output_tail"] = tail
		}
	}
	return result, nil
}

// detectBlockedSleep checks if the command starts with `sleep N` where N >= 2.
// Returns a human-readable reason string, or "" if the command is allowed.
func detectBlockedSleep(command string) string {
	cmd := strings.TrimSpace(command)
	m := blockedSleepPattern.FindStringSubmatch(cmd)
	if m == nil {
		return ""
	}
	seconds, err := strconv.ParseFloat(m[1], 64)
	if err != nil || seconds < 2 {
		return ""
	}
	return fmt.Sprintf("sleep %.0f is not allowed in foreground execution", seconds)
}

// execExecBackground spawns the command as a background task and returns immediately.
func (p *ContainerProvider) execExecBackground(
	ctx context.Context, session SessionContext, client *bridge.Client,
	command, workDir, description, outputDir string, budgets ...time.Duration,
) (any, error) {
	streamCtx, streamCancel := execBudgetContext(ctx, budgets)
	stream, err := client.ExecStreamWithOptions(streamCtx, command, workDir, -1, bridge.ExecOptions{ReportLiveness: true})
	if err != nil {
		streamCancel()
		return nil, err
	}
	reader := startBackgroundExecStreamReader(p.logger, stream, streamCancel, command)
	reader.deadline, _ = streamCtx.Deadline()
	reader.cancel = streamCancel

	writeFn := func(ctx context.Context, path string, data []byte) error {
		return client.WriteFile(ctx, path, data)
	}
	taskID, outputFile := p.bgManager.SpawnAdopt(
		ctx,
		session.BotID, session.SessionID, command, workDir, description, outputDir,
		reader.Result(), writeFn, background.AdoptOptions{Deadline: reader.deadline, Cancel: reader.cancel},
	)
	reader.SetLivenessHandler(func(at time.Time) { p.bgManager.RecordLiveness(taskID, at) })
	reader.SetChunkHandler(func(stream, chunk string) {
		p.bgManager.RecordOutput(taskID, stream, chunk)
	})

	return map[string]any{
		"status":      "background_started",
		"task_id":     taskID,
		"output_file": outputFile,
		"message": fmt.Sprintf(
			"Command started in background with task ID: %s. "+
				"Use wait_until(task_id) to observe it: it returns when the task finishes, stalls on a prompt, or output goes quiet (reason 'idle'), always with the latest output_tail — for servers, a ready message there means it is up. "+
				"get_background_status(task_id) also shows output_tail while running. The full log is written to %s after the task ends.",
			taskID, outputFile,
		),
	}, nil
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func addLineNumbers(content string, startLine int) string {
	if content == "" {
		return content
	}
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	var out strings.Builder
	out.Grow(len(content) + len(lines)*8)
	for i, line := range lines {
		fmt.Fprintf(&out, "%6d\t%s\n", startLine+i, line)
	}
	return out.String()
}

func (r *backgroundExecStreamReader) SetLivenessHandler(handler func(time.Time)) {
	r.mu.Lock()
	r.livenessHandler = handler
	at := r.lastLiveness
	r.mu.Unlock()
	if handler != nil && !at.IsZero() {
		handler(at)
	}
}

func execBudget(maxDurationSeconds *int, backgroundMode string, backgroundRun bool) (time.Duration, error) {
	mode := strings.TrimSpace(backgroundMode)
	if mode != "" && mode != "task" && mode != "service" {
		return 0, errors.New("background_mode must be task or service")
	}
	if mode == "service" {
		if !backgroundRun || maxDurationSeconds != nil {
			return 0, errors.New("service mode requires run_in_background=true and no max_duration_seconds")
		}
		return 0, nil
	}
	return finiteTaskBudget(maxDurationSeconds, minCommandExecBudgetSeconds)
}

// Video monitoring has no foreground-to-background transition and retains its
// existing one-second minimum.
func videoMonitorBudget(maxDurationSeconds *int) (time.Duration, error) {
	return finiteTaskBudget(maxDurationSeconds, 1)
}

func finiteTaskBudget(maxDurationSeconds *int, minimum int) (time.Duration, error) {
	seconds := int(background.BackgroundExecTimeout)
	if maxDurationSeconds != nil {
		seconds = *maxDurationSeconds
	}
	if seconds < minimum || seconds > int(background.MaxBackgroundExecTimeout) {
		return 0, fmt.Errorf("max_duration_seconds must be between %d and 86400", minimum)
	}
	return time.Duration(seconds) * time.Second, nil
}

func foregroundWaitBeforeBudget(ctx context.Context, softTimeout int32) time.Duration {
	wait := time.Duration(softTimeout) * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline) - time.Second; remaining < wait {
			wait = remaining
		}
	}
	if wait < 0 {
		return 0
	}
	return wait
}

func execBudgetContext(parent context.Context, budgets []time.Duration) (context.Context, context.CancelFunc) {
	budget := time.Duration(background.BackgroundExecTimeout) * time.Second
	if len(budgets) > 0 {
		budget = budgets[0]
	}
	deadline, hasDeadline := parent.Deadline()
	if budget > 0 {
		candidate := time.Now().Add(budget)
		if !hasDeadline || candidate.Before(deadline) {
			deadline = candidate
			hasDeadline = true
		}
	}
	if hasDeadline {
		return context.WithDeadline(context.WithoutCancel(parent), deadline)
	}
	return context.WithCancel(context.WithoutCancel(parent))
}

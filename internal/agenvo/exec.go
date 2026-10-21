package agenvo

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/google/uuid"
)

// Request supports bounded, foreground PowerShell commands only. It does not
// claim POSIX exec, a PTY, binary stdin, or persistent/background processes.
type Request struct {
	Scope      Scope
	Command    string
	WorkDir    string
	Timeout    time.Duration
	Stdin      []byte
	Env        []string
	PTY        bool
	Background bool
}

type Result struct {
	State            string `json:"state"`
	Output           string `json:"output,omitempty"`
	ExitCode         *int   `json:"exit_code,omitempty"`
	Truncated        bool   `json:"truncated"`
	WorkspaceID      string `json:"workspace_id,omitempty"`
	PaneID           string `json:"pane_id,omitempty"`
	RequestID        string `json:"request_id,omitempty"`
	CleanupConfirmed bool   `json:"cleanup_confirmed"`
}

type Executor struct {
	client       *Client
	pollInterval time.Duration
}

func NewExecutor(client *Client) *Executor {
	return &Executor{client: client, pollInterval: 500 * time.Millisecond}
}

type nativeWorkspace struct {
	Workspace struct {
		ID    string `json:"workspace_id"`
		Label string `json:"label"`
	} `json:"workspace"`
	RootPane struct {
		ID string `json:"pane_id"`
	} `json:"root_pane"`
}

// Execute allocates a fresh, uniquely labeled native workspace per operation.
// It never uses another Bot's pane or sends a command again after uncertainty.
// A receipt from pane.run is submission only; completion requires the bounded
// wrapper's nonce-bound result, never an idle status or a terminal prompt.
func (e *Executor) Execute(ctx context.Context, req Request) (result Result, err error) {
	result.State = "not_started"
	if len(req.Stdin) != 0 || len(req.Env) != 0 || req.PTY || req.Background {
		return result, ErrUnsupported
	}
	if strings.TrimSpace(req.Command) == "" || len(req.Command) > 4096 {
		return result, ErrUnsupported
	}
	if req.Timeout == 0 {
		req.Timeout = 30 * time.Second
	}
	if req.Timeout < time.Millisecond || req.Timeout > 5*time.Minute {
		return result, ErrUnsupported
	}
	if e == nil || e.client == nil {
		return result, ErrUnavailable
	}
	target, err := e.client.resolveTarget(ctx, req.Scope)
	if err != nil {
		return result, err
	}
	workDir := req.WorkDir
	if workDir == "" {
		workDir = target.WorkDir
	}
	if workDir == "" || len(workDir) > 4096 || strings.ContainsRune(workDir, 0) {
		return result, ErrUnsupported
	}
	if err := e.client.Probe(ctx, req.Scope); err != nil {
		return result, err
	}
	nonce := strings.ReplaceAll(uuid.NewString(), "-", "")
	label := "memoh-" + nonce
	ctx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()
	created, err := e.client.native(ctx, req.Scope, target, "workspace.create", map[string]any{"session": target.Session, "cwd": workDir, "label": label})
	result.RequestID = created.RequestID
	if err != nil {
		if !errors.Is(err, ErrRejected) {
			result.State = "unknown"
		}
		return result, err
	}
	var workspace nativeWorkspace
	if err := resultBody(created, &workspace); err != nil {
		result.State = "unknown"
		return result, err
	}
	result.WorkspaceID, result.PaneID = workspace.Workspace.ID, workspace.RootPane.ID
	if result.WorkspaceID == "" || result.PaneID == "" || workspace.Workspace.Label != label {
		result.State = "unknown"
		return result, ErrUnknown
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cleanupCancel()
		result.CleanupConfirmed = e.cleanup(cleanupCtx, req.Scope, target, result.WorkspaceID, label)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			if result.CleanupConfirmed {
				result.State = "canceled"
			} else {
				result.State = "unknown"
			}
		}
	}()
	command := powershellCommand(req.Command, nonce)
	submitted, err := e.client.native(ctx, req.Scope, target, "pane.run", map[string]any{"session": target.Session, "paneId": result.PaneID, "command": command})
	result.RequestID = submitted.RequestID
	if err != nil {
		result.State = "unknown"
		if errors.Is(err, ErrRejected) {
			result.State = "rejected"
		}
		return result, err
	}
	result.State = "submitted"
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		read, err := e.client.native(ctx, req.Scope, target, "pane.read", map[string]any{"session": target.Session, "paneId": result.PaneID, "lines": 500, "source": "recent-unwrapped"})
		if err != nil {
			result.State = "unknown"
			return result, err
		}
		var snapshot struct {
			Output string `json:"output"`
		}
		if err := resultBody(read, &snapshot); err != nil {
			result.State = "unknown"
			return result, err
		}
		completion, found, err := parseCompletion(snapshot.Output, nonce)
		if err != nil {
			result.State = "unknown"
			return result, err
		}
		if found {
			result.State, result.Output, result.ExitCode, result.Truncated = "completed", completion.Output, completion.ExitCode, completion.Truncated
			return result, nil
		}
		timer := time.NewTimer(e.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return result, ctx.Err()
		case <-timer.C:
		}
	}
}

func (e *Executor) cleanup(ctx context.Context, scope Scope, target Target, workspaceID, label string) bool {
	// Native IDs may be reused after a Herdr restart. Check our unique label
	// before closing, and refuse a mismatched or unknown native shape.
	got, err := e.client.native(ctx, scope, target, "workspace.get", map[string]any{"session": target.Session, "workspaceId": workspaceID})
	if err != nil {
		return false
	}
	var current nativeWorkspace
	if resultBody(got, &current) != nil || current.Workspace.ID != workspaceID || current.Workspace.Label != label {
		return false
	}
	if _, err := e.client.native(ctx, scope, target, "workspace.close", map[string]any{"session": target.Session, "workspaceId": workspaceID}); err != nil {
		return false
	}
	listed, err := e.client.native(ctx, scope, target, "workspace.list", map[string]any{"session": target.Session})
	if err != nil {
		return false
	}
	var remaining struct {
		Workspaces []struct {
			ID string `json:"workspace_id"`
		} `json:"workspaces"`
	}
	if resultBody(listed, &remaining) != nil || remaining.Workspaces == nil {
		return false
	}
	for _, workspace := range remaining.Workspaces {
		if workspace.ID == workspaceID {
			return false
		}
	}
	return true
}

type completion struct {
	Output    string `json:"output"`
	ExitCode  *int   `json:"exit_code"`
	Truncated bool   `json:"truncated"`
}

func parseCompletion(snapshot, nonce string) (completion, bool, error) {
	start, end := "MEMOH_"+nonce+":", ":END_"+nonce
	var matched *completion
	for _, line := range strings.Split(strings.ReplaceAll(snapshot, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, start) || !strings.HasSuffix(line, end) {
			continue
		}
		encoded := strings.TrimSuffix(strings.TrimPrefix(line, start), end)
		if len(encoded) > 32768 || matched != nil {
			return completion{}, false, ErrUnknown
		}
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return completion{}, false, ErrUnknown
		}
		var parsed completion
		if json.Unmarshal(data, &parsed) != nil || parsed.ExitCode == nil || len(parsed.Output) > 16384 {
			return completion{}, false, ErrUnknown
		}
		matched = &parsed
	}
	if matched == nil {
		return completion{}, false, nil
	}
	return *matched, true, nil
}

func powershellCommand(command, nonce string) string {
	// Commands are UTF-8 data inside an encoded PowerShell script. The nonce is
	// not visible in the echoed shell command, which cannot fake completion.
	body := base64.StdEncoding.EncodeToString([]byte(command))
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$global:LASTEXITCODE = 0
$memohOutput = New-Object System.Text.StringBuilder
$memohTruncated = $false
$memohExit = 0
function Add-MemohOutput([string]$line) {
  $remaining = 4096 - $memohOutput.Length
  if ($remaining -gt 0) {
    $text = $line + [Environment]::NewLine
    if ($text.Length -gt $remaining) { $text = $text.Substring(0, $remaining); $script:memohTruncated = $true }
    [void]$memohOutput.Append($text)
  } else { $script:memohTruncated = $true }
}
try {
  $body = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('%s'))
  & ([scriptblock]::Create($body)) 2>&1 | ForEach-Object { Add-MemohOutput ([string]$_) }
  $memohExit = $global:LASTEXITCODE
} catch { $memohExit = 1; Add-MemohOutput ([string]$_) }
$result = @{output=$memohOutput.ToString();exit_code=[int]$memohExit;truncated=$memohTruncated} | ConvertTo-Json -Compress
$encoded = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($result))
Write-Output ('MEMOH_%s:' + $encoded + ':END_%s')`, body, nonce, nonce)
	units := utf16.Encode([]rune(script))
	bytes := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.LittleEndian.PutUint16(bytes[i*2:], unit)
	}
	return "powershell.exe -NoProfile -NonInteractive -EncodedCommand " + base64.StdEncoding.EncodeToString(bytes)
}

package agenvo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Request supports bounded, foreground Bash commands only. It does not
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
	State           string `json:"state"`
	Output          string `json:"output,omitempty"`
	ExitCode        *int   `json:"exit_code,omitempty"`
	Truncated       bool   `json:"truncated"`
	WorkspaceID     string `json:"workspace_id,omitempty"`
	PaneID          string `json:"pane_id,omitempty"`
	RequestID       string `json:"request_id,omitempty"`
	CleanupRequired bool   `json:"cleanup_required"`
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
	if !windowsBash.MatchString(target.BashPath) || len(target.BashPath) > 4096 || strings.ContainsRune(target.BashPath, 0) {
		return result, ErrUnsupported
	}
	workDir, err := resolveWorkDir(target.WorkDir, req.WorkDir)
	if err != nil {
		return result, err
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
	// Agenvo does not offer an atomic expected-generation close. Retain the
	// workspace for inspection; never close a potentially reused native ID.
	result.CleanupRequired = true
	defer func() {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			result.State = "unknown"
		}
	}()
	command := bashCommand(req.Command, nonce, target.BashPath)
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
		// Unlike workspace CLI replies, pane.read is plain terminal output
		// directly under the Agenvo native result, without another result key.
		if err := json.Unmarshal(read.Result, &snapshot); err != nil {
			result.State = "unknown"
			return result, ErrUnknown
		}
		completion, found, err := parseCompletion(snapshot.Output, nonce)
		if err != nil {
			result.State = "unknown"
			return result, err
		}
		if found {
			if completion.Failure != "" {
				result.State = "rejected"
				return result, ErrUnsupported
			}
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

type completion struct {
	Output    string
	ExitCode  *int
	Truncated bool
	Failure   string
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
		var wire struct {
			OutputBase64 *string `json:"output_base64"`
			ExitCode     *int    `json:"exit_code"`
			Truncated    bool    `json:"truncated"`
			Failure      string  `json:"failure"`
		}
		if json.Unmarshal(data, &wire) != nil {
			return completion{}, false, ErrUnknown
		}
		parsed := completion{ExitCode: wire.ExitCode, Truncated: wire.Truncated, Failure: wire.Failure}
		if wire.Failure != "" {
			if wire.ExitCode != nil || wire.OutputBase64 != nil || (wire.Failure != "bash_unavailable" && wire.Failure != "unsupported_bash_host" && wire.Failure != "bash_tools_unavailable") {
				return completion{}, false, ErrUnknown
			}
		} else {
			if wire.ExitCode == nil || *wire.ExitCode < 0 || *wire.ExitCode > 255 || wire.OutputBase64 == nil {
				return completion{}, false, ErrUnknown
			}
			output, err := base64.StdEncoding.DecodeString(*wire.OutputBase64)
			if err != nil || len(output) > 4096 {
				return completion{}, false, ErrUnknown
			}
			// A byte limit may split only the final UTF-8 rune. Reject binary
			// or malformed text; trim an incomplete suffix only on truncation.
			if wire.Truncated {
				for len(output) > 0 && !utf8.Valid(output) {
					start := len(output) - 1
					for start > 0 && !utf8.RuneStart(output[start]) {
						start--
					}
					if utf8.FullRune(output[start:]) {
						break
					}
					output = output[:start]
				}
			}
			if !utf8.Valid(output) || strings.ContainsRune(string(output), 0) {
				return completion{}, false, ErrUnknown
			}
			parsed.Output = string(output)
		}
		matched = &parsed
	}
	if matched == nil {
		return completion{}, false, nil
	}
	return *matched, true, nil
}

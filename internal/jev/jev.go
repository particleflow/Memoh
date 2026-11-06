// Package jev is a client for TypeSafe System One selection models (for the
// production NAS deployment: the local Magpie gateway /v1/systemone route).
// System One models are non-generative: they answer typed questions (choice,
// score, noul) over a state text with probabilities, never free-form prose.
//
// The wire format was verified against the live gateway (jev-1.14.0):
//
//	POST {endpoint}  Authorization: Bearer {api_key}
//	{"model":"jev-latest","state":"...","questions":{
//	  "qid":{"type":"choice","instructions":"...","criteria":{"opt":"desc"}},
//	  "qid2":{"type":"noul","instructions":"...","criteria":{"true":"..","false":".."}},
//	  "qid3":{"type":"score","instructions":"...","criteria":["lv0","lv1","lv2"]}}}
//	→ {"model":"jev-1.14.0","answers":{"qid":{"type":"choice","choice":"opt",
//	  "confidence":0.44,"probabilities":{"opt":0.72}}},
//	  "usage":{"input_tokens":304,"output_tokens":31}}
package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Fallback reason kinds shared by accounting and logs (design v3 §6.5).
const (
	ReasonAuthError          = "auth_error"
	ReasonBadRequest         = "bad_request"
	ReasonGatewayUnreachable = "gateway_unreachable"
	ReasonTimeout            = "jev_timeout"
	ReasonInvalidResponse    = "invalid_response"
)

// Error is a classified Jev call failure. Kind is one of the Reason* constants.
type Error struct {
	Kind string
	// Body holds up to the first 200 bytes of a non-JSON response body for
	// gateway/proxy error pages, for logs only.
	Body string
	Err  error
}

func (e *Error) Error() string {
	if e.Body != "" {
		return fmt.Sprintf("jev %s: %v: %s", e.Kind, e.Err, e.Body)
	}
	return fmt.Sprintf("jev %s: %v", e.Kind, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

// Question is one typed question. Type is "choice", "score" or "noul".
// Instructions carries the criteria text; Criteria is the option→description
// map (choice), the ordered level labels (score) or the true/false meanings
// (noul).
type Question struct {
	Type         string          `json:"type"`
	Instructions string          `json:"instructions"`
	Criteria     json.RawMessage `json:"criteria"`
}

// NewChoice builds a choice question from an option→description map.
func NewChoice(instructions string, options map[string]string) Question {
	raw, _ := json.Marshal(options)
	return Question{Type: "choice", Instructions: instructions, Criteria: raw}
}

// NewScore builds a score question from ordered level labels (index 0 first).
func NewScore(instructions string, levels []string) Question {
	raw, _ := json.Marshal(levels)
	return Question{Type: "score", Instructions: instructions, Criteria: raw}
}

// NewNoul builds a boolean (noul) question from true/false meanings.
func NewNoul(instructions string, truthy, falsy string) Question {
	raw, _ := json.Marshal(map[string]string{"true": truthy, "false": falsy})
	return Question{Type: "noul", Instructions: instructions, Criteria: raw}
}

type request struct {
	Model     string              `json:"model"`
	State     string              `json:"state"`
	Questions map[string]Question `json:"questions"`
}

// Answer is one typed answer exactly as the gateway returned it.
type Answer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice"`
	Score         *float64           `json:"score"`
	Noul          *float64           `json:"noul"`
	Confidence    *float64           `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
}

// Usage mirrors the gateway usage block when present.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type response struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   *Usage            `json:"usage"`
}

// Response is a successful Jev call result.
type Response struct {
	Model   string
	Latency time.Duration
	Answers map[string]Answer
	Usage   Usage
}

// Client posts single calls to a System One gateway. It never retries; the
// caller owns the per-call context deadline (design v3: the whole decision
// window is 10s and every phase entry is guarded before a call is made).
type Client struct {
	Endpoint string
	// APIKey is server configuration material: it is never logged, marshaled
	// or echoed back; the struct has no JSON tags on purpose.
	APIKey string //nolint:gosec // config credential field, not a hardcoded secret
	Model  string
	HTTP   *http.Client
}

// Ask performs one call. ctx must carry the caller-derived deadline; a
// context deadline or cancellation is classified as jev_timeout.
func (c *Client) Ask(ctx context.Context, state string, questions map[string]Question) (Response, error) {
	start := time.Now()
	if strings.TrimSpace(c.Endpoint) == "" {
		return Response{}, &Error{Kind: ReasonBadRequest, Err: errors.New("empty jev endpoint")}
	}
	body, err := json.Marshal(request{Model: c.Model, State: state, Questions: questions})
	if err != nil {
		return Response{}, &Error{Kind: ReasonBadRequest, Err: err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return Response{}, &Error{Kind: ReasonBadRequest, Err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	httpResp, err := client.Do(req) //nolint:gosec // endpoint is admin server config (NAS loopback gateway), not user input
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Response{}, &Error{Kind: ReasonTimeout, Err: ctxErr}
		}
		return Response{}, &Error{Kind: ReasonGatewayUnreachable, Err: err}
	}
	defer func() { _ = httpResp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(httpResp.Body, 1<<20))
	if httpResp.StatusCode == http.StatusUnauthorized || httpResp.StatusCode == http.StatusForbidden {
		return Response{}, &Error{Kind: ReasonAuthError, Err: fmt.Errorf("http %d", httpResp.StatusCode)}
	}
	if httpResp.StatusCode == http.StatusBadRequest {
		return Response{}, &Error{Kind: ReasonBadRequest, Err: fmt.Errorf("http 400: %s", snippet(raw))}
	}
	if err != nil {
		return Response{}, &Error{Kind: ReasonInvalidResponse, Err: err}
	}
	if httpResp.StatusCode != http.StatusOK {
		return Response{}, &Error{Kind: ReasonInvalidResponse, Err: fmt.Errorf("http %d: %s", httpResp.StatusCode, snippet(raw))}
	}
	var parsed response
	if err := json.Unmarshal(raw, &parsed); err != nil || len(parsed.Answers) == 0 {
		return Response{}, &Error{Kind: ReasonInvalidResponse, Err: fmt.Errorf("decode: %w", err), Body: snippet(raw)}
	}
	out := Response{Model: parsed.Model, Latency: time.Since(start), Answers: parsed.Answers}
	if parsed.Usage != nil {
		out.Usage = *parsed.Usage
	}
	return out, nil
}

// Choice returns the chosen option for a choice question id.
func (r Response) Choice(id string) (string, Answer, bool) {
	answer, ok := r.Answers[id]
	if !ok || answer.Type != "choice" || answer.Choice == "" {
		return "", Answer{}, false
	}
	return answer.Choice, answer, true
}

// Score returns the raw score value for a score question id. Level indexing
// is zero-based (a three-level score lies in [0,2]).
func (r Response) Score(id string) (float64, Answer, bool) {
	answer, ok := r.Answers[id]
	if !ok || answer.Type != "score" || answer.Score == nil {
		return 0, Answer{}, false
	}
	return *answer.Score, answer, true
}

// Noul returns the boolean probability for a noul question id.
func (r Response) Noul(id string) (float64, Answer, bool) {
	answer, ok := r.Answers[id]
	if !ok || answer.Type != "noul" || answer.Noul == nil {
		return 0, Answer{}, false
	}
	return *answer.Noul, answer, true
}

func snippet(raw []byte) string {
	if len(raw) > 200 {
		raw = raw[:200]
	}
	return strings.ToValidUTF8(string(raw), "")
}

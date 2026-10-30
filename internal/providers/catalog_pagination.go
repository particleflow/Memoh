package providers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/felinics/memoh/internal/models"
)

// ErrCatalogIncomplete never carries upstream cursors, credentials or bodies.
// Callers must retain their previous snapshot on this error, not apply a prefix.
var ErrCatalogIncomplete = errors.New("model catalog pagination is incomplete or inconsistent")

// catalogHTTPClient completes discovery before the existing SDK decodes it.
// This client is used only for model lists, never generation or credential refresh.
func catalogHTTPClient(typ models.ClientType, configured *http.Client) *http.Client {
	client := models.NewProviderHTTPClient(probeTimeout)
	if configured != nil && configured.Transport != nil {
		client.Transport = configured.Transport
	}
	client.Transport = &catalogPaginationTransport{base: client.Transport, typ: typ}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.Jar = nil
	return client
}

type catalogPaginationTransport struct {
	base http.RoundTripper
	typ  models.ClientType
}

func (t *catalogPaginationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet || !strings.HasSuffix(req.URL.Path, "/models") {
		return t.base.RoundTrip(req)
	}
	field := "data"
	if t.typ == models.ClientTypeGoogleGenerativeAI {
		field = "models"
	}
	var all []json.RawMessage
	seenIDs, seenCursors := map[string]bool{}, map[string]bool{}
	remaining := int64(remoteModelsResponseLimit)
	pageReq := req.Clone(req.Context())
	for {
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		resp, err := t.base.RoundTrip(pageReq)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			// The SDK/HTTP boundary retains the real upstream failure. A later
			// page's failure never returns the earlier pages as a successful list.
			return resp, nil
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, remaining+1))
		closeErr := resp.Body.Close()
		remaining -= int64(len(body))
		if readErr != nil || closeErr != nil || remaining < 0 {
			return nil, ErrMediaCatalogResponse
		}
		var page map[string]json.RawMessage
		if json.Unmarshal(body, &page) != nil || page == nil {
			return nil, ErrMediaCatalogResponse
		}
		var rows []json.RawMessage
		raw, exists := page[field]
		// Google may omit an empty repeated field. Other supported list
		// envelopes require data, including for an empty complete catalog.
		if (!exists && field != "models") || (exists && (bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &rows) != nil)) {
			return nil, ErrMediaCatalogResponse
		}
		for _, row := range rows {
			var item struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			if json.Unmarshal(row, &item) != nil {
				return nil, ErrMediaCatalogResponse
			}
			id := item.ID
			if field == "models" {
				id = strings.TrimPrefix(item.Name, "models/")
			}
			if id == "" || seenIDs[id] {
				return nil, ErrCatalogIncomplete
			}
			seenIDs[id] = true
			all = append(all, row)
		}
		param, cursor, err := catalogNextPage(t.typ, page)
		if err != nil {
			return nil, err
		}
		if cursor == "" {
			if err := req.Context().Err(); err != nil {
				return nil, err
			}
			if all == nil {
				all = []json.RawMessage{}
			}
			page[field], err = json.Marshal(all)
			if err != nil {
				return nil, ErrMediaCatalogResponse
			}
			complete, err := json.Marshal(page)
			if err != nil {
				return nil, ErrMediaCatalogResponse
			}
			resp.Body = io.NopCloser(bytes.NewReader(complete))
			resp.ContentLength = int64(len(complete))
			resp.Header = resp.Header.Clone()
			if resp.Header == nil {
				resp.Header = make(http.Header)
			}
			resp.Header.Set("Content-Length", strconv.Itoa(len(complete)))
			return resp, nil
		}
		if seenCursors[cursor] {
			return nil, ErrCatalogIncomplete
		}
		seenCursors[cursor] = true
		// A cursor is only a query value on the exact original destination.
		// Never follow upstream URLs, change hosts, or forward auth elsewhere.
		pageReq = req.Clone(req.Context())
		query := pageReq.URL.Query()
		query.Set(param, cursor)
		pageReq.URL.RawQuery = query.Encode()
	}
}

func catalogNextPage(typ models.ClientType, page map[string]json.RawMessage) (string, string, error) {
	var more bool
	if raw, ok := page["has_more"]; ok {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &more) != nil {
			return "", "", ErrCatalogIncomplete
		}
	}
	readCursor := func(key string) (string, error) {
		raw, ok := page[key]
		if !ok {
			return "", nil
		}
		var value string
		if json.Unmarshal(raw, &value) != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return "", ErrCatalogIncomplete
		}
		return value, nil
	}
	google, err := readCursor("nextPageToken")
	if err != nil {
		return "", "", err
	}
	other, err := readCursor("next_cursor")
	if err != nil || other != "" {
		return "", "", ErrCatalogIncomplete
	}
	switch typ {
	case models.ClientTypeAnthropicMessages:
		if google != "" {
			return "", "", ErrCatalogIncomplete
		}
		if more {
			cursor, err := readCursor("last_id")
			if err != nil || cursor == "" {
				return "", "", ErrCatalogIncomplete
			}
			return "after_id", cursor, nil
		}
	case models.ClientTypeGoogleGenerativeAI:
		if more {
			return "", "", ErrCatalogIncomplete
		}
		return "pageToken", google, nil
	default:
		// OpenAI /models documents a complete data list, not after/after_id.
		// A compatible server advertising pagination needs a documented contract;
		// do not guess its request parameters or treat its prefix as complete.
		if more || google != "" {
			return "", "", ErrCatalogIncomplete
		}
	}
	return "", "", nil
}

package postgresstore

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"path"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

func computerEndpoint(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("invalid computer endpoint")
	}
	host, port := strings.ToLower(u.Hostname()), u.Port()
	if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	// Fragment and userinfo do not identify an HTTP endpoint. Decode and clean
	// the path conservatively: ambiguous aliases must not reopen raw access.
	return strings.ToLower(u.Scheme) + "://" + host + path.Clean("/"+strings.TrimLeft(u.Path, "/")) + "?" + u.Query().Encode(), nil
}

// Reusing a static computer credential under another URL must not reopen the
// raw route. This compares identities locally; no secret enters receipts/logs.
func sameComputerCredential(a, b dbstore.JSON) bool {
	read := func(raw dbstore.JSON) map[string]string {
		var config struct {
			Headers map[string]string `json:"headers"`
		}
		_ = json.Unmarshal(raw, &config)
		values := map[string]string{}
		for key, value := range config.Headers {
			key = strings.ToLower(strings.TrimSpace(key))
			value = strings.TrimSpace(value) // Match the HTTP gateway's header normalization.
			if key == "authorization" {
				if parts := strings.Fields(value); len(parts) == 2 {
					value = strings.ToLower(parts[0]) + " " + parts[1]
				}
			}
			if value != "" && (key == "authorization" || key == "x-api-key") {
				values[key] = value
			}
		}
		return values
	}
	left, right := read(a), read(b)
	for key, value := range left {
		if right[key] == value {
			return true
		}
	}
	return false
}

func endpointSet(raw dbstore.JSON) (map[string]bool, error) {
	var config map[string]any
	if json.Unmarshal(raw, &config) != nil {
		return nil, errors.New("invalid endpoint config")
	}
	out := map[string]bool{}
	for _, key := range []string{"url", "sse_url", "sseUrl", "message_url", "messageUrl"} {
		if value, ok := config[key].(string); ok && value != "" {
			endpoint, err := computerEndpoint(value)
			if err != nil {
				return nil, err
			}
			out[endpoint] = true
		}
	}
	return out, nil
}

func (s *Store) IsManagedComputerConnection(ctx context.Context, id string) (bool, error) {
	return s.IsManagedComputerEndpoint(ctx, id, nil)
}

func (s *Store) IsManagedComputerEndpoint(ctx context.Context, id string, actual dbstore.JSON) (bool, error) {
	rows, err := s.queries.ListComputerConnectionEndpoints(ctx, id)
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		if row.Direct || sameComputerCredential(row.CandidateConfig, row.RegisteredConfig) {
			return true, nil
		}
		candidate := row.CandidateConfig
		if len(actual) > 0 {
			candidate = actual
		}
		left, err := endpointSet(candidate)
		if err != nil {
			return false, err
		}
		right, err := endpointSet(row.RegisteredConfig)
		if err != nil {
			return false, err
		}
		for endpoint := range left {
			if right[endpoint] {
				return true, nil
			}
		}
	}
	return false, nil
}

func computerIDs(r dbstore.ComputerOperationRecord) ([]pgtype.UUID, error) {
	ids := make([]pgtype.UUID, 0, 5)
	for i, value := range []string{r.ID, r.BotID, r.SessionID, r.UserID, r.TargetID} {
		if i == 4 && value == "" {
			ids = append(ids, pgtype.UUID{})
			continue
		}
		id, err := db.ParseUUID(value)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func operationRecord(r dbsqlc.ComputerOperation) dbstore.ComputerOperationRecord {
	return dbstore.ComputerOperationRecord{ID: r.ID.String(), BotID: r.BotID.String(), SessionID: r.SessionID.String(), UserID: r.UserID.String(), TargetID: r.TargetID.String(), RunID: r.RunID, Fingerprint: r.Fingerprint, TargetFingerprint: r.TargetFingerprint, State: r.State, Details: r.Details, CancelRequested: r.CancelRequested, Revision: r.Revision, CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time}
}

func (s *Store) ClaimComputerOperation(ctx context.Context, r dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, bool, error) {
	ids, err := computerIDs(r)
	if err != nil {
		return r, false, err
	}
	row, err := s.queries.CreateComputerOperation(ctx, dbsqlc.CreateComputerOperationParams{ID: ids[0], BotID: ids[1], SessionID: ids[2], UserID: ids[3], TargetID: ids[4], RunID: r.RunID, Fingerprint: r.Fingerprint, TargetFingerprint: r.TargetFingerprint, Details: r.Details})
	if err == nil {
		return operationRecord(row), true, nil
	}
	if !errors.Is(mapQueryErr(err), db.ErrNotFound) {
		return r, false, err
	}
	row, err = s.queries.FindComputerOperation(ctx, dbsqlc.FindComputerOperationParams{BotID: ids[1], SessionID: ids[2], UserID: ids[3], TargetID: ids[4], RunID: r.RunID, Fingerprint: r.Fingerprint})
	return operationRecord(row), false, mapQueryErr(err)
}

func (s *Store) ReadComputerOperation(ctx context.Context, r dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, error) {
	ids, err := computerIDs(r)
	if err != nil {
		return r, err
	}
	row, err := s.queries.GetComputerOperation(ctx, dbsqlc.GetComputerOperationParams{ID: ids[0], BotID: ids[1], SessionID: ids[2], UserID: ids[3]})
	return operationRecord(row), mapQueryErr(err)
}

func (s *Store) SaveComputerOperation(ctx context.Context, r dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, error) {
	ids, err := computerIDs(r)
	if err != nil {
		return r, err
	}
	row, err := s.queries.UpdateComputerOperation(ctx, dbsqlc.UpdateComputerOperationParams{ID: ids[0], BotID: ids[1], SessionID: ids[2], UserID: ids[3], State: r.State, Details: r.Details, CancelRequested: r.CancelRequested, Revision: r.Revision})
	return operationRecord(row), mapQueryErr(err)
}

func (s *Store) OwnedAgenvoRuntime(ctx context.Context, owner, id string) (dbstore.UserRuntimeRecord, error) {
	user, err := db.ParseUUID(owner)
	if err != nil {
		return dbstore.UserRuntimeRecord{}, err
	}
	runtime, err := db.ParseUUID(id)
	if err != nil {
		return dbstore.UserRuntimeRecord{}, err
	}
	row, err := s.queries.GetOwnedAgenvoRuntime(ctx, dbsqlc.GetOwnedAgenvoRuntimeParams{ID: runtime, UserID: user})
	return userRuntimeRecord(row), mapQueryErr(err)
}

func (s *Store) ConfigureAgenvoRuntime(ctx context.Context, owner, id string, previous, next dbstore.JSON) (dbstore.UserRuntimeRecord, error) {
	user, err := db.ParseUUID(owner)
	if err != nil {
		return dbstore.UserRuntimeRecord{}, err
	}
	runtime, err := db.ParseUUID(id)
	if err != nil {
		return dbstore.UserRuntimeRecord{}, err
	}
	row, err := s.queries.UpdateAgenvoRuntimeConfig(ctx, dbsqlc.UpdateAgenvoRuntimeConfigParams{ID: runtime, UserID: user, ExpectedConfig: previous, ProviderConfig: next})
	return userRuntimeRecord(row), mapQueryErr(err)
}

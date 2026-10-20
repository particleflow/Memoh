package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/apperror"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/storage"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
)

type originalMediaFailure struct{ storage.Provider }

func (originalMediaFailure) ListPrefix(context.Context, string) ([]string, error) {
	return nil, errors.New("PRIVATE_STORAGE_DIAGNOSTIC")
}

func TestOriginalMediaHTTPClassification(t *testing.T) {
	for _, tc := range []struct {
		name, rawHash string
		status        int
		storageFails  bool
		unauthorized  bool
	}{
		{"short hash", "ab", http.StatusNotFound, false, false},
		{"invalid hash", "not-a-hash", http.StatusNotFound, false, false},
		{"nonhex hash", strings.Repeat("g", 64), http.StatusNotFound, false, false},
		{"uppercase hash", strings.Repeat("A", 64), http.StatusNotFound, false, false},
		{"encoded path", "%2e%2e%2fprivate", http.StatusNotFound, false, false},
		{"double encoded path", "%252e%252e%252fprivate", http.StatusNotFound, false, false},
		{"encoded hash prefix and path", "sha256%3A..%2F..%2Fconfig.toml", http.StatusNotFound, false, false},
		{"missing original", strings.Repeat("a", 64), http.StatusNotFound, false, false},
		{"existing original", "", http.StatusOK, false, false},
		{"storage failure", strings.Repeat("a", 64), http.StatusInternalServerError, true, false},
		{"authorization before malformed hash", "not-a-hash", http.StatusForbidden, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var provider storage.Provider = localfs.NewOriginals(t.TempDir())
			if tc.storageFails {
				provider = originalMediaFailure{}
			}
			service := media.NewService(nil, provider)
			service.SetRequirePersistence(true)
			hash := tc.rawHash
			const original = "synthetic preserved original"
			if tc.status == http.StatusOK {
				asset, err := service.Ingest(context.Background(), media.IngestInput{
					BotID: invocationTestBotID, Mime: "text/plain", Reader: strings.NewReader(original),
				})
				if err != nil {
					t.Fatal(err)
				}
				hash = asset.ContentHash
			}
			queries := newInvocationTestQueries()
			queries.permissions = []byte(`[]`)
			role := "admin"
			if tc.unauthorized {
				role = "user"
			}
			h := &MessageHandler{
				mediaService:   service,
				botService:     bots.NewService(nil, queries),
				accountService: newTestAdminAccountService(role),
			}
			e := echo.New()
			e.HTTPErrorHandler = server.NewHTTPErrorHandler(slog.New(slog.DiscardHandler))
			e.GET("/bots/:bot_id/media/:content_hash", func(c echo.Context) error {
				c.Set("user", &jwt.Token{Valid: true, Claims: jwt.MapClaims{
					"sub": invocationTestOtherUser, "user_id": invocationTestOtherUser,
				}})
				return h.ServeMedia(c)
			})
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
				"/bots/"+invocationTestBotID+"/media/"+hash, nil))
			if rec.Code != tc.status {
				t.Fatalf("status=%d, want %d: %s", rec.Code, tc.status, rec.Body.String())
			}
			if tc.status == http.StatusOK {
				if rec.Body.String() != original {
					t.Fatal("valid original changed")
				}
				return
			}
			var problem server.Problem
			if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
				t.Fatal(err)
			}
			if tc.status == http.StatusNotFound && problem.Code != string(apperror.CodeMediaAssetNotFound) {
				t.Fatalf("not-found code=%q", problem.Code)
			}
			wantFault := apperror.FaultClient
			if tc.status == http.StatusInternalServerError {
				wantFault = apperror.FaultServer
				if problem.Code != "internal" {
					t.Fatalf("storage failure code=%q", problem.Code)
				}
			}
			if problem.Status != tc.status || problem.Fault != wantFault {
				t.Fatalf("incorrect problem classification: %+v", problem)
			}
			if strings.Contains(rec.Body.String(), "PRIVATE_STORAGE_DIAGNOSTIC") {
				t.Fatal("storage diagnostic leaked")
			}
		})
	}
}

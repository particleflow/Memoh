//go:build integration

package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/computertask"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/handlers"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/userruntime"
)

func exerciseDeviceRegistry(ctx context.Context, t *testing.T, store *postgresstore.Store, admin *pgxpool.Pool, id computertask.DeliveryIdentity) {
	t.Helper()
	v := newRegisteredTransferVerifier(ctx, t, store, id)
	c := v.Credentials
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := admin.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	denied := func() {
		t.Helper()
		if _, err := v.VerifyDevice(ctx, v.token); !errors.Is(err, userruntime.ErrTransferCredential) {
			t.Fatal("credential did not deny", err)
		}
		if err := v.RevalidateDevice(ctx, v.device); !errors.Is(err, userruntime.ErrTransferCredential) {
			t.Fatal("revalidation did not deny", err)
		}
	}
	allowed := func() {
		t.Helper()
		if _, err := v.VerifyDevice(ctx, v.token); err != nil {
			t.Fatal(err)
		}
	}
	row, err := store.ReadRuntimeTransfer(ctx, v.device.CredentialID)
	if err != nil || row.Digest == "" || strings.Contains(row.Digest, v.token) {
		t.Fatal("stored credential is not digest-only", err)
	}
	wrongSecret := v.token[:len(v.token)-1] + "a"
	if wrongSecret == v.token {
		wrongSecret = v.token[:len(v.token)-1] + "b"
	}
	for _, secret := range []string{"Bearer account-fixture", "agenvo_fixture", "mrk_" + strings.Repeat("a", 64), v.token + "x", wrongSecret} {
		if _, err := v.VerifyDevice(ctx, secret); err == nil {
			t.Fatal("wrong purpose/secret accepted")
		}
	}
	if _, err := c.Enroll(ctx, id.Binding.UserID, v.runtime, userruntime.ChangeTransferCredentialRequest{}); err == nil {
		t.Fatal("caller administered another owner's computer")
	}
	// Device expiry is independent of the fifteen-minute request context.
	exec("UPDATE user_runtimes SET transfer_expires_at=now()+interval '30 days' WHERE id=$1", v.runtime)
	allowed()
	exec("UPDATE user_runtimes SET transfer_expires_at=now()-interval '1 second' WHERE id=$1", v.runtime)
	denied()
	exec("UPDATE user_runtimes SET transfer_expires_at=NULL WHERE id=$1", v.runtime)
	allowed()
	for _, toggle := range []struct {
		off, on string
		arg     string
	}{
		{"UPDATE users SET is_active=false WHERE id=$1", "UPDATE users SET is_active=true WHERE id=$1", v.owner},
		{"UPDATE team_members SET is_active=false WHERE user_id=$1", "UPDATE team_members SET is_active=true WHERE user_id=$1", v.owner},
		{"UPDATE user_runtimes SET revoked_at=now() WHERE id=$1", "UPDATE user_runtimes SET revoked_at=NULL WHERE id=$1", v.runtime},
	} {
		exec(toggle.off, toggle.arg)
		denied()
		exec(toggle.on, toggle.arg)
		allowed()
	}
	exec("UPDATE mcp_connections SET is_active=false WHERE id=$1", v.device.ConnectionID)
	allowed() // An unrelated MCP connection cannot revoke native Runtime authority.
	exec("UPDATE mcp_connections SET is_active=true WHERE id=$1", v.device.ConnectionID)
	exec(`UPDATE user_runtimes SET provider_config=jsonb_set(provider_config,'{herdr,instance_id}','"other-instance"') WHERE id=$1`, v.runtime)
	denied()
	exec("UPDATE user_runtimes SET provider_config=$2 WHERE id=$1", v.runtime, []byte(row.Config))
	allowed()
	// A disabled connection can still have its credential explicitly revoked.
	exec("UPDATE mcp_connections SET is_active=false WHERE id=$1", v.device.ConnectionID)
	v.revoke(ctx, t)
	exec("UPDATE mcp_connections SET is_active=true WHERE id=$1", v.device.ConnectionID)
	denied()
	// Concurrent one-time enrollment: exactly one secret wins the same row CAS.
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			_, err := c.Enroll(ctx, v.owner, v.runtime, userruntime.ChangeTransferCredentialRequest{})
			results <- err
		})
	}
	wg.Wait()
	close(results)
	wins, conflicts := 0, 0
	for err := range results {
		if err == nil {
			wins++
		} else if errors.Is(err, userruntime.ErrTransferConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatal("enrollment CAS", wins, conflicts)
	}
	status, err := c.Status(ctx, v.owner, v.runtime)
	if err != nil || status.CredentialID == "" {
		t.Fatal("lost response status", err)
	}
	if _, err := c.Enroll(ctx, v.owner, v.runtime, userruntime.ChangeTransferCredentialRequest{}); !errors.Is(err, userruntime.ErrTransferConflict) {
		t.Fatal("lost enrollment repeated", err)
	}
	expires := time.Now().Add(24 * time.Hour)
	replacement, err := c.Enroll(ctx, v.owner, v.runtime, userruntime.ChangeTransferCredentialRequest{ExpectedID: status.CredentialID, ExpiresAt: &expires})
	if err != nil {
		t.Fatal(err)
	}
	if replacement.CredentialID == status.CredentialID {
		t.Fatal("rotation reused identity")
	}
	if _, err := c.VerifyDevice(ctx, replacement.Credential); err != nil {
		t.Fatal(err)
	}
	if err := c.Revoke(ctx, v.owner, v.runtime, status.CredentialID); !errors.Is(err, userruntime.ErrTransferConflict) {
		t.Fatal("stale revoke canceled replacement", err)
	}
	if err := c.Revoke(ctx, v.owner, v.runtime, replacement.CredentialID); err != nil {
		t.Fatal(err)
	}
	public, err := userruntime.NewService(store, nil).ListRuntimes(ctx, v.owner)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(public)
	if strings.Contains(string(raw), "transfer_credential") || strings.Contains(string(raw), replacement.Credential) {
		t.Fatal("ordinary runtime list leaked transfer credential")
	}
	exerciseCredentialHTTP(ctx, t, store, v.owner, v.runtime, id.Binding.UserID)
	disabled := userruntime.NewTransferCredentials(store, false)
	if _, err := disabled.Enroll(ctx, v.owner, v.runtime, userruntime.ChangeTransferCredentialRequest{}); !errors.Is(err, userruntime.ErrTransferUnavailable) {
		t.Fatal("disabled enrollment", err)
	}
}

func exerciseCredentialHTTP(ctx context.Context, t *testing.T, store *postgresstore.Store, owner, runtime, other string) {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	credentials := userruntime.NewTransferCredentials(store, true)
	api := server.NewServerWithSessionValidator(log, ":0", "isolated-setup-secret", accounts.NewService(log, store).ValidateSession, handlers.NewRuntimeTransferCredentialHandler(credentials)) //nolint:contextcheck // The server handles independent HTTP contexts.
	https := httptest.NewTLSServer(api)
	defer https.Close()
	request := func(user, method, body, query string, want int) []byte {
		t.Helper()
		token, _, err := auth.GenerateToken(user, "isolated-setup-secret", time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequestWithContext(ctx, method, https.URL+"/users/me/runtimes/"+runtime+"/transfer-credential"+query, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		response, err := https.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want || response.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("setup status %d cache %q", response.StatusCode, response.Header.Get("Cache-Control"))
		}
		return raw
	}
	request(other, "POST", `{"expected_id":""}`, "", 403)
	raw := request(owner, "POST", `{"expected_id":""}`, "", 201)
	var enrollment userruntime.TransferCredentialEnrollment
	if err := json.Unmarshal(raw, &enrollment); err != nil {
		t.Fatal(err)
	}
	if _, err := credentials.VerifyDevice(ctx, enrollment.Credential); err != nil {
		t.Fatal(err)
	}
	status := request(owner, "GET", "", "", 200)
	if strings.Contains(string(status), enrollment.Credential) || strings.Contains(string(status), "digest") {
		t.Fatal("setup status exposed secret")
	}
	request(owner, "POST", `{"expected_id":""}`, "", 409)
	request(owner, "DELETE", "", "?expected_id="+enrollment.CredentialID, 204)
	if _, err := credentials.VerifyDevice(ctx, enrollment.Credential); err == nil {
		t.Fatal("HTTP revocation did not invalidate device")
	}
}

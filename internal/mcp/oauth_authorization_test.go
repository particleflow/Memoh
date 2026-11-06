package mcp

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

type oauthAuthorizationFailQueries struct {
	dbstore.Queries
	failTokens bool
}

func (q *oauthAuthorizationFailQueries) UpdateMCPOAuthTokens(ctx context.Context, arg sqlc.UpdateMCPOAuthTokensParams) error {
	if q.failTokens {
		return errors.New("synthetic persistence failure")
	}
	return q.Queries.UpdateMCPOAuthTokens(ctx, arg)
}

func (*oauthAuthorizationFailQueries) UpdateMCPConnectionAuthType(context.Context, sqlc.UpdateMCPConnectionAuthTypeParams) error {
	return errors.New("synthetic connection update failure")
}

func TestOAuthAuthorizationPersistenceFailureIsNotSuccess(t *testing.T) {
	for _, failTokens := range []bool{true, false} {
		t.Run(map[bool]string{true: "tokens", false: "connection"}[failTokens], func(t *testing.T) {
			svc, queries, _, _ := completeLinearJourney(t)
			auth, err := svc.StartAuthorization(t.Context(), oauthJourneyConnID, "", "", oauthJourneyCallback)
			if err != nil {
				t.Fatal(err)
			}
			response := authorizeInBrowser(t, t.Context(), auth.AuthorizationURL)
			_ = response.Body.Close()
			redirect, err := url.Parse(response.Header.Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			failing := svc.WithQueries(&oauthAuthorizationFailQueries{Queries: queries, failTokens: failTokens})
			if _, err := failing.HandleCallback(t.Context(), redirect.Query().Get("state"), redirect.Query().Get("code")); err == nil {
				t.Fatal("expected persistence failure")
			}
			if svc.AuthorizationStatus(oauthJourneyConnID, auth.AuthorizationID).Status != "failed" {
				t.Fatal("failed persistence reported a successful transaction")
			}
		})
	}
}

func TestOAuthAuthorizationReceiptRequiresThisExchange(t *testing.T) {
	svc, queries, _, conn := completeLinearJourney(t)
	ctx := t.Context()
	// Reauthorization retains an old expired token. Its existence, timestamp or
	// refresh activity must not constitute completion of the new transaction.
	queries.mu.Lock()
	row := queries.byConn[conn]
	row.ExpiresAt = pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true}
	queries.byConn[conn] = row
	queries.mu.Unlock()
	auth, err := svc.StartAuthorization(ctx, oauthJourneyConnID, "", "", oauthJourneyCallback)
	if err != nil {
		t.Fatal(err)
	}
	status, err := svc.GetStatus(ctx, oauthJourneyConnID)
	if err != nil || !status.HasToken || !status.Expired || svc.AuthorizationStatus(oauthJourneyConnID, auth.AuthorizationID).Status != "pending" {
		t.Fatal("old token completed a new authorization")
	}
	if svc.AuthorizationStatus("another-connection", auth.AuthorizationID).Status != "unknown" || svc.AuthorizationStatus(oauthJourneyConnID, "another-flow").Status != "unknown" {
		t.Fatal("receipt crossed connection/flow boundary")
	}
	response := authorizeInBrowser(t, ctx, auth.AuthorizationURL)
	_ = response.Body.Close()
	redirect, err := url.Parse(response.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	// Request-scoped query rebinding shares only the bounded receipt observer.
	if _, err := svc.WithQueries(queries).HandleCallback(ctx, redirect.Query().Get("state"), redirect.Query().Get("code")); err != nil {
		t.Fatal(err)
	}
	if svc.AuthorizationStatus(oauthJourneyConnID, auth.AuthorizationID).Status != "succeeded" {
		t.Fatal("successful exchange did not complete its receipt")
	}
	next, err := svc.StartAuthorization(ctx, oauthJourneyConnID, "", "", oauthJourneyCallback)
	if err != nil {
		t.Fatal(err)
	}
	if svc.AuthorizationStatus(oauthJourneyConnID, auth.AuthorizationID).Status != "unknown" {
		t.Fatal("superseded authorization still current")
	}
	nextURL, err := url.Parse(next.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.HandleCallback(ctx, nextURL.Query().Get("state"), "invalid-synthetic-code"); err == nil {
		t.Fatal("expected failed exchange")
	}
	if svc.AuthorizationStatus(oauthJourneyConnID, next.AuthorizationID).Status != "failed" {
		t.Fatal("failed exchange looked successful because an old token exists")
	}
	if NewOAuthService(nil, queries, oauthJourneyCallback).AuthorizationStatus(oauthJourneyConnID, next.AuthorizationID).Status != "unknown" {
		t.Fatal("restart inferred a transaction from persisted tokens")
	}
}

func TestOAuthAuthorizationLateCompletionAndExpiry(t *testing.T) {
	svc := NewOAuthService(nil, nil, "")
	old := svc.authorizations.begin("a", "first-state")
	current := svc.authorizations.begin("a", "second-state")
	svc.authorizations.finish("a", "first-state", true)
	if svc.AuthorizationStatus("a", old).Status != "unknown" || svc.AuthorizationStatus("a", current).Status != "pending" {
		t.Fatal("late completion affected a newer flow")
	}
	entry := svc.authorizations.entries["a"]
	entry.expires = time.Now().Add(-time.Second)
	svc.authorizations.entries["a"] = entry
	svc.authorizations.finish("a", "second-state", true)
	if svc.AuthorizationStatus("a", current).Status != "unknown" {
		t.Fatal("expired receipt became successful")
	}
}

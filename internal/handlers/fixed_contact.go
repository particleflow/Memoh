package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/felinics/memoh/internal/apperror"
	"github.com/felinics/memoh/internal/channel/route"
	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/labstack/echo/v4"
)

// These legacy public mutations create parallel chats or destroy continuity.
// Background tasks use the internal task services, never these CRUD endpoints.
func rejectParallelChat(echo.Context) error { return apperror.New(apperror.CodeHTTPForbidden, nil) }

func resetsConversation(text string) bool {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return false
	}
	name := strings.TrimPrefix(strings.ToLower(fields[0]), "/")
	switch name {
	case "new", "reset", "clear":
		return true
	}
	return false
}

// First binding is system-owned. A unique verified legacy chat can be adopted;
// ambiguous legacy roots require an explicit administrative migration, not a
// normal chat picker. Existing bindings are never replaced.
func resolveContinuousContact(ctx context.Context, resolver privateChatResolver, bot, user string) (thread.Thread, error) {
	if resolver == nil {
		return thread.Thread{}, apperror.New(apperror.CodeHTTPServiceUnavailable, nil)
	}
	if navigator, ok := resolver.(privateChatNavigator); ok {
		entry, err := navigator.InspectPrivateChat(ctx, bot, user, "")
		if err != nil {
			return thread.Thread{}, contactNavigationError(err)
		}
		if entry.Session != nil {
			return *entry.Session, nil
		}
		if len(entry.Candidates) > 0 {
			if len(entry.Candidates) != 1 || entry.NextCursor != "" {
				return thread.Thread{}, contactNavigationError(route.ErrContactConflict)
			}
			bound, err := navigator.BindPrivateChat(ctx, bot, user, entry.Candidates[0].ID)
			if errors.Is(err, route.ErrContactConflict) {
				// Another device may have won. Read the committed identity; never choose
				// another candidate or repeat an uncertain mutation.
				read, readErr := navigator.InspectPrivateChat(ctx, bot, user, "")
				if readErr != nil {
					return thread.Thread{}, readErr
				}
				if read.Session != nil {
					return *read.Session, nil
				}
			}
			return bound, contactNavigationError(err)
		}
	}
	result, err := resolver.EnsurePrivateChat(ctx, bot, user)
	return result, contactNavigationError(err)
}

func (h *LocalChannelHandler) SetPrivateChatResolver(resolver privateChatResolver) {
	h.privateChats = resolver
}
func (h *LocalChannelHandler) resolveWebMessageContact(ctx context.Context, bot, user, requested, workdir string) (thread.Thread, error) {
	contact, err := resolveContinuousContact(ctx, h.privateChats, bot, user)
	if err != nil {
		return thread.Thread{}, err
	}
	if strings.TrimSpace(requested) != "" && requested != contact.ID {
		return thread.Thread{}, apperror.New(apperror.CodeHTTPForbidden, nil)
	}
	if strings.TrimSpace(workdir) != "" && workdir != contact.WorkdirID {
		return thread.Thread{}, apperror.New(apperror.CodeHTTPConflict, nil)
	}
	return contact, nil
}

// Keep model selection without exposing descriptor resets, renames, or writes
// to legacy history. The normal handler still performs its full authorization.
func (h *SessionHandler) fixedContactPreferences(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		user, bot, err := h.contactAccount(c)
		if err != nil {
			return err
		}
		navigator, ok := h.privateChats.(privateChatNavigator)
		if !ok {
			return apperror.New(apperror.CodeHTTPServiceUnavailable, nil)
		}
		entry, err := navigator.InspectPrivateChat(c.Request().Context(), bot, user, "")
		if err != nil {
			return contactNavigationError(err)
		}
		if entry.Session == nil || entry.Session.ID != c.Param("session_id") {
			return rejectParallelChat(c)
		}
		data, err := io.ReadAll(io.LimitReader(c.Request().Body, 65537))
		if err != nil || len(data) > 65536 {
			return apperror.New(apperror.CodeHTTPBadRequest, nil)
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(data, &fields) != nil {
			return apperror.New(apperror.CodeHTTPBadRequest, nil)
		}
		for key := range fields {
			switch key {
			case "preferred_chat_model_id", "preferred_reasoning_effort", "expected_model_preference_revision":
			default:
				return rejectParallelChat(c)
			}
		}
		c.Request().Body = io.NopCloser(bytes.NewReader(data))
		return next(c)
	}
}

func requireExistingContact(ctx context.Context, resolver privateChatResolver, bot, user, sessionID string) error {
	navigator, ok := resolver.(privateChatNavigator)
	if !ok {
		return apperror.New(apperror.CodeHTTPServiceUnavailable, nil)
	}
	entry, err := navigator.InspectPrivateChat(ctx, bot, user, "")
	if err != nil {
		return contactNavigationError(err)
	}
	if entry.Session == nil || entry.Session.ID != sessionID {
		return apperror.New(apperror.CodeHTTPForbidden, nil)
	}
	return nil
}
func (h *LocalChannelHandler) authorizeWSContactWrite(ctx context.Context, user, bot, sessionID string) error {
	if err := h.authorizeWSSession(ctx, user, bot, sessionID); err != nil {
		return err
	}
	return requireExistingContact(ctx, h.privateChats, bot, user, sessionID)
}
func (h *SessionQueueHandler) SetPrivateChatResolver(resolver privateChatResolver) {
	h.privateChats = resolver
}

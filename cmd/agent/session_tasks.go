package main

import (
	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/boot"
	"github.com/felinics/memoh/internal/bots"
	thread "github.com/felinics/memoh/internal/chat/thread"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/handlers"
	"github.com/felinics/memoh/internal/sessiontask"
	"github.com/felinics/memoh/internal/workdir"
)

func provideSessionTaskHandler(store *postgresstore.Store, sessions *thread.Service, workdirs *workdir.Service, targets *agenvo.Service, bots *bots.Service, accounts *accounts.Service, runtime *boot.RuntimeConfig) *handlers.SessionTaskHandler {
	return handlers.NewSessionTaskHandler(sessiontask.NewService(store, sessions, workdirs, targets, runtime.JwtSecret), bots, accounts)
}

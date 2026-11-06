package schedule

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/robfig/cron/v3"

	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	"github.com/felinics/memoh/internal/job"
)

// dueOnce lets the existing cron scheduler own the timer while the callback
// captures an immutable occurrence. Reading Entry.Prev inside a delayed
// callback would silently substitute a later occurrence.
type dueOnce struct {
	due    time.Time
	issued bool
}

func (d *dueOnce) Next(time.Time) time.Time {
	if d.issued {
		return time.Time{}
	}
	d.issued = true
	return d.due
}

func nextContactOccurrence(row sqlc.Schedule, parsed cron.Schedule, loc *time.Location, after time.Time) time.Time {
	// @every must share a durable anchor across process starts. Calendar patterns
	// already identify the same instant regardless of registration time.
	if interval, ok := parsed.(cron.ConstantDelaySchedule); ok {
		first := interval.Next(row.CreatedAt.Time)
		if first.After(after) {
			return first
		}
		return first.Add((after.Sub(first)/interval.Delay + 1) * interval.Delay)
	}
	return newLocationSchedule(parsed, loc).Next(after)
}

func (s *Service) registerContactOccurrence(row sqlc.Schedule, parsed cron.Schedule, loc *time.Location) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := row.ID.String()
	if previous, ok := s.jobs[id]; ok {
		s.cron.Remove(previous)
	}
	s.registerContactOccurrenceLocked(row, parsed, loc, nextContactOccurrence(row, parsed, loc, time.Now()))
	return nil
}

func (s *Service) registerContactOccurrenceLocked(row sqlc.Schedule, parsed cron.Schedule, loc *time.Location, due time.Time) {
	id := row.ID.String()
	if due.IsZero() {
		delete(s.jobs, id)
		return
	}
	var entry cron.EntryID
	var once sync.Once
	entry = s.cron.Schedule(&dueOnce{due: due}, cron.FuncJob(func() {
		once.Do(func() {
			// Re-arm only this registration, before doing long work. Replacement/removal
			// must not be overwritten by a late callback. The durable claim query
			// separately serializes execution authorization with pause.
			s.mu.Lock()
			if s.jobs[id] == entry {
				s.cron.Remove(entry)
				s.registerContactOccurrenceLocked(row, parsed, loc, nextContactOccurrence(row, parsed, loc, time.Now()))
			}
			s.mu.Unlock()
			ctx := s.lifecycleCtx
			if ctx == nil {
				ctx = context.Background()
			}
			_ = job.Run(ctx, s.logger, "schedule.fire", job.Options{OwnRequestID: true}, func(ctx context.Context) error {
				return fireResult(ctx, s.runScheduleOccurrence(ctx, toSchedule(row), due))
			}, slog.String("schedule_id", id), slog.String("bot_id", row.BotID.String()))
		})
	}))
	s.jobs[id] = entry
}

func (s *Service) runScheduleOccurrence(ctx context.Context, sched Schedule, due time.Time) error {
	if s.triggerer == nil {
		return errors.New("schedule triggerer not configured")
	}
	if due.IsZero() {
		return errors.New("schedule occurrence time missing")
	}
	// Use the credential captured by this registration, never reload the latest
	// generation here: doing so would reauthorize a pre-pause callback on resume.
	claimed, err := s.queries.ClaimScheduleOccurrence(ctx, sqlc.ClaimScheduleOccurrenceParams{
		ScheduleID: toUUID(sched.ID), ScheduledFor: pgtype.Timestamptz{Time: due.UTC(), Valid: true},
		RegistrationGeneration: sched.registrationGeneration,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	} // paused/deleted/replaced registration, or another callback owns this occurrence
	if err != nil {
		return err
	} // uncertain write: never create a second fire
	claim := sqlc.ScheduleLog(claimed)
	return s.runScheduleWithClaim(ctx, sched, &claim)
}

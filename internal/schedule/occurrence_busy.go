package schedule

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/felinics/memoh/internal/db/postgres/sqlc"
)

// Only a definitive pre-admission busy refusal may release this occurrence's
// count. Keep its durable identity as a failed, unexecuted fire; never retry it
// under a new run/session or silently consume a max_calls allowance.
func (s *Service) finishBusyOccurrence(ctx context.Context, sched Schedule, claim sqlc.ScheduleLog, busy error) error {
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, err := s.queries.RefundBusyScheduleOccurrence(persist, sqlc.RefundBusyScheduleOccurrenceParams{
		ScheduleID: claim.ScheduleID, BotID: claim.BotID, FireID: claim.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Already settled, unknown, or the original invocation exists. Do
		// not overwrite its evidence or infer that execution can be repeated.
		return busy
	}
	if err != nil {
		// The write outcome may be unknown. Existing recovery reads the original
		// invocation/budget; do not finish a second log or retry the execution.
		return fmt.Errorf("persist unadmitted busy occurrence: %w", err)
	}
	s.publishChanged(sched.BotID, sched.ID)
	if s.resultHooks != nil {
		s.resultHooks.WakeScheduleResults(persist)
	}
	row, err := s.queries.GetScheduleByID(persist, claim.ScheduleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return busy
	} // deleted plan still retains the fire
	if err != nil {
		return err
	}
	// A limit stop may have removed the local timer. Re-arm only the persisted
	// plan; explicit pauses remain disabled and the row-locked claim is final.
	if err = s.rescheduleJob(persist, row); err != nil {
		return err
	}
	return busy
}

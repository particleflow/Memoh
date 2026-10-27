package postgresstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/felinics/memoh/internal/catalogsync"
	"github.com/felinics/memoh/internal/db"
	"github.com/jackc/pgx/v5"
)

// ApplyModelCatalog applies one complete chat catalog transaction. Only model
// config's discovery-owned fields are touched; the row's enable, name, type,
// identity and all user-owned config survive concurrent user writes.
func (q *Queries) ApplyModelCatalog(ctx context.Context, providerID string, expectedConfig []byte, expectedClientType string, records []catalogsync.Model) error {
	if q == nil || q.pool == nil {
		return errors.New("catalog sync requires a transactional database")
	}
	id, err := db.ParseUUID(providerID)
	if err != nil {
		return err
	}
	seen := make(map[string]bool, len(records))
	for _, m := range records {
		if m.ID == "" || seen[m.ID] {
			return errors.New("invalid or duplicate catalog model ID")
		}
		seen[m.ID] = true
		if _, err := catalogsync.NewConfig(m.Config); err != nil {
			return err
		}
	}
	tx, err := q.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Synchronizers across server processes serialize by provider, while user
	// edits acquire ordinary row locks and do not need this advisory lock.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, providerID); err != nil {
		return err
	}
	var enabled, unchanged bool
	if err = tx.QueryRow(ctx, `SELECT enable, config = $2::jsonb AND client_type = $3 FROM providers WHERE team_id = public.memoh_current_team_id() AND id = $1 FOR UPDATE`, id, expectedConfig, expectedClientType).Scan(&enabled, &unchanged); err != nil {
		return err
	}
	if !enabled || !unchanged {
		return errors.New("provider changed or disabled during catalog sync")
	}
	for _, m := range records {
		initial, err := catalogsync.NewConfig(m.Config)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO models (provider_id,model_id,name,type,enable,config)
    VALUES ($1,$2,$3,'chat',true,$4::jsonb) ON CONFLICT (team_id,provider_id,model_id) DO NOTHING`, id, m.ID, m.Name, initial)
		if err != nil {
			return fmt.Errorf("insert catalog model: %w", err)
		}
		var rowID string
		var config []byte
		var typ string
		err = tx.QueryRow(ctx, `SELECT id,config,type FROM models WHERE team_id = public.memoh_current_team_id() AND provider_id=$1 AND model_id=$2 FOR UPDATE`, id, m.ID).Scan(&rowID, &config, &typ)
		if err != nil {
			return err
		}
		if typ != "chat" {
			continue
		}
		merged, err := catalogsync.Merge(config, m.Config)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE models SET config=$1::jsonb,updated_at=now() WHERE team_id = public.memoh_current_team_id() AND id=$2 AND config IS DISTINCT FROM $1::jsonb`, merged, rowID); err != nil {
			return err
		}
	}
	ids := make([]string, 0, len(records))
	for _, m := range records {
		ids = append(ids, m.ID)
	}
	// Never delete historical rows or touch embedding models omitted by no-probe
	// discovery. Empty successful chat catalog is a valid removal observation.
	_, err = tx.Exec(ctx, `UPDATE models SET config=jsonb_set(config,'{catalog_available}','false'::jsonb,true),updated_at=now()
  WHERE team_id=public.memoh_current_team_id() AND provider_id=$1 AND type='chat' AND NOT (model_id=ANY($2::text[])) AND config->'catalog_available' IS DISTINCT FROM 'false'::jsonb`, id, ids)
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		if errors.Is(err, pgx.ErrTxCommitRollback) {
			return err
		}
		return fmt.Errorf("catalog commit: %w", err)
	}
	return nil
}

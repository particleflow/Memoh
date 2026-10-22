//go:build integration

package db_test

import (
	"io/fs"
	"testing"
)

func TestComputerDeliveryPostgresTransferCredentialMigration(t *testing.T) {
	pool := freshMigratedDB(t)
	apply := func(name string) {
		t.Helper()
		raw, err := fs.ReadFile(postgresMigrationsFS(t), name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(t.Context(), string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	count := func(want int) {
		t.Helper()
		var n int
		if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='user_runtimes' AND column_name LIKE 'transfer_%'").Scan(&n); err != nil || n != want {
			t.Fatal("credential migration fields", n, err)
		}
	}
	count(4)
	apply("0172_runtime_transfer_credentials.down.sql")
	apply("0172_runtime_transfer_credentials.down.sql")
	count(0)
	apply("0172_runtime_transfer_credentials.up.sql")
	apply("0172_runtime_transfer_credentials.up.sql")
	count(4)
}

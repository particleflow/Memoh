package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestLoadNativeObservationCapacity(t *testing.T) {
	for _, limit := range []int{-1, 0, 1, 32, 128, 129} {
		t.Run(strconv.Itoa(limit), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(fmt.Sprintf("[computer_tasks]\nnative_observation_limit = %d\n", limit)), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			invalid := limit < 0 || limit > 128
			if invalid {
				if err == nil {
					t.Fatal("unbounded capacity accepted")
				}
				return
			}
			if err != nil || cfg.ComputerTasks.NativeObservationLimit != limit {
				t.Fatalf("capacity not loaded: %v", err)
			}
		})
	}
}

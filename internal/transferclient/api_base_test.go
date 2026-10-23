package transferclient

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

// These language-independent vectors are for the existing endpoint/mobile
// configuration owners too. Acceptance never authorizes or probes a destination.
func TestAPIBaseContractVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/api-base-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Contract string `json:"contract"`
		Cases    []struct {
			Name     string `json:"name"`
			Input    string `json:"input"`
			Accepted bool   `json:"accepted"`
		} `json:"cases"`
	}
	if json.Unmarshal(raw, &fixture) != nil || fixture.Contract != "memoh.file-api-base.v1" || len(fixture.Cases) == 0 {
		t.Fatal("invalid shared API base vectors")
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			if err := ValidateAPIBase(tc.Input); (err == nil) != tc.Accepted {
				t.Fatal("shared validator disagrees with contract")
			}
			calls := 0
			auth := func(context.Context) (string, error) { calls++; return "", ErrAuthorization }
			for _, task := range []bool{false, true} {
				var c *Client
				var err error
				if task {
					c, err = NewTask(tc.Input, "operation", "delivery", nil, auth)
				} else {
					c, err = New(tc.Input, nil, auth)
				}
				if tc.Accepted {
					if err != nil {
						t.Fatalf("task=%v valid configured base rejected: %v", task, err)
					}
					want := strings.TrimSuffix(tc.Input, "/")
					if task {
						want += "/internal/computer-transfers/operation/delivery"
					}
					if c.base != want {
						t.Fatal("prefix or identity changed")
					}
					c.CloseIdleConnections()
				} else if !errors.Is(err, ErrInvalid) {
					t.Fatalf("task=%v ambiguous base accepted", task)
				}
			}
			if calls != 0 {
				t.Fatal("URL validation requested credentials")
			}
		})
	}
}

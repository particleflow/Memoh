package transferclient

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestTaskAPIBaseLengthBoundary(t *testing.T) {
	const prefix = "https://nas.example.test/"
	operation, delivery := strings.Repeat("o", 128), strings.Repeat("d", 128)
	for _, length := range []int{2047, 2048, 2049} {
		for _, trailingSlash := range []bool{false, true} {
			t.Run(fmt.Sprintf("bytes-%d/trailing-%t", length, trailingSlash), func(t *testing.T) {
				base := prefix + strings.Repeat("a", length-len(prefix))
				if trailingSlash {
					base = base[:len(base)-1] + "/"
				}
				calls := 0
				auth := func(context.Context) (string, error) { calls++; return "", ErrAuthorization }
				accepted := length <= 2048
				if err := ValidateAPIBase(base); (err == nil) != accepted {
					t.Fatal("configured base length policy changed")
				}
				ordinary, err := New(base, nil, auth)
				if (err == nil) != accepted {
					t.Fatal("ordinary client disagrees with configured base limit")
				}
				if ordinary != nil {
					ordinary.CloseIdleConnections()
				}
				c, err := NewTask(base, operation, delivery, nil, auth)
				if accepted {
					if err != nil {
						t.Fatalf("valid %d-byte API base rejected by task suffix: %v", length, err)
					}
					defer c.CloseIdleConnections()
					if c.base != strings.TrimSuffix(base, "/")+"/internal/computer-transfers/"+operation+"/"+delivery {
						t.Fatal("base prefix or bounded task IDs changed")
					}
				} else if !errors.Is(err, ErrInvalid) {
					t.Fatal("oversized configured base accepted")
				}
				if calls != 0 {
					t.Fatal("constructor read credentials")
				}
			})
		}
	}
}

func TestTaskAPIBaseBoundaryPreservesChecks(t *testing.T) {
	const prefix = "https://nas.example.test/"
	base := prefix + strings.Repeat("a", 2048-len(prefix))
	auth := func(context.Context) (string, error) {
		t.Fatal("constructor read credentials")
		return "", ErrAuthorization
	}
	insecure := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} //nolint:gosec // Negative constructor test; no requests are made.
	for _, tc := range []struct {
		name, base, operation, delivery string
		client                          *http.Client
		auth                            Authorization
	}{
		{"query-at-limit", base[:len(base)-1] + "?", "operation", "delivery", nil, auth},
		{"operation-path", base, "../operation", "delivery", nil, auth},
		{"delivery-path", base, "operation", "delivery/other", nil, auth},
		{"operation-too-long", base, strings.Repeat("o", 129), "delivery", nil, auth},
		{"delivery-too-long", base, "operation", strings.Repeat("d", 129), nil, auth},
		{"missing-auth", base, "operation", "delivery", nil, nil},
		{"insecure-transport", base, "operation", "delivery", insecure, auth},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewTask(tc.base, tc.operation, tc.delivery, tc.client, tc.auth); !errors.Is(err, ErrInvalid) {
				t.Fatal("task constructor bypassed existing input or TLS policy")
			}
		})
	}
}

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

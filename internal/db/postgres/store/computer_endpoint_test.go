package postgresstore

import "testing"

func TestComputerEndpointAliases(t *testing.T) {
	want, err := computerEndpoint("https://example.invalid/computer")
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"https://EXAMPLE.invalid:443/computer#alias", "https://example.invalid/computer/", "https://example.invalid/%63omputer", "https://example.invalid/a/../computer"} {
		got, err := computerEndpoint(input)
		if err != nil || got != want {
			t.Fatalf("alias not normalized: %q %v", input, err)
		}
	}
	for _, input := range []string{"http://example.invalid/computer", "https://example.invalid/other", "https://other.invalid/computer"} {
		got, err := computerEndpoint(input)
		if err != nil || got == want {
			t.Fatalf("distinct endpoint changed: %q %v", input, err)
		}
	}
}

func TestComputerCredentialIdentityMatchesHTTPNormalization(t *testing.T) {
	registered := []byte(`{"headers":{"Authorization":"Bearer synthetic-only"}}`)
	for _, raw := range []string{`{"headers":{"authorization":"  Bearer synthetic-only  "}}`, `{"headers":{"AUTHORIZATION":"bearer synthetic-only"}}`} {
		if !sameComputerCredential(registered, []byte(raw)) {
			t.Fatal("equivalent HTTP credential reopened raw access")
		}
	}
	for _, raw := range []string{`{"headers":{}}`, `{"headers":{"Authorization":"Bearer another-token"}}`} {
		if sameComputerCredential(registered, []byte(raw)) {
			t.Fatal("unrelated credential reserved")
		}
	}
}

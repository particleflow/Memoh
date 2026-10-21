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

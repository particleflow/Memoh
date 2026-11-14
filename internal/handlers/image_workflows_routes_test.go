package handlers

import (
	"encoding/json"
	"net/http"
	"testing"
)

// F9: the route table's JSON field names are the wire contract the directory
// watcher and the phone read; they must not drift.
func TestF9ImageWorkflowRouteWireFields(t *testing.T) {
	e := newImageWorkflowTestServer(t)
	rec := albumPairCall(e, http.MethodGet, "/internal/image-workflow-routes", imageWorkflowToken(t), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("routes = %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Routes []map[string]json.RawMessage `json:"routes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode routes: %v", err)
	}
	// An empty table still proves the envelope. A non-empty one must carry the
	// five contract fields and nothing else.
	for _, route := range body.Routes {
		want := map[string]bool{"input_dir": true, "input_pair_key": true, "workflow_id": true, "bot_id": true, "state": true}
		for field := range route {
			if !want[field] {
				t.Fatalf("route carries an unknown field %q", field)
			}
			delete(want, field)
		}
		if len(want) > 0 {
			t.Fatalf("route is missing contract fields: %v", want)
		}
	}
}

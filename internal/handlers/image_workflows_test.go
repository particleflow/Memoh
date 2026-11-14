package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/felinics/memoh/internal/imageworkflow"
)

// F3: the workflow surface authenticates with the caller's own session, exactly
// like the album-pair surface. This is the one contract fact the stub already
// satisfies: the routes exist and the JWT boundary guards them.
func TestF3ImageWorkflowHTTPRequiresTheOwnersToken(t *testing.T) {
	e := newImageWorkflowTestServer(t)
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/users/me/image-workflows", ""},
		{http.MethodPost, "/users/me/image-workflows", `{"name":"n","instruction":"x","bot_id":"` + albumPairBot + `"}`},
		{http.MethodGet, "/users/me/image-workflows/" + imageWorkflowID, ""},
		{http.MethodPut, "/users/me/image-workflows/" + imageWorkflowID, `{"name":"n"}`},
		{http.MethodDelete, "/users/me/image-workflows/" + imageWorkflowID, ""},
		{http.MethodPost, "/users/me/image-workflows/" + imageWorkflowID + "/pause", ""},
		{http.MethodPost, "/users/me/image-workflows/" + imageWorkflowID + "/resume", ""},
		{http.MethodPost, "/bots/" + albumPairBot + "/image-workflows/" + imageWorkflowID + "/trigger", `{"source":"upload","files":[]}`},
		{http.MethodGet, "/internal/image-workflow-routes", ""},
	} {
		if rec := albumPairCall(e, tc.method, tc.path, "", tc.body); rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s anonymous = %d %s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

// F3: listing an empty archive answers an empty list, not an error.
func TestF3ImageWorkflowHTTPListAnswersAnEmptyArchive(t *testing.T) {
	e := newImageWorkflowTestServer(t)
	rec := albumPairCall(e, http.MethodGet, "/users/me/image-workflows", imageWorkflowToken(t), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d %s", rec.Code, rec.Body.String())
	}
	var body imageworkflow.ListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(body.Workflows) != 0 {
		t.Fatalf("workflows = %+v, want none", body.Workflows)
	}
}

// F3: create answers 201 with the minted labels.
func TestF3ImageWorkflowHTTPCreatesAWorkflow(t *testing.T) {
	e := newImageWorkflowTestServer(t)
	rec := albumPairCall(e, http.MethodPost, "/users/me/image-workflows", imageWorkflowToken(t),
		`{"name":"denoise","instruction":"sharpen then denoise","bot_id":"`+albumPairBot+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
	var created imageworkflow.Workflow
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.State != imageworkflow.StateActive || created.InputPairKey == "" || created.OutputPairKey == "" {
		t.Fatalf("created workflow = %+v", created)
	}
}

// F3: create rejects an unknown field and a malformed body at the boundary.
func TestF3ImageWorkflowHTTPRejectsMalformedCreates(t *testing.T) {
	e := newImageWorkflowTestServer(t)
	token := imageWorkflowToken(t)
	for _, tc := range []struct{ name, body string }{
		{"unknown field", `{"name":"n","instruction":"x","bot_id":"` + albumPairBot + `","album_uri":"file://media/x"}`},
		{"not json", `{"name":`},
		{"missing instruction", `{"name":"n","bot_id":"` + albumPairBot + `"}`},
	} {
		if rec := albumPairCall(e, http.MethodPost, "/users/me/image-workflows", token, tc.body); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d %s", tc.name, rec.Code, rec.Body.String())
		}
	}
}

// F3: an unknown workflow id is not_found, never a generic failure.
func TestF3ImageWorkflowHTTPReportsAnUnknownWorkflow(t *testing.T) {
	e := newImageWorkflowTestServer(t)
	token := imageWorkflowToken(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/users/me/image-workflows/" + imageWorkflowID},
		{http.MethodDelete, "/users/me/image-workflows/" + imageWorkflowID},
	} {
		if rec := albumPairCall(e, tc.method, tc.path, token, ""); rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s = %d %s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

// F4: the trigger answers 202 with the run identity for an accepted batch.
func TestF4ImageWorkflowHTTPTriggerAnswersAccepted(t *testing.T) {
	e := newImageWorkflowTestServer(t)
	body := `{"source":"directory","files":[{"path":"a.jpg","size_bytes":10,"mime":"image/jpeg"}]}`
	rec := albumPairCall(e, http.MethodPost,
		"/bots/"+albumPairBot+"/image-workflows/"+imageWorkflowID+"/trigger", imageWorkflowToken(t), body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("trigger = %d %s", rec.Code, rec.Body.String())
	}
}

// F9: the route table answers a routes array for the caller.
func TestF9ImageWorkflowHTTPRoutesAnswerAnArray(t *testing.T) {
	e := newImageWorkflowTestServer(t)
	rec := albumPairCall(e, http.MethodGet, "/internal/image-workflow-routes", imageWorkflowToken(t), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("routes = %d %s", rec.Code, rec.Body.String())
	}
	var body imageworkflow.RoutesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode routes: %v", err)
	}
}

// F7: pause and resume report the new state and the armed count.
func TestF7ImageWorkflowHTTPPauseAndResume(t *testing.T) {
	e := newImageWorkflowTestServer(t)
	token := imageWorkflowToken(t)
	if rec := albumPairCall(e, http.MethodPost, "/users/me/image-workflows/"+imageWorkflowID+"/pause", token, ""); rec.Code != http.StatusOK {
		t.Fatalf("pause = %d %s", rec.Code, rec.Body.String())
	}
	rec := albumPairCall(e, http.MethodPost, "/users/me/image-workflows/"+imageWorkflowID+"/resume", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("resume = %d %s", rec.Code, rec.Body.String())
	}
	var body imageworkflow.ResumeResult
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode resume: %v", err)
	}
}

package handlers

import (
	"log/slog"

	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/apperror"
	"github.com/felinics/memoh/internal/imageworkflow"
)

// ImageWorkflowHandler serves the image workflow surface: the user's own
// workflow configuration, the album-role pairings behind it, the bot-scoped
// trigger the directory watcher and the phone call, and the route table the
// watcher reads.
//
// Frozen contract: contracts/image-workflows.json.
//
// This handler is a stub for the test-first round. Every route is registered so
// the contract tests compile and reach the boundary, and every route answers
// 501 until the feature lands.
type ImageWorkflowHandler struct {
	logger  *slog.Logger
	service *imageworkflow.Service
}

func NewImageWorkflowHandler(log *slog.Logger, service *imageworkflow.Service) *ImageWorkflowHandler {
	if log == nil {
		log = slog.Default()
	}
	return &ImageWorkflowHandler{logger: log.With(slog.String("handler", "image_workflow")), service: service}
}

func (h *ImageWorkflowHandler) Register(e *echo.Echo) {
	g := e.Group("/users/me/image-workflows")
	g.GET("", h.List)
	g.POST("", h.Create)
	g.GET("/:workflow_id", h.Get)
	g.PUT("/:workflow_id", h.Update)
	g.DELETE("/:workflow_id", h.Revoke)
	g.POST("/:workflow_id/pause", h.Pause)
	g.POST("/:workflow_id/resume", h.Resume)

	bot := e.Group("/bots/:bot_id/image-workflows")
	bot.POST("/:workflow_id/trigger", h.Trigger)

	e.GET("/internal/image-workflow-routes", h.Routes)
}

// List godoc
// @Summary List the caller's image workflows
// @Description Every workflow this user owns, oldest first, revoked rows included.
// @Tags image-workflows
// @Produce json
// @Success 200 {object} imageworkflow.ListResponse
// @Failure 400 {object} server.Problem
// @Failure 503 {object} server.Problem
// @Router /users/me/image-workflows [get].
func (h *ImageWorkflowHandler) List(c echo.Context) error {
	// TODO(F3): read the owner from the request, call service.List, answer 200.
	return notImplemented(c)
}

// Create godoc
// @Summary Create an image workflow
// @Description Registers a workflow in state active and mints its input and output pairing labels. The albums themselves are designated on the phone.
// @Tags image-workflows
// @Accept json
// @Produce json
// @Param request body imageworkflow.CreateInput true "Name, instruction and executing Bot"
// @Success 201 {object} imageworkflow.Workflow
// @Failure 400 {object} server.Problem
// @Failure 404 {object} server.Problem
// @Failure 409 {object} server.Problem
// @Failure 503 {object} server.Problem
// @Router /users/me/image-workflows [post].
func (h *ImageWorkflowHandler) Create(c echo.Context) error {
	// TODO(F3): bind, validate, create, answer 201.
	return notImplemented(c)
}

// Get godoc
// @Summary Get one image workflow
// @Description The workflow plus its newest runs and per-state run counts.
// @Tags image-workflows
// @Produce json
// @Param workflow_id path string true "Workflow ID"
// @Success 200 {object} imageworkflow.WorkflowDetail
// @Failure 400 {object} server.Problem
// @Failure 404 {object} server.Problem
// @Failure 503 {object} server.Problem
// @Router /users/me/image-workflows/{workflow_id} [get].
func (h *ImageWorkflowHandler) Get(c echo.Context) error {
	// TODO(F3): read one workflow with its runs.
	return notImplemented(c)
}

// Update godoc
// @Summary Update an image workflow
// @Description Changes the name and/or the instruction. Pairing labels are not editable here.
// @Tags image-workflows
// @Accept json
// @Produce json
// @Param workflow_id path string true "Workflow ID"
// @Param request body imageworkflow.UpdateInput true "Fields to change"
// @Success 200 {object} imageworkflow.Workflow
// @Failure 400 {object} server.Problem
// @Failure 404 {object} server.Problem
// @Failure 409 {object} server.Problem
// @Failure 503 {object} server.Problem
// @Router /users/me/image-workflows/{workflow_id} [put].
func (h *ImageWorkflowHandler) Update(c echo.Context) error {
	// TODO(F3): bind, validate, update.
	return notImplemented(c)
}

// Revoke godoc
// @Summary Revoke an image workflow
// @Description Soft delete: the workflow and its pairing labels keep their row, both album pairings are revoked, and queued write-backs, runs and products stay untouched.
// @Tags image-workflows
// @Produce json
// @Param workflow_id path string true "Workflow ID"
// @Success 200 {object} imageworkflow.Workflow
// @Failure 400 {object} server.Problem
// @Failure 404 {object} server.Problem
// @Failure 503 {object} server.Problem
// @Router /users/me/image-workflows/{workflow_id} [delete].
func (h *ImageWorkflowHandler) Revoke(c echo.Context) error {
	// TODO(F3/F7): revoke the workflow and its two pairings in one transaction.
	return notImplemented(c)
}

// Pause godoc
// @Summary Pause an image workflow
// @Description Stops new triggers from arming and removes the directory route. Pending write-backs keep draining.
// @Tags image-workflows
// @Produce json
// @Param workflow_id path string true "Workflow ID"
// @Success 200 {object} imageworkflow.Workflow
// @Failure 400 {object} server.Problem
// @Failure 404 {object} server.Problem
// @Failure 409 {object} server.Problem
// @Failure 503 {object} server.Problem
// @Router /users/me/image-workflows/{workflow_id}/pause [post].
func (h *ImageWorkflowHandler) Pause(c echo.Context) error {
	// TODO(F7): pause, idempotently.
	return notImplemented(c)
}

// Resume godoc
// @Summary Resume an image workflow
// @Description Returns the workflow to active and arms every held run.
// @Tags image-workflows
// @Produce json
// @Param workflow_id path string true "Workflow ID"
// @Success 200 {object} imageworkflow.ResumeResult
// @Failure 400 {object} server.Problem
// @Failure 404 {object} server.Problem
// @Failure 409 {object} server.Problem
// @Failure 503 {object} server.Problem
// @Router /users/me/image-workflows/{workflow_id}/resume [post].
func (h *ImageWorkflowHandler) Resume(c echo.Context) error {
	// TODO(F7): arm every held run, then set the workflow active.
	return notImplemented(c)
}

// Trigger godoc
// @Summary Trigger one workflow batch
// @Description Accepts a batch of input files for this workflow. The workflow must be active with both album pairings designated; a paused workflow stores the batch as held. A repeated batch identity returns the original run.
// @Tags image-workflows
// @Accept json
// @Produce json
// @Param bot_id path string true "Bot ID"
// @Param workflow_id path string true "Workflow ID"
// @Param request body imageworkflow.TriggerRequest true "Batch source and files"
// @Success 202 {object} imageworkflow.TriggerResult
// @Failure 400 {object} server.Problem
// @Failure 404 {object} server.Problem
// @Failure 409 {object} server.Problem
// @Failure 503 {object} server.Problem
// @Router /bots/{bot_id}/image-workflows/{workflow_id}/trigger [post].
func (h *ImageWorkflowHandler) Trigger(c echo.Context) error {
	// TODO(F4/F5): authorize bot access, then trigger.
	return notImplemented(c)
}

// Routes godoc
// @Summary Read the image workflow route table
// @Description One entry per live workflow with the absolute input directory, so the directory watcher can route a batch to a workflow instead of the generic file batch.
// @Tags image-workflows
// @Produce json
// @Success 200 {object} imageworkflow.RoutesResponse
// @Failure 400 {object} server.Problem
// @Failure 503 {object} server.Problem
// @Router /internal/image-workflow-routes [get].
func (h *ImageWorkflowHandler) Routes(c echo.Context) error {
	// TODO(F9): list the caller's routes.
	return notImplemented(c)
}

// notImplemented is the stub answer of this test-first round: the route exists,
// nothing behind it does yet.
func notImplemented(echo.Context) error {
	return apperror.New(apperror.CodeHTTPNotImplemented, nil)
}

package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

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
type ImageWorkflowHandler struct {
	logger  *slog.Logger
	service *imageworkflow.Service
	// authorizeBot is the manage-access check the trigger route needs. The
	// other routes are owner-scoped by the caller's own session, exactly like
	// the album-pair surface.
	authorizeBot func(c echo.Context, userID, botID string) error
}

func NewImageWorkflowHandler(log *slog.Logger, service *imageworkflow.Service) *ImageWorkflowHandler {
	if log == nil {
		log = slog.Default()
	}
	return &ImageWorkflowHandler{logger: log.With(slog.String("handler", "image_workflow")), service: service}
}

// SetBotAuthorizer installs the Bot manage-access check the trigger route
// requires, the same authorization family as POST /bots/{bot_id}/schedule.
func (h *ImageWorkflowHandler) SetBotAuthorizer(authorize func(c echo.Context, userID, botID string) error) {
	h.authorizeBot = authorize
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

// configured answers 503 when the service was not wired, exactly like the
// album-pair surface: a deployment without a database must not answer archive
// writes it cannot keep.
func (h *ImageWorkflowHandler) configured() error {
	if h == nil || h.service == nil {
		return apperror.New(apperror.CodeHTTPServiceUnavailable, nil)
	}
	return nil
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
	if err := h.configured(); err != nil {
		return err
	}
	userID, err := RequireChannelIdentityID(c)
	if err != nil {
		return err
	}
	workflows, err := h.service.List(c.Request().Context(), userID)
	if err != nil {
		return imageWorkflowHTTPError(err)
	}
	return c.JSON(http.StatusOK, imageworkflow.ListResponse{Workflows: workflows})
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
	if err := h.configured(); err != nil {
		return err
	}
	userID, err := RequireChannelIdentityID(c)
	if err != nil {
		return err
	}
	var req imageworkflow.CreateInput
	if err := decodeStrict(c, &req); err != nil {
		return err
	}
	created, err := h.service.Create(c.Request().Context(), userID, req)
	if err != nil {
		return imageWorkflowHTTPError(err)
	}
	return c.JSON(http.StatusCreated, created)
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
	if err := h.configured(); err != nil {
		return err
	}
	userID, err := RequireChannelIdentityID(c)
	if err != nil {
		return err
	}
	detail, err := h.service.Get(c.Request().Context(), userID, c.Param("workflow_id"))
	if err != nil {
		return imageWorkflowHTTPError(err)
	}
	return c.JSON(http.StatusOK, detail)
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
	if err := h.configured(); err != nil {
		return err
	}
	userID, err := RequireChannelIdentityID(c)
	if err != nil {
		return err
	}
	var req imageworkflow.UpdateInput
	if err := decodeStrict(c, &req); err != nil {
		return err
	}
	updated, err := h.service.Update(c.Request().Context(), userID, c.Param("workflow_id"), req)
	if err != nil {
		return imageWorkflowHTTPError(err)
	}
	return c.JSON(http.StatusOK, updated)
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
	if err := h.configured(); err != nil {
		return err
	}
	userID, err := RequireChannelIdentityID(c)
	if err != nil {
		return err
	}
	revoked, err := h.service.Revoke(c.Request().Context(), userID, c.Param("workflow_id"))
	if err != nil {
		return imageWorkflowHTTPError(err)
	}
	return c.JSON(http.StatusOK, revoked)
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
	if err := h.configured(); err != nil {
		return err
	}
	userID, err := RequireChannelIdentityID(c)
	if err != nil {
		return err
	}
	paused, err := h.service.Pause(c.Request().Context(), userID, c.Param("workflow_id"))
	if err != nil {
		return imageWorkflowHTTPError(err)
	}
	return c.JSON(http.StatusOK, paused)
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
	if err := h.configured(); err != nil {
		return err
	}
	userID, err := RequireChannelIdentityID(c)
	if err != nil {
		return err
	}
	result, err := h.service.Resume(c.Request().Context(), userID, c.Param("workflow_id"))
	if err != nil {
		return imageWorkflowHTTPError(err)
	}
	return c.JSON(http.StatusOK, result)
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
	if err := h.configured(); err != nil {
		return err
	}
	userID, err := RequireChannelIdentityID(c)
	if err != nil {
		return err
	}
	botID := c.Param("bot_id")
	// The trigger is the one route that acts on a Bot rather than on the
	// caller's own archive, so it carries the same manage-access check as
	// POST /bots/{bot_id}/schedule.
	if h.authorizeBot != nil {
		if err := h.authorizeBot(c, userID, botID); err != nil {
			return err
		}
	}
	var req imageworkflow.TriggerRequest
	if err := decodeStrict(c, &req); err != nil {
		return err
	}
	result, err := h.service.Trigger(c.Request().Context(), userID, botID, c.Param("workflow_id"), req)
	if err != nil {
		return imageWorkflowHTTPError(err)
	}
	return c.JSON(http.StatusAccepted, result)
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
	if err := h.configured(); err != nil {
		return err
	}
	userID, err := RequireChannelIdentityID(c)
	if err != nil {
		return err
	}
	routes, err := h.service.Routes(c.Request().Context(), userID)
	if err != nil {
		return imageWorkflowHTTPError(err)
	}
	return c.JSON(http.StatusOK, imageworkflow.RoutesResponse{Routes: routes})
}

// decodeStrict binds one JSON body and refuses unknown fields, so a client that
// sends an album uri or a label to this surface learns it is not editable here
// instead of having it silently ignored.
func decodeStrict(c echo.Context, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(c.Response(), c.Request().Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return apperror.FieldInvalid("request", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return apperror.FieldInvalid("request", err)
	}
	return nil
}

// imageWorkflowHTTPError maps the package errors onto the contract's six codes.
func imageWorkflowHTTPError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, imageworkflow.ErrInvalid):
		return apperror.Wrap(apperror.CodeImageWorkflowRequestInvalid, err, nil)
	case errors.Is(err, imageworkflow.ErrNotFound):
		return apperror.Wrap(apperror.CodeImageWorkflowNotFound, err, nil)
	case errors.Is(err, imageworkflow.ErrNameTaken):
		return apperror.Wrap(apperror.CodeImageWorkflowNameTaken, err, nil)
	case errors.Is(err, imageworkflow.ErrPairUnavailable):
		return apperror.Wrap(apperror.CodeImageWorkflowPairUnavailable, err, nil)
	case errors.Is(err, imageworkflow.ErrCycle):
		return apperror.Wrap(apperror.CodeImageWorkflowCycleDetected, err, nil)
	case errors.Is(err, imageworkflow.ErrNotActive):
		return apperror.Wrap(apperror.CodeImageWorkflowNotActive, err, nil)
	default:
		return err
	}
}

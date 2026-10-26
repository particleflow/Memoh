package handlers

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/apperror"
	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/userruntime"
)

func workspaceStorageOwner(c echo.Context) (string, error) {
	if _, err := auth.ChatTokenFromContext(c); err == nil {
		return "", apperror.New(apperror.CodeHTTPForbidden, nil)
	}
	owner, err := RequireChannelIdentityID(c)
	if err != nil {
		return "", err
	}
	if _, err = uuid.Parse(strings.TrimSpace(c.Param("id"))); err != nil {
		return "", apperror.FieldInvalid("id", err)
	}
	return owner, nil
}

// WorkspaceDirectories godoc
// @Summary Browse this owned computer's NAS workspace directories
// @Description Account session required. Uses an existing configured private LAN share and existing Runtime identity. No directory creation, execution or credential changes.
// @Tags user-runtimes
// @Produce json
// @Param id path string true "Owned Runtime ID"
// @Param path query string false "Relative directory within this computer's NAS storage"
// @Success 200 {object} userruntime.WorkspaceStorage
// @Failure 400 {object} server.Problem
// @Failure 401 {object} server.Problem
// @Failure 403 {object} server.Problem
// @Failure 404 {object} server.Problem
// @Failure 503 {object} server.Problem
// @Router /users/me/runtimes/{id}/workspace-storage [get].
func (h *UserRuntimeHandler) WorkspaceDirectories(c echo.Context) error {
	owner, err := workspaceStorageOwner(c)
	if err != nil {
		return err
	}
	out, err := h.service.WorkspaceDirectories(c.Request().Context(), owner, strings.TrimSpace(c.Param("id")), c.QueryParam("path"))
	if err != nil {
		return runtimeHTTPError(err)
	}
	return c.JSON(http.StatusOK, out)
}

// CreateWorkspaceDirectory godoc
// @Summary Create a directory in this owned computer's NAS storage
// @Description Requires the existing private LAN share to be readable before any NAS write. Does not change selection, local defaults, tasks or sessions.
// @Tags user-runtimes
// @Accept json
// @Param id path string true "Owned Runtime ID"
// @Param request body userruntime.WorkspaceStorageRequest true "Relative path"
// @Success 204
// @Failure 400 {object} server.Problem
// @Failure 403 {object} server.Problem
// @Failure 404 {object} server.Problem
// @Failure 503 {object} server.Problem
// @Router /users/me/runtimes/{id}/workspace-storage/directories [post].
func (h *UserRuntimeHandler) CreateWorkspaceDirectory(c echo.Context) error {
	owner, err := workspaceStorageOwner(c)
	if err != nil {
		return err
	}
	var in userruntime.WorkspaceStorageRequest
	if err = c.Bind(&in); err != nil {
		return err
	}
	if err = h.service.CreateWorkspaceDirectory(c.Request().Context(), owner, strings.TrimSpace(c.Param("id")), in.Path); err != nil {
		return runtimeHTTPError(err)
	}
	return c.NoContent(http.StatusNoContent)
}

// SelectWorkspaceDirectory godoc
// @Summary Save this owned computer's default NAS storage directory
// @Description Persists only workspace_storage in the existing Runtime configuration with compare-and-swap. Creates the default folder if missing after LAN/share access validation. Does not rewrite Herdr WorkDir, existing tasks, sessions, local sourceRoot or credentials.
// @Tags user-runtimes
// @Accept json
// @Param id path string true "Owned Runtime ID"
// @Param request body userruntime.WorkspaceStorageRequest true "Relative selected directory"
// @Success 204
// @Failure 400 {object} server.Problem
// @Failure 403 {object} server.Problem
// @Failure 404 {object} server.Problem
// @Failure 409 {object} server.Problem
// @Failure 503 {object} server.Problem
// @Router /users/me/runtimes/{id}/workspace-storage [put].
func (h *UserRuntimeHandler) SelectWorkspaceDirectory(c echo.Context) error {
	owner, err := workspaceStorageOwner(c)
	if err != nil {
		return err
	}
	var in userruntime.WorkspaceStorageRequest
	if err = c.Bind(&in); err != nil {
		return err
	}
	if err = h.service.SelectWorkspaceDirectory(c.Request().Context(), owner, strings.TrimSpace(c.Param("id")), in.Path); err != nil {
		return runtimeHTTPError(err)
	}
	return c.NoContent(http.StatusNoContent)
}

package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"
)

// Client net-report intake. The mobile client posts a machine-format
// diagnostic snapshot whenever it enters a network failure state. The report
// is validated strictly and appended to the process log stream as one JSON
// line; there is no database table, query API or user-facing copy.

const (
	clientNetReportMaxBytes  = 64 << 10
	clientNetReportMaxEvents = 200
	clientNetReportMaxNets   = 16
)

type clientNetReportEvent struct {
	At        int64  `json:"at"`
	Area      string `json:"area"`
	Component string `json:"component"`
	Phase     string `json:"phase"`
	Detail    string `json:"detail"`
}

type clientNetReportNet struct {
	NetId    int  `json:"netId"`
	Wifi     bool `json:"wifi"`
	Cellular bool `json:"cellular"`
	Ipv6     bool `json:"ipv6"`
}

type clientNetReportBinding struct {
	Version  int                  `json:"version"`
	Started  bool                 `json:"started"`
	BoundNet int                  `json:"boundNetId"`
	Action   string               `json:"action"`
	Reason   string               `json:"reason"`
	Nets     []clientNetReportNet `json:"nets"`
}

type clientNetReport struct {
	Schema  int                     `json:"schema"`
	At      int64                   `json:"at"`
	Version string                  `json:"version"`
	Build   int64                   `json:"build"`
	Reason  string                  `json:"reason"`
	Binding *clientNetReportBinding `json:"binding"`
	Journal []clientNetReportEvent  `json:"journal"`
}

type ClientNetReportHandler struct {
	logger *slog.Logger
}

func NewClientNetReportHandler(log *slog.Logger) *ClientNetReportHandler {
	return &ClientNetReportHandler{logger: log.With(slog.String("handler", "client_net_report"))}
}

func (h *ClientNetReportHandler) Register(e *echo.Echo) {
	e.POST("/client/net-report", h.Report)
}

var clientNetReportReasons = map[string]bool{
	"client-failed": true, "page-failed": true, "client-timeout": true, "manual": true,
}

// Report godoc
// @Summary Ingest a client network failure report
// @Description Validates a machine-format diagnostic snapshot and appends it to the log stream. No UI, no storage beyond logs.
// @Tags system
// @Accept json
// @Success 204 {string} string
// @Failure 400 {object} server.Problem
// @Router /client/net-report [post].
func (h *ClientNetReportHandler) Report(c echo.Context) error {
	body, err := io.ReadAll(io.LimitReader(c.Request().Body, clientNetReportMaxBytes+1))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	if err := validateClientNetReport(body); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	// The strict-validated body is logged verbatim as one line; the log
	// stream is the only sink.
	h.logger.LogAttrs(c.Request().Context(), slog.LevelInfo, "client_net_report",
		slog.String("report", string(body)))
	return c.NoContent(http.StatusNoContent)
}

func validateClientNetReport(body []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var report clientNetReport
	if err := decoder.Decode(&report); err != nil {
		return err
	}
	if decoder.More() {
		return errors.New("trailing content")
	}
	if len(body) > clientNetReportMaxBytes {
		return errors.New("size")
	}
	if report.Schema != 1 || report.At <= 0 || !clientNetReportReasons[report.Reason] {
		return errors.New("header")
	}
	if report.Version == "" || len(report.Version) > 32 || report.Build < 0 {
		return errors.New("app")
	}
	if report.Binding != nil {
		if report.Binding.Version != 1 || report.Binding.BoundNet < 0 ||
			!clientReportToken(report.Binding.Action, 32) ||
			!clientReportToken(report.Binding.Reason, 64) ||
			len(report.Binding.Nets) > clientNetReportMaxNets {
			return errors.New("binding")
		}
		for _, net := range report.Binding.Nets {
			if net.NetId < 0 {
				return errors.New("binding net")
			}
		}
	}
	if len(report.Journal) > clientNetReportMaxEvents {
		return errors.New("journal size")
	}
	for _, event := range report.Journal {
		if event.At <= 0 || !clientNetReportAreas[event.Area] ||
			!clientReportToken(event.Component, 64) || !clientReportToken(event.Phase, 64) ||
			len(event.Detail) > 512 {
			return errors.New("journal event")
		}
	}
	return nil
}

var clientNetReportAreas = map[string]bool{"NETWORK": true, "SYSTEM": true}

func clientReportToken(value string, limit int) bool {
	if value == "" || len(value) > limit {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

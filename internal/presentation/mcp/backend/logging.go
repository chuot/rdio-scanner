// Copyright (C) 2019-2026 Chrystian Huot <chrystian.huot@saubeo.solutions>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>

package backend

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// This adapter files the surface's slog lines in the server log view the
// operator already reads: subsystem, severity, and attributes folded into
// the entry's text; see the surface-lines-land-in-the-operator-log-view
// maintainer note.

// NewLogViewHandler files the surface's lines under the surface's
// subsystem, so the assistant's work lands in the same view every other
// writer files into; see the surface-lines-land-in-the-operator-log-view
// maintainer note.
func NewLogViewHandler(logs ports.LogRepository, clock ports.Clock) slog.Handler {
	return &logViewHandler{logs: logs, clock: clock}
}

// logViewHandler is the `log/slog` handler that files into the server's log view.
type logViewHandler struct {
	logs  ports.LogRepository
	clock ports.Clock

	// attrs and group are what WithAttrs and WithGroup add to a handler: a
	// derived handler's attributes travel with every line it logs.
	attrs []slog.Attr
	group string
}

// Enabled reports the lines the log view keeps. The surface's own detail is
// debug, which an operator filtering the deployment does not keep.
func (handler *logViewHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelInfo
}

// Handle files one line as one entry: filed under the surface's
// subsystem, at the record's severity, at the server clock's instant, and
// with the attributes appended to the text; see the surface-lines-land-
// in-the-operator-log-view maintainer note.
func (handler *logViewHandler) Handle(_ context.Context, record slog.Record) error {
	if handler.logs == nil {
		return nil
	}

	message := record.Message

	for _, attr := range handler.attrs {
		message += " " + handler.named(attr) + "=" + attr.Value.String()
	}

	record.Attrs(func(attr slog.Attr) bool {
		message += " " + handler.named(attr) + "=" + attr.Value.String()
		return true
	})

	return handler.logs.AppendLog(context.Background(), domain.LogEntry{
		DateTime: handler.clock.Now(),
		Level:    logViewLevelOf(record.Level),
		Source:   domain.LogSourceMcp,
		Message:  message,
	})
}

// WithAttrs is the handler with attributes added to every line it files.
func (handler *logViewHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	derived := *handler
	derived.attrs = append(append([]slog.Attr{}, handler.attrs...), attrs...)

	return &derived
}

// WithGroup is the handler with a group name prefixed to every attribute it files.
func (handler *logViewHandler) WithGroup(name string) slog.Handler {
	derived := *handler

	if derived.group != "" {
		derived.group += "_"
	}

	derived.group += name

	return &derived
}

// named is an attribute as the log view spells it: a grouped attribute
// carries its group.
func (handler *logViewHandler) named(attr slog.Attr) string {
	if handler.group == "" {
		return attr.Key
	}

	return handler.group + attr.Key
}

// logViewLevelOf is the log view's severity for a log/slog level. The log
// view has three, and an operator filtering for errors is looking for the
// deployment's failures.
func logViewLevelOf(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return domain.LogLevelError

	case level >= slog.LevelWarn:
		return domain.LogLevelWarn

	default:
		return domain.LogLevelInfo
	}
}

// withActivityLog files what the operator asks about after the fact: a
// refused caller and a capability that changed the deployment. A
// capability that changed nothing files nothing; see the surface-lines-
// land-in-the-operator-log-view maintainer note.
func withActivityLog(log *slog.Logger, changes map[string]bool) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			answered, err := next(ctx, method, req)

			if method != methodCallTool {
				return answered, err
			}

			call, ok := req.(*mcp.CallToolRequest)
			if !ok || call.Params == nil {
				return answered, err
			}

			tool, who := call.Params.Name, callerDescription(ctx)

			if reason, refused := refusalOf(answered, err); refused {
				log.Warn("the surface refused a caller",
					"capability", tool, "caller", who, "refusal", reason)

				return answered, err
			}

			if changes[tool] && changedTheDeployment(answered) {
				log.Info("the surface changed the deployment",
					"capability", tool, "caller", who)
			}

			return answered, err
		}
	}
}

// callerDescription is who a filed line names its caller as: the identity a
// listener's code is filed under, never the administrator's session token,
// which is a credential.
func callerDescription(ctx context.Context) string {
	who, ok := CallerOf(ctx)
	if !ok {
		return "a caller the gate did not name"
	}

	switch {
	case who.Tier == TierFull:
		return "the administrator"

	case who.Ident != "":
		return who.Ident

	default:
		return "a caller with no credential"
	}
}

// refusalOf is the sentence a capability refused its caller with, and
// whether it refused at all. Refusals are the protocol's own tool-level
// failures, readable here in the shape the caller reads them; a capability
// this surface does not have is refused at the protocol's level.
func refusalOf(answered mcp.Result, err error) (string, bool) {
	if err != nil {
		return err.Error(), true
	}

	result, ok := answered.(*mcp.CallToolResult)
	if !ok || result == nil || !result.IsError {
		return "", false
	}

	text := []string{}

	for _, content := range result.Content {
		if line, ok := content.(*mcp.TextContent); ok {
			text = append(text, line.Text)
		}
	}

	return strings.Join(text, " "), true
}

// changedTheDeployment reports whether a capability's answer says it
// changed the deployment: the write outcomes the operator is told about.
func changedTheDeployment(answered mcp.Result) bool {
	result, ok := answered.(*mcp.CallToolResult)
	if !ok || result == nil || result.StructuredContent == nil {
		return false
	}

	body, err := json.Marshal(result.StructuredContent)
	if err != nil {
		return false
	}

	var outcome struct {
		Outcome string `json:"outcome"`
	}

	if err := json.Unmarshal(body, &outcome); err != nil {
		return false
	}

	return outcome.Outcome == outcomeSaved || outcome.Outcome == outcomeChanged
}

// The outcomes that mean the deployment is not what it was, the same
// facts the administrator's own page reports about a save.
const (
	outcomeSaved   = "saved"
	outcomeChanged = "changed"
)

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

// The surface's lines, in the log view the operator already reads.
//
// The surface speaks `log/slog` — that is what the protocol's software development kit logs with —
// and this server's log view is a repository of entries filed under a subsystem, a severity, and an
// instant. The adapter is the seam between the two, and it is the surface's own: the subsystem the
// lines are filed under is the surface's name, the severities are mapped onto the three the log view
// has, and the attributes a line carries travel into the entry's text, because the log view has one
// text and a line that loses its attributes loses the part of the story the operator needed.
//
// What the surface files is what an operator asks about on Monday: a caller it would not let in, a
// caller a capability refused, and a capability that changed the deployment. Reading a recorded call
// is not filed, the way the administrator's own page does not file a page read: the log view is where
// the deployment's decisions are recorded, and an assistant reading the history it was entitled to is
// not a decision.
//
// And nothing here carries a credential. The surface is built on credentials at every layer — the
// session token it recognises, the access code it resolves, the upload keys and the credential hash it
// reads out of the configuration, the credential an operator replaces — and a log view is read out
// loud, pasted into a ticket, and handed to a client. The surface names a caller by the identity its
// code is filed under, and by the address a request came from, and never by what the caller presented.

// NewLogViewHandler is the surface's log view: the lines the surface logs become entries in the
// server's log view, filed under the subsystem the surface is.
//
// The composition root hands this to the surface as its logger, and hands the same repository to
// every other writer, so the assistant's work lands in the view the operator filters by subsystem and
// echoes out loud the way every other line of this server does.
func NewLogViewHandler(logs ports.LogRepository, clock ports.Clock) slog.Handler {
	return &logViewHandler{logs: logs, clock: clock}
}

// logViewHandler is the `log/slog` handler that files into the server's log view.
type logViewHandler struct {
	logs  ports.LogRepository
	clock ports.Clock

	// attrs and group are what `WithAttrs` and `WithGroup` add to a handler: the attributes of a
	// derived handler travel with every line it logs, and a group names them the way the log view
	// spells a qualified attribute.
	attrs []slog.Attr
	group string
}

// Enabled reports the lines the log view keeps. The surface's own detail is debug, and the log view
// is an operator's view of the deployment: it keeps the severities an operator filters for.
func (handler *logViewHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelInfo
}

// Handle files one line as one entry.
//
// The entry is filed under the surface's subsystem, at the severity the line carries, at the instant
// the server's clock reports. The attributes are appended to the text: the log view has one text, and
// a line that dropped its attributes would say a caller was refused without saying by what.
//
// The write is not the request's business. A refusal is filed after the caller was answered, and the
// request's own context is over by then: a line tied to a canceled request is a line that never
// reaches the operator who needs it.
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

// named is an attribute as the log view spells it: a grouped attribute carries its group, the way
// the log view spells every qualified name this server writes.
func (handler *logViewHandler) named(attr slog.Attr) string {
	if handler.group == "" {
		return attr.Key
	}

	return handler.group + attr.Key
}

// logViewLevelOf is the log view's severity for a `log/slog` level. The log view has three severities
// and an operator filtering for errors is looking for the deployment's failures, so a surface that
// filed its failures as info would hide them from that filter.
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

// withActivityLog files what the operator asks about after the fact: a caller a capability refused,
// and a capability that changed the deployment.
//
// It is one place, on both capability sets, and it files from what the capability answered — the
// refusal the caller read, and the outcome the answer carries. A capability that changed nothing
// files nothing: a no-op is not an event in the deployment, and a log view full of nothing is how an
// operator stops reading it.
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

// callerDescription is who a filed line names its caller as. The identity a listener's code is filed
// under is the answer, and the administrator's session token is not: it is a credential, and a
// credential is not what a log view is for.
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

// refusalOf is the sentence a capability refused its caller with, and whether it refused at all. The
// surface's refusals are the protocol's own tool-level failures, so they are readable here in the same
// shape the caller reads them in. A caller that asked for a capability this surface does not have is
// refused at the protocol's level, and that refusal is the caller's doing and the surface's news too.
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

// changedTheDeployment reports whether a capability's answer says it changed the deployment. Every
// write capability answers an outcome, and the outcomes that mean a write happened are the ones the
// operator is told about.
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

// The outcomes that mean the deployment is not what it was. The write capabilities answer these, and
// the administrator's own page reports the same facts about a save.
const (
	outcomeSaved   = "saved"
	outcomeChanged = "changed"
)

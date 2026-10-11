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
//
// WebSocket API Access Policy:
// This WebSocket API is reserved exclusively for Saubeo Solutions and its native applications.
// Unauthorized access is strictly prohibited.
// See API_ACCESS_POLICY.md for full terms.

package downstream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// uploadPath is the endpoint every Rdio Scanner server accepts a call upload on. The
// downstream's configured address is a server address, not an endpoint address: the
// legacy server appends this path to whatever the administrator typed.
const uploadPath = "/api/call-upload"

// requestTimeout is how long the legacy server waits for a downstream to take a call, and
// it is the reason a deployment with an unreachable downstream stalls its forwarding for
// that long rather than giving up on it.
const requestTimeout = 30 * time.Second

// Forwarder re-posts a recorded call to another Rdio Scanner server, over the same
// call-upload endpoint an external application uploads to.
//
// The group and tag repositories are read for the two fields the wire carries as labels
// rather than as identifiers — the talkgroup's group names and its tag name — which is how
// the legacy resolves them too, from the configuration in effect rather than from anything
// the call itself carries.
type Forwarder struct {
	groups ports.GroupRepository
	tags   ports.TagRepository
	client *http.Client
}

// NewForwarder builds the forwarder for a server that re-posts its calls to other servers.
//
// The client does not follow redirects (decision D9): a downstream that answers 3xx
// points the upload at an address the administrator did not name, and following it
// would re-post the recording and the apikey there. The redirect is reported as the
// failure it is.
func NewForwarder(groups ports.GroupRepository, tags ports.TagRepository) *Forwarder {
	return &Forwarder{
		groups: groups,
		tags:   tags,
		client: &http.Client{
			Timeout: requestTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// Forward posts one call to one downstream server and reports what that server said.
//
// Only a 200 is a success. Anything else is reported to the caller, which is what the
// legacy does with it: a line in the server log naming the system, talkgroup, file, and
// the downstream that refused it.
func (forwarder *Forwarder) Forward(ctx context.Context, target ports.DownstreamTarget, call domain.Call) error {
	body, contentType, err := forwarder.uploadBody(ctx, target, call)
	if err != nil {
		return operationError(err)
	}

	// The legacy parses the configured address and appends the upload path to whatever
	// path it already had, so an administrator who typed the full endpoint address gets
	// the path twice. That is reproduced rather than tidied.
	endpoint, err := url.Parse(target.URL)
	if err != nil {
		return operationError(err)
	}

	// The scheme is re-checked here (decision D9, CA-009.1): an address stored by an
	// older server — before the apply-time rule — is read by this forwarder, and it is
	// posted to only over http or https.
	if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		return operationError(fmt.Errorf("the downstream address %s must be an http or https address", target.URL))
	}

	endpoint.Path = path.Join(endpoint.Path, uploadPath)

	// The legacy builds a fresh client per send and hands it no context, so a shutdown
	// cannot interrupt a send in flight. The context is honored here instead; the timeout
	// the legacy hard-codes is kept.
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), body)
	if err != nil {
		return operationError(err)
	}

	request.Header.Set("Content-Type", contentType)

	response, err := forwarder.client.Do(request)
	if err != nil {
		return operationError(err)
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		// The answer is read out before the connection is handed back (CA-009.3): a
		// body left unread makes the connection unusable, and every later call to the
		// same downstream pays for a fresh one.
		io.Copy(io.Discard, response.Body)

		return operationError(fmt.Errorf("bad status: %s", response.Status))
	}

	return nil
}

// uploadBody builds the multipart upload the receiving server parses, in the order the
// legacy writes the parts.
//
// The order is not what the receiver reads by — it reads parts by name — but it is what a
// byte-for-byte comparison against the legacy server sees, so it is kept.
func (forwarder *Forwarder) uploadBody(ctx context.Context, target ports.DownstreamTarget, call domain.Call) (io.Reader, string, error) {
	buf := bytes.Buffer{}

	writer := multipart.NewWriter(&buf)

	if err := forwarder.writeAudio(writer, call); err != nil {
		return nil, "", err
	}

	if err := writeField(writer, "audioFilename", call.AudioFilename); err != nil {
		return nil, "", err
	}

	if err := writeField(writer, "audioMime", call.AudioMime); err != nil {
		return nil, "", err
	}

	// The pre-v7 name for the call instant, kept because a downstream that has not been
	// upgraded reads it.
	if err := writeField(writer, "dateTime", call.Timestamp.Format(time.RFC3339)); err != nil {
		return nil, "", err
	}

	if err := writeJSONField(writer, "frequencies", frequenciesOf(call)); err != nil {
		return nil, "", err
	}

	if err := writeField(writer, "key", target.Apikey); err != nil {
		return nil, "", err
	}

	if err := writeJSONField(writer, "patches", call.PatchTalkgroupRefs); err != nil {
		return nil, "", err
	}

	if err := writeField(writer, "system", fmt.Sprintf("%v", call.System.SystemRef)); err != nil {
		return nil, "", err
	}

	if err := writeField(writer, "systemLabel", call.System.Label); err != nil {
		return nil, "", err
	}

	if err := writeField(writer, "talkgroup", fmt.Sprintf("%v", call.Talkgroup.TalkgroupRef)); err != nil {
		return nil, "", err
	}

	groups, err := forwarder.groupLabels(ctx, call)
	if err != nil {
		return nil, "", err
	}

	if err := writeField(writer, "talkgroupGroups", groups); err != nil {
		return nil, "", err
	}

	if err := writeField(writer, "talkgroupLabel", call.Talkgroup.Label); err != nil {
		return nil, "", err
	}

	if err := writeField(writer, "talkgroupName", call.Talkgroup.Name); err != nil {
		return nil, "", err
	}

	if err := forwarder.writeTagLabel(ctx, writer, call); err != nil {
		return nil, "", err
	}

	if err := writeField(writer, "timestamp", fmt.Sprintf("%d", call.Timestamp.UnixMilli())); err != nil {
		return nil, "", err
	}

	if err := writeJSONField(writer, "units", unitsOf(call)); err != nil {
		return nil, "", err
	}

	if err := writer.Close(); err != nil {
		return nil, "", err
	}

	return &buf, writer.FormDataContentType(), nil
}

// writeAudio writes the recording itself, under the part name the upload endpoint reads it
// from and under the filename the call carries.
//
// The filename is written through the standard library's `CreateFormFile`, and what the
// receiver sees is the standard library's escaping of it, not the call's own string: the
// library strips any path from the name and escapes the characters a quoted
// `Content-Disposition` cannot carry (Go's `escapeFilename` and `escapeQuotes` in
// `mime/multipart/writer.go`). A filename with a `"` in it arrives at the downstream
// quoted, not ending the header. This is pinned here, not hidden, because it is a
// property of the wire format the server does not own and must not pretend to control
// (CA-020.1): the legacy server forwarded through this same library, so the escaping
// the two servers put on the wire is byte-identical, and a receiver that parses the
// header sees the same name from either.
func (forwarder *Forwarder) writeAudio(writer *multipart.Writer, call domain.Call) error {
	part, err := writer.CreateFormFile("audio", call.AudioFilename)
	if err != nil {
		return err
	}

	if _, err := part.Write(call.Audio); err != nil {
		return err
	}

	return nil
}

// writeTagLabel writes the talkgroup's tag label. The part is created whether or not the
// tag is configured, which is what the legacy does: a talkgroup whose tag has been deleted
// is forwarded with an empty tag name rather than without the field.
func (forwarder *Forwarder) writeTagLabel(ctx context.Context, writer *multipart.Writer, call domain.Call) error {
	part, err := writer.CreateFormField("talkgroupTag")
	if err != nil {
		return err
	}

	tags, err := forwarder.tags.AllTags(ctx)
	if err != nil {
		return err
	}

	tag, found := tags.ByID(call.Talkgroup.TagID)
	if !found {
		return nil
	}

	_, err = part.Write([]byte(tag.Label))

	return err
}

// groupLabels are the talkgroup's group names, in the order the talkgroup lists them,
// joined the way the upload endpoint splits them. A group the configuration no longer has
// is left out, as the legacy leaves it out.
func (forwarder *Forwarder) groupLabels(ctx context.Context, call domain.Call) (string, error) {
	groups, err := forwarder.groups.AllGroups(ctx)
	if err != nil {
		return "", err
	}

	labels := []string{}

	for _, id := range call.Talkgroup.GroupIDs {
		group, found := groups.ByID(id)
		if !found {
			continue
		}

		labels = append(labels, group.Label)
	}

	return strings.Join(labels, ","), nil
}

// frequenciesOf is the frequency list as the legacy marshals it: the Go field names of its
// own struct, not the wire names the upload endpoint reads.
//
// That is a defect, and it is reproduced rather than repaired. A call forwarded to
// another Rdio Scanner server arrives with its frequencies and units named in a vocabulary
// the receiving parser does not look for, so the quality measurements and the units heard
// during the call are dropped on arrival. Repairing it changes what a receiving server
// stores, which is the owner's call rather than this port's.
func frequenciesOf(call domain.Call) any {
	out := []map[string]any{}

	for _, frequency := range call.Frequencies {
		out = append(out, map[string]any{
			"Id":        frequency.ID,
			"CallId":    frequency.CallID,
			"Dbm":       frequency.Dbm,
			"Errors":    frequency.Errors,
			"Frequency": frequency.Frequency,
			"Offset":    frequency.Offset,
			"Spikes":    frequency.Spikes,
		})
	}

	return out
}

// unitsOf is the unit list as the legacy marshals it, for the same reason and with the same
// consequence as the frequency list.
func unitsOf(call domain.Call) any {
	out := []map[string]any{}

	for _, unit := range call.Units {
		out = append(out, map[string]any{
			"Id":      unit.ID,
			"CallId":  unit.CallID,
			"Offset":  unit.Offset,
			"UnitRef": unit.UnitRef,
		})
	}

	return out
}

// writeField writes one form field.
func writeField(writer *multipart.Writer, name, value string) error {
	part, err := writer.CreateFormField(name)
	if err != nil {
		return err
	}

	_, err = part.Write([]byte(value))

	return err
}

// writeJSONField writes one form field whose value is a JSON document.
func writeJSONField(writer *multipart.Writer, name string, value any) error {
	part, err := writer.CreateFormField(name)
	if err != nil {
		return err
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}

	_, err = part.Write(encoded)

	return err
}

// operationError keeps the shape the legacy reports a forwarding failure in, so the log
// line a deployment already greps for still names the same thing.
func operationError(err error) error {
	return fmt.Errorf("downstream.send: %w", err)
}

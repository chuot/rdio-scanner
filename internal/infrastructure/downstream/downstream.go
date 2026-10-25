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
	"strconv"
	"strings"
	"time"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// uploadPath is the endpoint every Rdio Scanner server accepts a call
// upload on; see the downstream-forwarding maintainer note.
const uploadPath = "/api/call-upload"

// requestTimeout is how long the legacy server waits for a downstream to
// take a call; see the downstream-forwarding maintainer note.
const requestTimeout = 30 * time.Second

// Forwarder re-posts a recorded call to another Rdio Scanner server,
// over the same call-upload endpoint an external application uploads
// to. The group and tag repositories are read for the two fields the
// wire carries as labels rather than as identifiers; see the
// downstream-forwarding maintainer note.
type Forwarder struct {
	groups ports.GroupRepository
	tags   ports.TagRepository
	client *http.Client
}

// NewForwarder builds the forwarder for a server that re-posts its calls
// to other servers; see the downstream-forwarding maintainer note.
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

// Forward posts one call to one downstream server and reports what that
// server said; see the downstream-forwarding maintainer note.
func (forwarder *Forwarder) Forward(ctx context.Context, target ports.DownstreamTarget, call domain.Call) error {
	body, contentType, err := forwarder.uploadBody(ctx, target, call)
	if err != nil {
		return operationError(err)
	}

	// The legacy's address parsing is reproduced rather than tidied: the
	// upload path is appended to whatever path the address already has;
	// see the downstream-forwarding maintainer note.
	endpoint, err := url.Parse(target.URL)
	if err != nil {
		return operationError(err)
	}

	// The scheme is re-checked here, and an address is posted to only over
	// http or https; see the downstream-forwarding maintainer note.
	if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		return operationError(fmt.Errorf("the downstream address %s must be an http or https address", target.URL))
	}

	endpoint.Path = path.Join(endpoint.Path, uploadPath)

	// The legacy's fresh per-send client with no context is not reproduced:
	// the context is honored here, and the legacy's hard-coded timeout is
	// kept; see the downstream-forwarding maintainer note.
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
		// The answer is read out before the connection is handed back; see the
		// downstream-forwarding maintainer note.
		io.Copy(io.Discard, response.Body)

		return operationError(fmt.Errorf("bad status: %s", response.Status))
	}

	return nil
}

// uploadBody builds the multipart upload the receiving server parses,
// in the order the legacy writes the parts; see the downstream-forwarding
// maintainer note.
func (forwarder *Forwarder) uploadBody(ctx context.Context, target ports.DownstreamTarget, call domain.Call) (io.Reader, string, error) {
	buf := bytes.Buffer{}

	writer := multipart.NewWriter(&buf)

	if err := forwarder.writeAudio(writer, call); err != nil {
		return nil, "", err
	}

	if err := writeField(writer, "audioFilename", call.AudioFilename); err != nil {
		return nil, "", err
	}

	// The length of the recording, in seconds to the millisecond, and only
	// when some source named it; see the downstream-forwarding maintainer
	// note.
	if call.AudioLength > 0 {
		if err := writeField(writer, "audioLength", audioLengthText(call.AudioLength)); err != nil {
			return nil, "", err
		}
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

// writeAudio writes the recording itself, under the part name the upload
// endpoint reads it from and under the filename the call carries. The
// filename goes over through the standard library's form writer, so the
// receiver sees the library's escaping of it rather than the call's own
// string; see the downstream-forwarding maintainer note.
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

// writeTagLabel writes the talkgroup's tag label. The part is created
// whether or not the tag is configured; see the downstream-forwarding
// maintainer note.
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

// groupLabels are the talkgroup's group names, in the order the
// talkgroup lists them, joined the way the upload endpoint splits them;
// see the downstream-forwarding maintainer note.
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

// frequenciesOf is the frequency list as the legacy marshals it: the Go
// field names of its own struct, not the wire names the upload endpoint
// reads. That is a defect, reproduced rather than repaired; see the
// downstream-forwarding maintainer note.
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

// audioLengthText writes a recording's length as the count of seconds it
// is, to the millisecond, 3.500 for three and a half seconds, the same
// spelling the `audioLength` upload field names on the way in; see the
// downstream-forwarding maintainer note.
func audioLengthText(length time.Duration) string {
	return strconv.FormatFloat(float64(length)/float64(time.Second), 'f', 3, 64)
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

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

package main

import (
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"time"
)

// maxUploadSize caps each /api/call-upload (and TR variant) request body.
// A 30-second AAC clip is ~120 KB; even raw WAV is well under 32 MB. The
// limit exists to prevent pre-auth memory exhaustion via an unbounded
// multipart body — previously the server happily io.ReadAll'd whatever
// the client sent before the API key was checked.
const maxUploadSize = 32 << 20 // 32 MiB

// maxUploadsPerMinute throttles uploads per remote address. A legitimate
// recorder feeding one talkgroup at full duty cycle generates ~2 calls/sec
// (= 120/min). The threshold leaves headroom for bursts while bounding
// per-IP cost.
const maxUploadsPerMinute = 600

type Api struct {
	Controller *Controller

	uploadMu       sync.Mutex
	uploadAttempts map[string]*uploadAttempt
}

type uploadAttempt struct {
	count       uint
	windowStart time.Time
}

func NewApi(controller *Controller) *Api {
	return &Api{
		Controller:     controller,
		uploadAttempts: map[string]*uploadAttempt{},
	}
}

// allowUpload enforces a sliding 60-second per-IP upload counter and
// returns false if the request should be rejected.
func (api *Api) allowUpload(remoteAddr string) bool {
	const window = time.Minute

	api.uploadMu.Lock()
	defer api.uploadMu.Unlock()

	now := time.Now()
	a := api.uploadAttempts[remoteAddr]
	if a == nil || now.Sub(a.windowStart) > window {
		a = &uploadAttempt{count: 0, windowStart: now}
		api.uploadAttempts[remoteAddr] = a
	}
	a.count++

	// Opportunistic prune of stale entries to bound map size under churn.
	for ip, v := range api.uploadAttempts {
		if now.Sub(v.windowStart) > window {
			delete(api.uploadAttempts, ip)
		}
	}

	return a.count <= maxUploadsPerMinute
}

// extractApikey returns the upload key from the Authorization: Bearer
// header if present, otherwise empty. Multipart form-field fallback is
// still honored by the per-handler parsing loops below.
func extractBearerKey(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(auth) > len(prefix) && strings.EqualFold(auth[:len(prefix)], prefix) {
		return strings.TrimSpace(auth[len(prefix):])
	}
	return ""
}

func (api *Api) CallUploadHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		remoteAddr := GetRemoteAddr(r)
		if !api.allowUpload(remoteAddr) {
			api.exitWithError(w, http.StatusTooManyRequests, "Upload rate limit exceeded")
			return
		}

		// Cap the request body before any read happens, so an unbounded
		// multipart cannot OOM the server pre-auth.
		r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

		var (
			call = NewCall()
			key  = extractBearerKey(r)
		)

		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			api.exitWithError(w, http.StatusBadRequest, "Invalid content-type")
			return
		}

		if !strings.HasPrefix(mediaType, "multipart/") {
			api.exitWithError(w, http.StatusBadRequest, "Not a multipart content")
			return
		}

		mr := multipart.NewReader(r.Body, params["boundary"])

		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			} else if err != nil {
				api.exitWithError(w, http.StatusExpectationFailed, fmt.Sprintf("multipart: %s\n", err.Error()))
				return
			}

			b, err := io.ReadAll(p)
			if err != nil {
				api.exitWithError(w, http.StatusExpectationFailed, fmt.Sprintf("ioread: %s\n", err.Error()))
				return
			}

			switch p.FormName() {
			case "key":
				key = string(b)
			default:
				ParseMultipartContent(call, p, b)
			}
		}

		if ok, err := call.IsValid(); ok {
			api.HandleCall(key, call, w)
		} else {
			api.exitWithError(w, http.StatusExpectationFailed, fmt.Sprintf("Incomplete call data: %s\n", err.Error()))
		}

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte("Unsupported method\n"))
	}
}

func (api *Api) HandleCall(key string, call *Call, w http.ResponseWriter) {
	// Don't echo the resolved system/talkgroup back to an unauthenticated
	// caller — that lets an attacker enumerate which (system, talkgroup)
	// pairs exist by submitting empty bodies with various headers.
	msg := []byte("Unauthorized.\n")

	if apikey, ok := api.Controller.Apikeys.GetApikey(api.Controller.Options.secret, key); ok {
		if apikey.HasAccess(call) {
			api.Controller.Ingest <- call

		} else {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write(msg)
			return
		}

	} else {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write(msg)
		return
	}

	w.Write([]byte("Call imported successfully.\n"))
}

func (api *Api) TrunkRecorderCallUploadHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		remoteAddr := GetRemoteAddr(r)
		if !api.allowUpload(remoteAddr) {
			api.exitWithError(w, http.StatusTooManyRequests, "Upload rate limit exceeded")
			return
		}

		// Cap the request body before any read happens.
		r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

		var (
			call = NewCall()
			key  = extractBearerKey(r)
		)

		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			api.exitWithError(w, http.StatusBadRequest, "Invalid content-type")
			return
		}

		if !strings.HasPrefix(mediaType, "multipart/") {
			api.exitWithError(w, http.StatusBadRequest, "Not a multipart content")
			return
		}

		mr := multipart.NewReader(r.Body, params["boundary"])

		parts := map[*multipart.Part][]byte{}

		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			} else if err != nil {
				api.exitWithError(w, http.StatusExpectationFailed, fmt.Sprintf("multipart: %s", err.Error()))
				return
			}

			b, err := io.ReadAll(p)
			if err != nil {
				api.exitWithError(w, http.StatusExpectationFailed, fmt.Sprintf("ioread: %s", err.Error()))
				return
			}

			switch p.FormName() {
			case "key":
				key = string(b)
			case "meta":
				if err := ParseTrunkRecorderMeta(call, b); err != nil {
					api.exitWithError(w, http.StatusExpectationFailed, "Invalid call data")
					return
				}
			default:
				parts[p] = b
			}
		}

		for p, b := range parts {
			ParseMultipartContent(call, p, b)
		}

		if ok, err := call.IsValid(); ok {
			api.HandleCall(key, call, w)

		} else {
			api.exitWithError(w, http.StatusExpectationFailed, fmt.Sprintf("Incomplete call data: %s\n", err.Error()))
		}

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte("Unsupported method\n"))
	}
}

func (api *Api) exitWithError(w http.ResponseWriter, status int, message string) {
	api.Controller.Logs.LogEvent(LogLevelError, fmt.Sprintf("api: %s", message))

	w.WriteHeader(status)
	w.Write([]byte(fmt.Sprintf("%s\n", message)))
}

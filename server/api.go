// Copyright (C) 2019-2022 Chrystian Huot <chrystian.huot@saubeo.solutions>
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

package main

import (
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
)

const (
	ApiUrlCallUpload              = "api/call-upload"
	ApiUrlTrunkRecorderCallUpload = "api/trunk-recorder-call-upload"
)

type Api struct {
	Controller *Controller
}

func NewApi(controller *Controller) *Api {
	return &Api{Controller: controller}
}

func (api *Api) CallUploadHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var (
			call = NewCall()
			key  string
		)

		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Invalid content-type\n"))
			return
		}

		if !strings.HasPrefix(mediaType, "multipart/") {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Not a multipart content\n"))
			return
		}

		mr := multipart.NewReader(r.Body, params["boundary"])

		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			} else if err != nil {
				continue
			}

			b, err := io.ReadAll(p)
			if err != nil {
				continue
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
			w.WriteHeader(http.StatusExpectationFailed)
			w.Write([]byte(fmt.Sprintf("Incomplete call data: %s\n", err.Error())))
		}

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte("Unsupported method\n"))
	}
}

func (api *Api) HandleCall(key string, call *Call, w http.ResponseWriter) {
	msg := []byte(fmt.Sprintf("Invalid API key for system %v talkgroup %v.\n", call.System, call.Talkgroup))

	if apikey, ok := api.Controller.Apikeys.GetApikey(key); ok {
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
		var (
			call = NewCall()
			key  string
		)

		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Invalid content-type\n"))
			return
		}

		if !strings.HasPrefix(mediaType, "multipart/") {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Not a multipart content\n"))
			return
		}

		mr := multipart.NewReader(r.Body, params["boundary"])

		parts := map[*multipart.Part][]byte{}

		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			} else if err != nil {
				continue
			}

			b, err := io.ReadAll(p)
			if err != nil {
				continue
			}

			switch p.FormName() {
			case "key":
				key = string(b)
			case "meta":
				if err := ParseTrunkRecorderMeta(call, b); err != nil {
					w.WriteHeader(http.StatusExpectationFailed)
					w.Write([]byte("Invalid call data\n"))
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
			w.WriteHeader(http.StatusExpectationFailed)
			w.Write([]byte(fmt.Sprintf("Incomplete call data: %s\n", err.Error())))
		}

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte("Unsupported method\n"))
	}
}

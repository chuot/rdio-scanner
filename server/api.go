// Copyright (C) 2019-2021 Chrystian Huot <chrystian.huot@saubeo.solutions>
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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	ApiUrlCallUpload              = "api/call-upload"
	ApiUrlTrunkRecorderCallUpload = "api/trunk-recorder-call-upload"
)

type Api struct {
	initialized bool
	Controller  *Controller
}

func (api *Api) Init(controller *Controller) error {
	if api.initialized {
		return errors.New("api already initialized")
	}

	api.Controller = controller

	return nil
}

func (api *Api) CallUploadHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var (
			audio          []byte
			audioName      string
			audioType      string
			dateTime       time.Time
			frequencies    = []map[string]interface{}{}
			frequency      interface{}
			key            string
			source         interface{}
			sources        = []map[string]interface{}{}
			system         uint
			systemLabel    interface{}
			talkgroup      uint
			talkgroupGroup interface{}
			talkgroupLabel interface{}
			talkgroupTag   interface{}
			units          interface{}
		)

		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Invalid content-type"))
			return
		}

		if !strings.HasPrefix(mediaType, "multipart/") {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Not a multipart content"))
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
			case "audio":
				audio = b
				audioName = p.FileName()
				audioType = p.Header.Get("Content-Type")

			case "audioName":
				audioName = string(b)

			case "audioType":
				audioType = string(b)

			case "dateTime":
				if regexp.MustCompile(`^[0-9]+$`).Match(b) {
					if i, err := strconv.Atoi(string(b)); err == nil {
						dateTime = time.Unix(int64(i), 0).UTC()
					}

				} else {
					dateTime, _ = time.Parse(time.RFC3339, string(b))
					dateTime = dateTime.UTC()
				}

			case "frequencies":
				var f interface{}
				if err := json.Unmarshal(b, &f); err == nil {
					switch v := f.(type) {
					case []interface{}:
						for _, f := range v {
							freq := map[string]interface{}{}

							switch v := f.(type) {
							case map[string]interface{}:
								switch v := v["errorCount"].(type) {
								case float64:
									freq["errorCount"] = uint(v)
								}

								switch v := v["freq"].(type) {
								case float64:
									freq["freq"] = uint(v)
								}

								switch v := v["len"].(type) {
								case float64:
									freq["len"] = uint(v)
								}

								switch v := v["pos"].(type) {
								case float64:
									freq["pos"] = uint(v)
								}

								switch v := v["spikeCount"].(type) {
								case float64:
									freq["spikeCount"] = uint(v)
								}
							}

							frequencies = append(frequencies, freq)
						}
					}
				}

			case "frequency":
				if i, err := strconv.Atoi(string(b)); err == nil {
					frequency = uint(i)
				}

			case "key":
				key = string(b)

			case "source":
				if i, err := strconv.Atoi(string(b)); err == nil {
					source = uint(i)
				}

			case "sources":
				var f interface{}
				if err := json.Unmarshal(b, &f); err == nil {
					switch v := f.(type) {
					case []interface{}:
						for _, f := range v {
							src := map[string]interface{}{}

							switch v := f.(type) {
							case map[string]interface{}:
								switch v := v["pos"].(type) {
								case float64:
									src["pos"] = uint(v)
								}

								switch s := v["src"].(type) {
								case float64:
									src["src"] = uint(s)

									switch t := v["tag"].(type) {
									case string:
										var u Units
										switch v := units.(type) {
										case Units:
											u = v
										default:
											u = Units{}
										}
										u.Add(uint(s), t)
										units = u
									}
								}
							}

							sources = append(sources, src)
						}
					}
				}

			case "system", "systemId":
				if i, err := strconv.Atoi(string(b)); err == nil {
					system = uint(i)
				}

			case "systemLabel":
				systemLabel = string(b)

			case "talkgroup", "talkgroupId":
				if i, err := strconv.Atoi(string(b)); err == nil {
					talkgroup = uint(i)
				}

			case "talkgroupGroup":
				if s := string(b); len(s) > 1 {
					talkgroupGroup = s
				}

			case "talkgroupLabel":
				if s := string(b); len(s) > 1 {
					talkgroupLabel = s
				}

			case "talkgroupTag":
				if s := string(b); len(s) > 1 {
					talkgroupTag = s
				}
			}
		}

		call := &Call{
			Audio:          audio,
			AudioName:      audioName,
			AudioType:      audioType,
			DateTime:       dateTime,
			Frequencies:    frequencies,
			Frequency:      frequency,
			Source:         source,
			Sources:        sources,
			System:         system,
			Talkgroup:      talkgroup,
			systemLabel:    systemLabel,
			talkgroupGroup: talkgroupGroup,
			talkgroupLabel: talkgroupLabel,
			talkgroupTag:   talkgroupTag,
			units:          units,
		}

		if call.IsValid() {
			api.HandleCall(key, call, w)

		} else {
			w.WriteHeader(http.StatusExpectationFailed)
			w.Write([]byte("Incomplete call data"))
		}

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte("Unsupported method"))
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
			audio     []byte
			audioName string
			audioType string
			key       string
			meta      []byte
			system    uint
		)

		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Invalid content-type"))
			return
		}

		if !strings.HasPrefix(mediaType, "multipart/") {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Not a multipart content"))
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
			case "audio":
				audio = b
				audioName = p.FileName()
				audioType = p.Header.Get("Content-Type")

			case "key":
				key = string(b)

			case "meta":
				meta = b

			case "system":
				if i, err := strconv.Atoi(string(b)); err == nil {
					system = uint(i)
				}
			}
		}

		call := &Call{
			Audio:     audio,
			AudioName: audioName,
			AudioType: audioType,
			System:    system,
		}

		if err := ParseTrunkRecorderMeta(call, meta); err != nil {
			w.WriteHeader(http.StatusExpectationFailed)
			w.Write([]byte("Invalid call data"))
			return
		}

		if call.IsValid() {
			api.HandleCall(key, call, w)

		} else {
			w.WriteHeader(http.StatusExpectationFailed)
			w.Write([]byte("Incomplete call data"))
		}

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte("Unsupported method"))
	}
}

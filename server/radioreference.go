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

package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	radioReferenceEndpoint = "https://api.radioreference.com/soap2/"
	radioReferenceVersion  = "15"
	radioReferenceMaxBody  = 25 * 1024 * 1024
	radioReferenceTimeout  = 60 * time.Second
)

// RadioReferenceCredentials carries the auth bundle used by the SOAP API.
// The appKey is required by Radio Reference to identify the calling app and
// must be obtained by registering an account on radioreference.com.
type RadioReferenceCredentials struct {
	Username string
	Password string
	AppKey   string
}

// RadioReferenceTalkgroup is a normalized talkgroup record produced by the
// importer; tag and group come back as resolved labels rather than the
// numeric ids returned over the wire.
type RadioReferenceTalkgroup struct {
	Dec      uint
	Hex      string
	Alpha    string
	Mode     string
	Descr    string
	Tag      string
	Category string
}

type rrEnvelope struct {
	XMLName xml.Name `xml:"Envelope"`
	Body    rrBody   `xml:"Body"`
}

type rrBody struct {
	Fault                  *rrFault                  `xml:"Fault"`
	GetTalkgroupsResp      *rrGetTalkgroupsResp      `xml:"getTalkgroupsResponse"`
	GetTalkgroupCatsResp   *rrGetTalkgroupCatsResp   `xml:"getTalkgroupCatsResponse"`
	GetTalkgroupTagsResp   *rrGetTalkgroupTagsResp   `xml:"getTalkgroupTagsResponse"`
}

type rrFault struct {
	Code   string `xml:"faultcode"`
	String string `xml:"faultstring"`
}

type rrGetTalkgroupsResp struct {
	Return rrTalkgroupsReturn `xml:"return"`
}

type rrTalkgroupsReturn struct {
	Items []rrTalkgroupData `xml:"item"`
}

type rrTalkgroupData struct {
	TgDec   uint   `xml:"tgDec"`
	TgHex   string `xml:"tgHex"`
	TgAlpha string `xml:"tgAlpha"`
	TgMode  string `xml:"tgMode"`
	TgDescr string `xml:"tgDescr"`
	TgTag   uint   `xml:"tgTag"`
	TgCat   uint   `xml:"tgCat"`
}

type rrGetTalkgroupCatsResp struct {
	Return rrCatsReturn `xml:"return"`
}

type rrCatsReturn struct {
	Items []rrCatData `xml:"item"`
}

type rrCatData struct {
	TgCid   uint   `xml:"tgCid"`
	TgCname string `xml:"tgCname"`
}

type rrGetTalkgroupTagsResp struct {
	Return rrTagsReturn `xml:"return"`
}

type rrTagsReturn struct {
	Items []rrTagData `xml:"item"`
}

type rrTagData struct {
	TgTid    uint   `xml:"tgTid"`
	TgTdescr string `xml:"tgTdescr"`
}

func radioReferenceSoapEnvelope(method string, params string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>`+
		`<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://schemas.xmlsoap.org/soap/envelope/"`+
		` xmlns:xsd="http://www.w3.org/2001/XMLSchema"`+
		` xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"`+
		` xmlns:SOAP-ENC="http://schemas.xmlsoap.org/soap/encoding/"`+
		` xmlns:tns="http://api.radioreference.com/soap2/"`+
		` SOAP-ENV:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">`+
		`<SOAP-ENV:Body>`+
		`<tns:%s>%s</tns:%s>`+
		`</SOAP-ENV:Body></SOAP-ENV:Envelope>`, method, params, method)
}

func radioReferenceAuthBlock(creds *RadioReferenceCredentials) string {
	return fmt.Sprintf(`<authInfo xsi:type="tns:authInfo">`+
		`<username xsi:type="xsd:string">%s</username>`+
		`<password xsi:type="xsd:string">%s</password>`+
		`<appKey xsi:type="xsd:string">%s</appKey>`+
		`<version xsi:type="xsd:string">%s</version>`+
		`<style xsi:type="xsd:string">rpc</style>`+
		`</authInfo>`,
		xmlEscape(creds.Username),
		xmlEscape(creds.Password),
		xmlEscape(creds.AppKey),
		radioReferenceVersion,
	)
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

func radioReferenceCall(ctx context.Context, method, body string, dest *rrEnvelope) error {
	envelope := radioReferenceSoapEnvelope(method, body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, radioReferenceEndpoint, strings.NewReader(envelope))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("SOAPAction", `""`)

	client := &http.Client{Timeout: radioReferenceTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("radioreference: http: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, radioReferenceMaxBody))
	if err != nil {
		return fmt.Errorf("radioreference: read: %w", err)
	}

	if err := xml.Unmarshal(raw, dest); err != nil {
		if resp.StatusCode >= 400 {
			return fmt.Errorf("radioreference: http %d", resp.StatusCode)
		}
		return fmt.Errorf("radioreference: parse: %w", err)
	}

	if dest.Body.Fault != nil {
		msg := strings.TrimSpace(dest.Body.Fault.String)
		if msg == "" {
			msg = strings.TrimSpace(dest.Body.Fault.Code)
		}
		if msg == "" {
			msg = "unknown SOAP fault"
		}
		return errors.New(msg)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("radioreference: http %d", resp.StatusCode)
	}

	return nil
}

func radioReferenceFetchTalkgroups(ctx context.Context, creds *RadioReferenceCredentials, sid uint) ([]rrTalkgroupData, error) {
	auth := radioReferenceAuthBlock(creds)
	body := fmt.Sprintf(`%s<sid xsi:type="xsd:int">%d</sid>`, auth, sid)

	var env rrEnvelope
	if err := radioReferenceCall(ctx, "getTalkgroups", body, &env); err != nil {
		return nil, err
	}
	if env.Body.GetTalkgroupsResp == nil {
		return nil, errors.New("radioreference: empty getTalkgroups response")
	}
	return env.Body.GetTalkgroupsResp.Return.Items, nil
}

func radioReferenceFetchCats(ctx context.Context, creds *RadioReferenceCredentials, sid uint) (map[uint]string, error) {
	auth := radioReferenceAuthBlock(creds)
	body := fmt.Sprintf(`%s<sid xsi:type="xsd:int">%d</sid>`, auth, sid)

	var env rrEnvelope
	if err := radioReferenceCall(ctx, "getTalkgroupCats", body, &env); err != nil {
		return nil, err
	}

	out := map[uint]string{}
	if env.Body.GetTalkgroupCatsResp != nil {
		for _, item := range env.Body.GetTalkgroupCatsResp.Return.Items {
			out[item.TgCid] = item.TgCname
		}
	}
	return out, nil
}

func radioReferenceFetchTags(ctx context.Context, creds *RadioReferenceCredentials, sid uint) (map[uint]string, error) {
	auth := radioReferenceAuthBlock(creds)
	body := fmt.Sprintf(`%s<sid xsi:type="xsd:int">%d</sid>`, auth, sid)

	var env rrEnvelope
	if err := radioReferenceCall(ctx, "getTalkgroupTags", body, &env); err != nil {
		return nil, err
	}

	out := map[uint]string{}
	if env.Body.GetTalkgroupTagsResp != nil {
		for _, item := range env.Body.GetTalkgroupTagsResp.Return.Items {
			out[item.TgTid] = item.TgTdescr
		}
	}
	return out, nil
}

// RadioReferenceImportTalkgroups fetches all talkgroups for a Radio Reference
// system id and resolves the numeric tag/category ids to label strings.
func RadioReferenceImportTalkgroups(creds *RadioReferenceCredentials, sid uint) ([]RadioReferenceTalkgroup, error) {
	if creds == nil || strings.TrimSpace(creds.Username) == "" || creds.Password == "" || strings.TrimSpace(creds.AppKey) == "" {
		return nil, errors.New("radioreference: credentials are required")
	}
	if sid == 0 {
		return nil, errors.New("radioreference: system id is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), radioReferenceTimeout)
	defer cancel()

	talkgroups, err := radioReferenceFetchTalkgroups(ctx, creds, sid)
	if err != nil {
		return nil, err
	}

	cats, err := radioReferenceFetchCats(ctx, creds, sid)
	if err != nil {
		// non-fatal: keep numeric ids if we can't resolve names
		cats = map[uint]string{}
	}

	tags, err := radioReferenceFetchTags(ctx, creds, sid)
	if err != nil {
		tags = map[uint]string{}
	}

	out := make([]RadioReferenceTalkgroup, 0, len(talkgroups))
	for _, t := range talkgroups {
		tagLabel, ok := tags[t.TgTag]
		if !ok || tagLabel == "" {
			if t.TgTag == 0 {
				tagLabel = "Untagged"
			} else {
				tagLabel = fmt.Sprintf("Tag %d", t.TgTag)
			}
		}
		catLabel, ok := cats[t.TgCat]
		if !ok || catLabel == "" {
			if t.TgCat == 0 {
				catLabel = "Unknown"
			} else {
				catLabel = fmt.Sprintf("Category %d", t.TgCat)
			}
		}

		out = append(out, RadioReferenceTalkgroup{
			Dec:      t.TgDec,
			Hex:      t.TgHex,
			Alpha:    t.TgAlpha,
			Mode:     t.TgMode,
			Descr:    t.TgDescr,
			Tag:      tagLabel,
			Category: catLabel,
		})
	}

	return out, nil
}

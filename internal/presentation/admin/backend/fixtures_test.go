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

package backend

import "testing"

// capturedAnswers are the answers the legacy server put on the wire, recorded byte for byte and keyed
// by the name the test asks for them under. They live in the package that consumes them, which is what
// REQ-007 asks for: no fixture directory, no manifest, and nothing outside the test that proves it.
//
// The captures were taken from a legacy server answering the scenarios this package drives; the
// divergences this package states against them are recorded as the Q58 entries of
// decision-parity-verification.md.
var capturedAnswers = map[string]string{
	"rest-login.json": `{"passwordNeedChange":true,"token":"<token>"}`,
	"rest-logout-status.txt": `401
`,
	"rest-config.json": `{"config":{"access":[],"apikeys":[{"disabled":false,"id":1,"ident":"Capture open","key":"capture-apikey-open","systems":"*"},{"disabled":false,"id":2,"ident":"Capture scoped","key":"capture-apikey-scoped","systems":[{"id":10001,"talkgroups":"*"}]}],"dirwatch":[{"delay":1000,"deleteAfter":true,"directory":"/tmp/opencode/capture-watch","disabled":true,"extension":".wav","frequency":151400000,"id":1,"mask":"#S#TG#YYYY-MM-DDTHH-mm-ss","order":1,"siteId":1,"systemId":1,"talkgroupId":1,"type":"default"}],"downstreams":[{"apikey":"capture-apikey-open","disabled":true,"id":1,"order":1,"systems":[{"id":10001,"talkgroups":[1514000]}],"url":"http://127.0.0.1:39999/api/call-upload"}],"groups":[{"id":1,"label":"Air"},{"id":2,"label":"EMS"},{"id":3,"label":"Fire"},{"id":4,"label":"Interop"},{"id":5,"label":"Law"},{"id":6,"label":"Unknown"},{"id":7,"label":"Ground"}],"options":{"audioConversion":0,"autoPopulate":false,"branding":"Capture Dispatch","dimmerDelay":5000,"disableDuplicateDetection":true,"duplicateDetectionTimeFrame":500,"email":"dispatch@example.com","keypadBeeps":"uniden","maxClients":200,"playbackGoesLive":true,"pruneDays":7,"showListenersCount":true,"sortTalkgroups":true,"time12hFormat":true},"systems":[{"autoPopulate":false,"id":1,"label":"Grand Metro","order":1,"sites":[{"id":1,"label":"Metro East","siteRef":101,"systemId":0},{"id":2,"label":"Metro West","siteRef":102,"systemId":0}],"systemRef":10001,"talkgroups":[{"frequency":151400000,"groupIds":[7],"id":1,"label":"Ground","name":"Dispatch 1","tagId":3,"talkgroupRef":1514000,"type":"Aggregate"},{"groupIds":[3,7],"id":2,"label":"Ground","name":"Dispatch 2","tagId":4,"talkgroupRef":1514100},{"groupIds":[1],"id":3,"label":"Air","name":"Air 1","tagId":1,"talkgroupRef":1514200}],"units":[{"id":1001,"label":"Car 1","unitRef":1001},{"id":1002,"label":"Car 2","unitRef":1002}]},{"autoPopulate":false,"id":2,"label":"Harbour","order":2,"sites":[],"systemRef":10002,"talkgroups":[{"groupIds":[6],"id":4,"label":"Marine","name":"Marine 16","tagId":9,"talkgroupRef":1515000}],"units":[]}],"tags":[{"id":1,"label":"Air Traffic Control"},{"id":2,"label":"Emergency "},{"id":3,"label":"Fire Dispatch"},{"id":4,"label":"Fire Tac"},{"id":5,"label":"Fire Talk"},{"id":6,"label":"Interop"},{"id":7,"label":"Security"},{"id":8,"label":"Service"},{"id":9,"label":"Untagged"},{"id":10,"label":"Capture Tag"}],"version":"8.0.0"},"passwordNeedChange":true}`,
	"rest-config-unauthorized-status.txt": `401
`,
	"rest-alerts.json": `{"alert1":[{"begin":0,"end":0.05,"frequency":3000,"type":"square"},{"begin":0.075,"end":0.125,"frequency":3000,"type":"square"},{"begin":0.15,"end":0.2,"frequency":3000,"type":"square"}],"alert2":[{"begin":0,"end":0.05,"frequency":800,"type":"square"},{"begin":0.075,"end":0.125,"frequency":1000,"type":"square"},{"begin":0.15,"end":0.2,"frequency":800,"type":"square"},{"begin":0.225,"end":0.275,"frequency":1000,"type":"square"}],"alert3":[{"begin":0,"end":0.05,"frequency":800,"type":"square"},{"begin":0.075,"end":0.125,"frequency":1000,"type":"square"},{"begin":0.15,"end":0.175,"frequency":3000,"type":"square"},{"begin":0.225,"end":0.275,"frequency":3000,"type":"square"}],"alert4":[{"begin":0,"end":0.125,"frequency":800,"type":"square"},{"begin":0.125,"end":0.25,"frequency":1000,"type":"square"},{"begin":0.375,"end":0.5,"frequency":800,"type":"square"},{"begin":0.5,"end":0.625,"frequency":1000,"type":"square"},{"begin":0.75,"end":0.875,"frequency":800,"type":"square"},{"begin":0.875,"end":1,"frequency":1000,"type":"square"},{"begin":1.125,"end":1.25,"frequency":800,"type":"square"},{"begin":1.25,"end":1.325,"frequency":1000,"type":"square"}],"alert5":[{"begin":0,"end":1.5,"frequency":1200,"type":"square"}],"alert6":[{"begin":0,"end":0.175,"frequency":1200,"type":"square"},{"begin":0.225,"end":0.3,"frequency":1200,"type":"square"}],"alert7":[{"begin":0,"end":0.2,"frequency":2000,"type":"square"},{"begin":0.2,"end":0.4,"frequency":800,"type":"square"},{"begin":0.4,"end":0.6,"frequency":2000,"type":"square"},{"begin":0.6,"end":0.8,"frequency":800,"type":"square"},{"begin":0.8,"end":1,"frequency":2000,"type":"square"},{"begin":1,"end":1.2,"frequency":800,"type":"square"}],"alert8":[{"begin":0,"end":0.03,"frequency":500,"type":"square"},{"begin":0.04,"end":0.07,"frequency":500,"type":"square"},{"begin":0.08,"end":0.11,"frequency":500,"type":"square"}],"alert9":[{"begin":0,"end":0.07,"frequency":2400,"type":"square"},{"begin":0.09,"end":0.16,"frequency":3000,"type":"square"},{"begin":0.27,"end":0.34,"frequency":2400,"type":"square"},{"begin":0.37,"end":0.44,"frequency":3000,"type":"square"}]}`,
	"rest-logs.json": `{"count":3,"dateStart":"<datetime>","dateStop":"<datetime>","options":{"level":"warn","limit":200,"offset":0,"sort":0},"sources":[{"source":"config","count":2},{"source":"legacy","count":1}],"logs":[{"id":1,"dateTime":"<datetime>","level":"warn","source":"legacy","message":"server started"},{"id":2,"dateTime":"<datetime>","level":"warn","source":"config","message":"the groups configuration changed"},{"id":3,"dateTime":"<datetime>","level":"warn","source":"config","message":"the options configuration changed"}]}`,
	"rest-password.json": `{"passwordNeedChange":false}`,
	"rest-section-access.json": `[]`,
	"rest-section-apikeys.json": `[{"disabled":false,"id":1,"ident":"Capture open","key":"capture-apikey-open","systems":"*"},{"disabled":false,"id":2,"ident":"Capture scoped","key":"capture-apikey-scoped","systems":[{"id":10001,"talkgroups":"*"}]}]`,
	"rest-section-dirwatch.json": `[{"delay":1000,"deleteAfter":true,"directory":"/tmp/opencode/capture-watch","disabled":true,"extension":".wav","frequency":151400000,"id":1,"mask":"#S#TG#YYYY-MM-DDTHH-mm-ss","order":1,"siteId":1,"systemId":1,"talkgroupId":1,"type":"default"}]`,
	"rest-section-downstreams.json": `[{"apikey":"capture-apikey-open","disabled":true,"id":1,"order":1,"systems":[{"id":10001,"talkgroups":[1514000]}],"url":"http://127.0.0.1:39999/api/call-upload"}]`,
	"rest-section-groups.json": `[{"id":1,"label":"Air"},{"id":2,"label":"EMS"},{"id":3,"label":"Fire"},{"id":4,"label":"Interop"},{"id":5,"label":"Law"},{"id":6,"label":"Unknown"},{"id":7,"label":"Ground"}]`,
	"rest-section-options.json": `{"audioConversion":0,"autoPopulate":false,"branding":"Capture Dispatch","dimmerDelay":5000,"disableDuplicateDetection":true,"duplicateDetectionTimeFrame":500,"email":"dispatch@example.com","keypadBeeps":"uniden","maxClients":200,"playbackGoesLive":true,"pruneDays":7,"showListenersCount":true,"sortTalkgroups":true,"time12hFormat":true}`,
	"rest-section-systems.json": `[{"autoPopulate":false,"id":1,"label":"Grand Metro","order":1,"sites":[{"id":1,"label":"Metro East","siteRef":101,"systemId":0},{"id":2,"label":"Metro West","siteRef":102,"systemId":0}],"systemRef":10001,"talkgroups":[{"frequency":151400000,"groupIds":[7],"id":1,"label":"Ground","name":"Dispatch 1","tagId":3,"talkgroupRef":1514000,"type":"Aggregate"},{"groupIds":[3,7],"id":2,"label":"Ground","name":"Dispatch 2","tagId":4,"talkgroupRef":1514100},{"groupIds":[1],"id":3,"label":"Air","name":"Air 1","tagId":1,"talkgroupRef":1514200}],"units":[{"id":1001,"label":"Car 1","unitRef":1001},{"id":1002,"label":"Car 2","unitRef":1002}]},{"autoPopulate":false,"id":2,"label":"Harbour","order":2,"sites":[],"systemRef":10002,"talkgroups":[{"groupIds":[6],"id":4,"label":"Marine","name":"Marine 16","tagId":9,"talkgroupRef":1515000}],"units":[]}]`,
	"rest-section-tags.json": `[{"id":1,"label":"Air Traffic Control"},{"id":2,"label":"Emergency "},{"id":3,"label":"Fire Dispatch"},{"id":4,"label":"Fire Tac"},{"id":5,"label":"Fire Talk"},{"id":6,"label":"Interop"},{"id":7,"label":"Security"},{"id":8,"label":"Service"},{"id":9,"label":"Untagged"},{"id":10,"label":"Capture Tag"}]`,
	"adminws-bad-token.json": `{
  "code": 1000,
  "reason": ""
}
`,
	"adminws-config.json": `{"access":[],"apikeys":[{"disabled":false,"id":1,"ident":"Capture open","key":"capture-apikey-open","systems":"*"},{"disabled":false,"id":2,"ident":"Capture scoped","key":"capture-apikey-scoped","systems":[{"id":10001,"talkgroups":"*"}]}],"dirwatch":[],"downstreams":[{"apikey":"capture-apikey-open","disabled":true,"id":1,"order":1,"systems":[{"id":10001,"talkgroups":[1514000]}],"url":"http://127.0.0.1:39999/api/call-upload"}],"groups":[{"id":1,"label":"Air"},{"id":2,"label":"EMS"},{"id":3,"label":"Fire"},{"id":4,"label":"Interop"},{"id":5,"label":"Law"},{"id":6,"label":"Unknown"},{"id":7,"label":"Ground"},{"id":8,"label":"Ground"}],"options":{"audioConversion":0,"autoPopulate":false,"branding":"Capture Dispatch","dimmerDelay":5000,"disableDuplicateDetection":true,"duplicateDetectionTimeFrame":500,"email":"dispatch@example.com","keypadBeeps":"uniden","maxClients":200,"playbackGoesLive":true,"pruneDays":7,"showListenersCount":false,"sortTalkgroups":true,"time12hFormat":true},"systems":[{"autoPopulate":false,"id":1,"label":"Grand Metro","order":1,"sites":[{"id":1,"label":"Metro East","siteRef":101,"systemId":0},{"id":2,"label":"Metro West","siteRef":102,"systemId":0}],"systemRef":10001,"talkgroups":[{"groupIds":[1],"id":3,"label":"Air","name":"Air 1","tagId":1,"talkgroup":1,"talkgroupRef":1514200},{"frequency":151400000,"groupIds":[7],"id":1,"label":"Ground","name":"Dispatch 1","tagId":3,"talkgroup":2,"talkgroupRef":1514000,"type":"Aggregate"},{"groupIds":[3,7],"id":2,"label":"Ground","name":"Dispatch 2","tagId":4,"talkgroup":3,"talkgroupRef":1514100}],"units":[{"id":1001,"label":"Car 1","unitRef":1001},{"id":1002,"label":"Car 2","unitRef":1002}]},{"autoPopulate":false,"id":2,"label":"Harbour","order":2,"sites":[],"systemRef":10002,"talkgroups":[{"groupIds":[6],"id":4,"label":"Marine","name":"Marine 16","tagId":9,"talkgroup":1,"talkgroupRef":1515000}],"units":[]}],"tags":[{"id":1,"label":"Air Traffic Control"},{"id":2,"label":"Emergency "},{"id":3,"label":"Fire Dispatch"},{"id":4,"label":"Fire Tac"},{"id":5,"label":"Fire Talk"},{"id":6,"label":"Interop"},{"id":7,"label":"Security"},{"id":8,"label":"Service"},{"id":9,"label":"Untagged"},{"id":10,"label":"Capture Tag"},{"id":11,"label":"Capture Tag"}],"version":"8.0.0"}`,
}

// capturedAnswer hands back the recorded answer under the name a test asks for it under, and stops
// when that name was never recorded: a test that asks for a capture this package does not hold has
// changed, and it would rather stop than quietly compare against nothing.
func capturedAnswer(t *testing.T, name string) string {
	t.Helper()

	body, ok := capturedAnswers[name]
	if !ok {
		t.Fatalf("the captured answer %s is not recorded in this package", name)
	}

	return body
}

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

package upload

import "testing"

// capturedAnswers are the answers the legacy server put on the wire for the upload endpoints, recorded
// byte for byte and keyed by the name the test asks for them under. They live in the package that
// consumes them, which is what REQ-007 asks for: no fixture directory, no manifest, and nothing outside
// the test that proves it. The upload endpoints answer with plain text, so the captures are compared as
// bytes and not decoded.
var capturedAnswers = map[string]string{
	"rest-upload.txt": `Call imported successfully.
`,
	"rest-upload-denied.txt": `Invalid API key for system <nil> talkgroup <nil>.
`,
	"rest-trunk-recorder-upload.txt": `Call imported successfully.
`,
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

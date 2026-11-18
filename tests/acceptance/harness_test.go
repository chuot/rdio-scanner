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

package acceptance

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"rdio-scanner/internal/app"
	"rdio-scanner/internal/application"
	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/infrastructure/clock"
	"rdio-scanner/internal/infrastructure/ffmpeg"
	"rdio-scanner/internal/infrastructure/persistence"
)

// This is the harness the journeys in this package run against: a whole
// server reached only through the composition root's exported API, and a
// second database connection, configured the way a deployment configures one.

// journeyServer is a server a journey is running against, together with the two things a journey reads
// and writes beside it: the folder the server was built in, and the database the server opened.
type journeyServer struct {
	server *app.Server
	base   string
	db     *sql.DB
}

// startedJourneyServer is a server that has been through the full start
// sequence, on a folder the test owns.
func startedJourneyServer(t *testing.T) *journeyServer {
	t.Helper()

	return startedJourneyServerIn(t, t.TempDir())
}

// journeyServerIn builds a server against a folder the test owns and does not start it, which is how a
// journey looks at the state a previous server left behind.
func journeyServerIn(t *testing.T, base string) *journeyServer {
	t.Helper()

	srv, err := app.New(journeyConfigIn(base))
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}

	t.Cleanup(func() { srv.Close() })

	return &journeyServer{server: srv, base: base, db: openTestDatabase(t, base)}
}

// consoleJourneyServerIn is a started server on a folder the test chose, with the console the test reads
// handed to it. A journey that reads what the server says out loud builds its server this way; every other
// journey takes the harness's discarded console.
func consoleJourneyServerIn(t *testing.T, base string, out io.Writer) *journeyServer {
	t.Helper()

	srv, err := app.New(servedConfigIn(base, out))
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}

	t.Cleanup(func() { srv.Close() })

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	return &journeyServer{server: srv, base: base, db: openTestDatabase(t, base)}
}

// startedJourneyServerIn is a server that has been through the start sequence on a folder the test chose.
func startedJourneyServerIn(t *testing.T, base string) *journeyServer {
	t.Helper()

	srv := journeyServerIn(t, base)

	if err := srv.server.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	return srv
}

// theJourneyVersion is the version a journey's server reports. The version a build injects is a fact about
// the build, not a fact about a server, so the journeys name the version they configure the server with
// instead of inheriting what the binary being compiled happens to carry.
const theJourneyVersion = "8.0.0"

// servedConfigIn is the configuration a test builds a server from on a folder the test owns, with the
// console the test reads.
func servedConfigIn(base string, out io.Writer) app.Config {
	config := journeyConfigIn(base)
	config.Out = out

	return config
}

func journeyConfigIn(base string) app.Config {
	return app.Config{
		BaseDir: base,
		DbFile:  "rdio-scanner.db",
		DbType:  app.DbTypeSqlite,
		Listen:  ":3000",
		Version: theJourneyVersion,

		// A journey reads the server's log view, not its terminal, so the server's lines out loud are
		// discarded.
		Out: io.Discard,

		// the converter a journey's server is given is the ffmpeg the host installs,
		// and the prober measures nothing: every journey reaching conversion turns
		// it off, and conversion and probing are proven where ffmpeg and ffprobe
		// are the subject.
		Converter: func(dir string) (ports.AudioConverter, ports.AudioLengthProber, string) {
			return ffmpeg.New(), &fakes.Prober{}, ""
		},
	}
}

// stop ends the server the way an interrupted server ends itself: the watchers, the schedules, the
// connections, and the database.
func (s *journeyServer) stop(t *testing.T) {
	t.Helper()

	if err := s.server.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// pruneHandler is the retention pass a journey asks for. The server runs it on its own hourly schedule,
// which a journey cannot wait for, so the journey builds the handler the way the composition root wires
// it — the same repositories over the same database, the server's own clock — and hands it the command the
// schedule hands it.
func (s *journeyServer) pruneHandler(t *testing.T) *application.PruneOldCallsHandler {
	t.Helper()

	return application.NewPruneOldCallsHandler(
		persistence.NewCalls(s.db, persistence.DialectSqlite),
		persistence.NewLogs(s.db, persistence.DialectSqlite),
		persistence.NewOptions(s.db, persistence.DialectSqlite),
		clock.NewClock(),
	)
}

// defaultAdminCredential is the credential a fresh server accepts, and the one the recorded runs logged in
// with.
const defaultAdminCredential = "rdio-scanner"

func (s *journeyServer) get(path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()

	s.server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

	return rec
}

// askForAnUpgrade sends a request that claims to be a websocket upgrade and is not one: the Upgrade header
// is the one header this server routes on, and everything else Go's upgrader insists on is left out. It is
// how a journey reaches the listener endpoint's refusal of a connection it would not take over.
func (s *journeyServer) askForAnUpgrade(path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Upgrade", "websocket")

	s.server.ServeHTTP(rec, req)

	return rec
}

func (s *journeyServer) post(path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()

	s.server.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))

	return rec
}

// put sends an authenticated request to an administrator endpoint, which is how a journey installs a
// configuration the way an administrator installs one.
func (s *journeyServer) put(path, token, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()

	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	req.Header.Set("Authorization", token)

	s.server.ServeHTTP(rec, req)

	return rec
}

// postAuthorized is the same request with the verb an administrator's own form uses.
func (s *journeyServer) postAuthorized(path, token, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Authorization", token)

	s.server.ServeHTTP(rec, req)

	return rec
}

// authorizedRequest reaches a path that needs the session a login handed out, with the verb the test
// names. The credential goes in the Authorization header raw, with no scheme in front of it, because that
// is how the administration interface sends it.
func (s *journeyServer) authorizedRequest(t *testing.T, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", token)

	rec := httptest.NewRecorder()

	s.server.ServeHTTP(rec, req)

	return rec
}

// codeRequest asks a call history endpoint with a listener access code on the request, which is how
// a listener-facing journey proves itself. The header is not the administrator's: the two
// credentials are two principals with two sets of rights, and a journey that mixed them up would
// prove the wrong thing.
func (s *journeyServer) codeRequest(method, path, code, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("X-Access-Code", code)

	s.server.ServeHTTP(rec, req)

	return rec
}

// codeRequestFrom asks a call history endpoint with an access code, from an address the journey
// names: the guess budget is keyed on the address, and a journey that wants to show the budget
// belongs to one address has to be able to arrive from another one.
func (s *journeyServer) codeRequestFrom(method, path, code, body, remoteAddr string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("X-Access-Code", code)
	req.RemoteAddr = remoteAddr + ":52000"

	s.server.ServeHTTP(rec, req)

	return rec
}

// getRange asks an endpoint for a slice of what it holds, which is how a client resumes a download
// or seeks a player.
func (s *journeyServer) getRange(path, rng string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Range", rng)

	s.server.ServeHTTP(rec, req)

	return rec
}

// getAuthorized asks an administrator endpoint for what it holds with a session on the request.
func (s *journeyServer) getAuthorized(path, token string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", token)

	s.server.ServeHTTP(rec, req)

	return rec
}

// loginAnswered logs a journey in the way an administrator logs in, and hands back the session token the
// login answer carries.
func (s *journeyServer) loginAnswered(t *testing.T) string {
	t.Helper()

	rec := s.post("/api/admin/login", `{"password":"`+defaultAdminCredential+`"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("login against a fresh server was answered %d: %s", rec.Code, rec.Body.String())
	}

	var answered struct {
		Token string `json:"token"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &answered); err != nil {
		t.Fatalf("the login answer is not the answer the legacy server gives: %v", err)
	}

	if answered.Token == "" {
		t.Fatalf("the login answer carries no session: %s", rec.Body.String())
	}

	return answered.Token
}

// dial opens a listener socket against the server's own routes, which is how a journey reaches a path that
// can be either an endpoint or a socket.
func (s *journeyServer) dial(t *testing.T, path string) *websocket.Conn {
	t.Helper()

	test := httptest.NewServer(s.server)
	t.Cleanup(test.Close)

	url := "ws" + strings.TrimPrefix(test.URL, "http") + path

	// The upgrade headers are the dialer's to write: a caller that writes them itself is refused by the
	// dialer for duplicating them.
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("opening %s as a socket: %v", path, err)
	}

	t.Cleanup(func() { conn.Close() })

	return conn
}

// sendFrame writes a frame exactly as the journey names it, and assertFrame waits for one frame and
// compares it byte for byte.
func sendFrame(t *testing.T, conn *websocket.Conn, frame string) {
	t.Helper()

	if err := conn.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
		t.Fatalf("sending %s: %v", frame, err)
	}
}

func assertFrame(t *testing.T, conn *websocket.Conn, want string) {
	t.Helper()

	if got := readRawFrame(t, conn); got != want {
		t.Errorf("the server wrote\n  %s\nwant\n  %s", got, want)
	}
}

// readRawFrame waits for one frame and returns it as the bytes the server wrote, because these journeys
// compare what a server said rather than what they think it meant.
func readRawFrame(t *testing.T, conn *websocket.Conn) string {
	t.Helper()

	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetReadDeadline(time.Time{})

	_, b, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("waiting for a frame: %v", err)
	}

	return string(b)
}

func readFrame(t *testing.T, conn *websocket.Conn) []any {
	t.Helper()

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	_, b, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("waiting for a frame: %v", err)
	}

	var frame []any
	if err := json.Unmarshal(b, &frame); err != nil {
		t.Fatalf("the frame %s is not a frame: %v", b, err)
	}

	return frame
}

// closedWith installs the handler that reports how a connection was ended, and returns the thing that
// waits for it. A socket closed with the normal code is not reported by ReadMessage as a close code, which
// is why a journey that cares about the code has to ask for it before reading.
func closedWith(t *testing.T, conn *websocket.Conn) func() int {
	t.Helper()

	var (
		code    = -1
		arrived = make(chan struct{}, 1)
	)

	conn.SetCloseHandler(func(closeCode int, text string) error {
		code = closeCode

		arrived <- struct{}{}

		return conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(closeCode, ""), time.Now().Add(time.Second))
	})

	return func() int {
		t.Helper()

		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		defer conn.SetReadDeadline(time.Time{})

		for {
			select {
			case <-arrived:
				return code
			default:
				if _, _, err := conn.ReadMessage(); err != nil {
					if code != -1 {
						return code
					}

					t.Fatalf("waiting for the socket to be ended: %v", err)
				}
			}
		}
	}
}

// uploadedRecording is a recording posted to the upload endpoint, with the moment it happened named the way
// an uploader names it.
type uploadedRecording struct {
	key       string
	systemRef uint
	talkgroup uint
	filename  string
	audio     []byte
	at        time.Time

	// audioName is the name the upload gives its recording out loud, which is how an uploader
	// names the type it posted: the server reads the recording's type from the extension of the
	// name it is given, and an upload that posts only the file part stores a recording whose type
	// it never named.
	audioName string
}

// uploadRecording posts a recording the way an uploader posts one, and reports how the server answered. The
// upload is how these tests put a call into the ingest path without waiting for a watched directory to
// settle, which is what makes a hold of a few seconds testable.
func (s *journeyServer) uploadRecording(t *testing.T, upload uploadedRecording) *httptest.ResponseRecorder {
	t.Helper()

	body := bytes.Buffer{}
	writer := multipart.NewWriter(&body)

	fields := []struct{ name, value string }{
		{name: "key", value: upload.key},
		{name: "system", value: fmt.Sprintf("%d", upload.systemRef)},
		{name: "talkgroup", value: fmt.Sprintf("%d", upload.talkgroup)},
		{name: "timestamp", value: fmt.Sprintf("%d", upload.at.UnixMilli())},
	}

	for _, field := range fields {
		if err := writer.WriteField(field.name, field.value); err != nil {
			t.Fatalf("writing the %s field: %v", field.name, err)
		}
	}

	part, err := writer.CreateFormFile("audio", upload.filename)
	if err != nil {
		t.Fatalf("writing the recording: %v", err)
	}

	if _, err := part.Write(upload.audio); err != nil {
		t.Fatalf("writing the recording: %v", err)
	}

	// The name the upload gives its recording out loud comes after the recording itself, which is
	// the order that decides which of the two wins: the server reads a field per upload and the
	// later one writes over what the earlier one wrote.
	if upload.audioName != "" {
		if err := writer.WriteField("audioFilename", upload.audioName); err != nil {
			t.Fatalf("writing the audioFilename field: %v", err)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("closing the multipart body: %v", err)
	}

	rec := httptest.NewRecorder()

	req := httptest.NewRequest(http.MethodPost, "/api/call-upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	s.server.ServeHTTP(rec, req)

	return rec
}

// uploadedConnectionCheck is a capture client's connection check: the credential it always sends,
// the system number its own configuration names, and the part that says the submission is only a
// test. It carries no recording, because a client asking whether the server is listening has no
// recording to send.
type uploadedConnectionCheck struct {
	key       string
	systemRef uint

	// testValue is what the client writes in the part named test. "1" is what a capture client
	// writes; an empty string leaves the part out, which is how a submission that is not a
	// connection check posts.
	testValue string

	// talkgroup and at are what a connection check names when the client's own configuration knows
	// more than a system: a talkgroup number this server has never been told about, and the moment
	// the client stamps its submission with. A check names no recording either way.
	talkgroup uint
	at        time.Time
}

// uploadConnectionCheck posts a connection check the way a capture client posts one, and reports how
// the server answered. Only the fields the client names are written, and no recording part is written
// at all, which is what makes the submission short of a recording whatever the client named beside it.
func (s *journeyServer) uploadConnectionCheck(t *testing.T, check uploadedConnectionCheck) *httptest.ResponseRecorder {
	t.Helper()

	body := bytes.Buffer{}
	writer := multipart.NewWriter(&body)

	fields := []struct{ name, value string }{
		{name: "key", value: check.key},
		{name: "system", value: fmt.Sprintf("%d", check.systemRef)},
	}

	if check.talkgroup != 0 {
		fields = append(fields, struct{ name, value string }{
			name:  "talkgroup",
			value: fmt.Sprintf("%d", check.talkgroup),
		})
	}

	if !check.at.IsZero() {
		fields = append(fields, struct{ name, value string }{
			name:  "dateTime",
			value: fmt.Sprintf("%d", check.at.UnixMilli()),
		})
	}

	if check.testValue != "" {
		fields = append(fields, struct{ name, value string }{
			name:  "test",
			value: check.testValue,
		})
	}

	for _, field := range fields {
		if err := writer.WriteField(field.name, field.value); err != nil {
			t.Fatalf("writing the %s field: %v", field.name, err)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("closing the multipart body: %v", err)
	}

	rec := httptest.NewRecorder()

	req := httptest.NewRequest(http.MethodPost, "/api/call-upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	s.server.ServeHTTP(rec, req)

	return rec
}

// encodeAccessCode encodes an access code the way a client does, because the wire carries it that way.
func encodeAccessCode(code string) string {
	return base64.StdEncoding.EncodeToString([]byte(code))
}

// openTestDatabase opens the database a server wrote, so a journey can seed a configuration and read back
// what the server left behind.
func openTestDatabase(t *testing.T, base string) *sql.DB {
	t.Helper()

	db, err := persistence.Open(persistence.Settings{
		Dialect:  persistence.DialectSqlite,
		FilePath: filepath.Join(base, "rdio-scanner.db"),
	})
	if err != nil {
		t.Fatalf("opening the database the server wrote: %v", err)
	}

	t.Cleanup(func() { db.Close() })

	return db
}

// seedStatement is one of the statements that put a configuration in front of a server, with the values it
// needs passed in rather than written into it.
type seedStatement struct {
	query string
	args  []any
}

func seed(t *testing.T, db *sql.DB, statements ...seedStatement) {
	t.Helper()

	for _, statement := range statements {
		if _, err := db.Exec(statement.query, statement.args...); err != nil {
			t.Fatalf("seeding %s: %v", statement.query, err)
		}
	}
}

// seedUploadTarget installs what a journey needs to put a call into the ingest path through the upload
// endpoint: the system and talkgroup the recording names, the credential the post is made with, and the
// switch that leaves the recording as it was uploaded so that what the journey is about is the server and
// not ffmpeg. The hold is named in the unit the configuration stores it in, so a journey can say whether
// the call it posts is one the server holds back.
func seedUploadTarget(t *testing.T, db *sql.DB, hold uint, key string) {
	t.Helper()

	seed(t, db,
		seedStatement{
			query: `INSERT INTO "systems" ("systemId", "autoPopulate", "blacklists", "delay", "label", "led", "order", "systemRef", "type") VALUES (1, 0, '', 0, 'Grand Metro', '', 0, 10001, '')`,
		},
		seedStatement{
			query: fmt.Sprintf(`INSERT INTO "talkgroups" ("talkgroupId", "alert", "delay", "frequency", "label", "led", "name", "order", "systemId", "tagId", "talkgroupRef", "type") VALUES (1, '', %d, 0, 'Main', '', 'Dispatch 1', 0, 1, 9, 1514000, '')`, hold),
		},
		seedStatement{
			query: `INSERT INTO "apikeys" ("apikeyId", "disabled", "ident", "key", "order", "systems") VALUES (1, 0, 'test upload', ?, 0, '"*"')`,
			args:  []any{key},
		},
		seedStatement{
			query: `INSERT INTO "options" ("key", "value") VALUES ('audioConversion', '0')`,
		},
	)
}

func countRows(t *testing.T, db *sql.DB, query string) int {
	t.Helper()

	var rows int
	if err := db.QueryRow(query).Scan(&rows); err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	return rows
}

// countLogLines is how many times the server wrote exactly this entry.
func countLogLines(t *testing.T, db *sql.DB, message string) int {
	t.Helper()

	const query = `SELECT COUNT(*) FROM "logs" WHERE "message" = ?`

	var rows int
	if err := db.QueryRow(query, message).Scan(&rows); err != nil {
		t.Fatalf("%s (%s): %v", query, message, err)
	}

	return rows
}

func hasLogMessage(t *testing.T, db *sql.DB, message string) bool {
	t.Helper()

	const query = `SELECT COUNT(*) FROM "logs" WHERE "message" = ?`

	var rows int
	if err := db.QueryRow(query, message).Scan(&rows); err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	return rows > 0
}

// logSourceOfMessage is the subsystem a stored entry is filed under, or an empty string when no entry
// carries that message. A journey that expects a source reads this, so a writer that names nothing fails as
// a missing subsystem rather than as a missing message.
func logSourceOfMessage(t *testing.T, db *sql.DB, message string) string {
	t.Helper()

	const query = `SELECT "source" FROM "logs" WHERE "message" = ? ORDER BY "logid" DESC LIMIT 1`

	var source string
	if err := db.QueryRow(query, message).Scan(&source); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ""
		}

		t.Fatalf("%s (%s): %v", query, message, err)
	}

	return source
}

// logLevelOfMessage is the level a stored entry was filed at, or an empty string when no entry carries
// that message. A journey that expects an entry to be filed where an operator reads a routine upload
// problem, rather than where a lost recording is, reads this beside logSourceOfMessage.
func logLevelOfMessage(t *testing.T, db *sql.DB, message string) string {
	t.Helper()

	const query = `SELECT "level" FROM "logs" WHERE "message" = ? ORDER BY "logid" DESC LIMIT 1`

	var level string
	if err := db.QueryRow(query, message).Scan(&level); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ""
		}

		t.Fatalf("%s (%s): %v", query, message, err)
	}

	return level
}

// storedLogMentioning reads the first stored server log entry that mentions a fragment, from a database the
// journey opened itself. It is how a journey looks at what a stopped server left behind: the server's own
// connection is closed by then, so the only way in is through the file.
func storedLogMentioning(t *testing.T, db *sql.DB, fragment string) string {
	t.Helper()

	const query = `SELECT "message" FROM "logs" ORDER BY "logid"`

	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	defer rows.Close()

	for rows.Next() {
		var message string

		if err := rows.Scan(&message); err != nil {
			t.Fatalf("reading a server log entry: %v", err)
		}

		if strings.Contains(message, fragment) {
			return message
		}
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	t.Fatalf("the server log holds no entry mentioning %s", fragment)

	return ""
}

func readOptionValue(t *testing.T, db *sql.DB, key string) string {
	t.Helper()

	const query = `SELECT "value" FROM "options" WHERE "key" = ?`

	var value string
	if err := db.QueryRow(query, key).Scan(&value); err != nil {
		t.Fatalf("%s (%s): %v", query, key, err)
	}

	return value
}

// waitForLogMessage waits for the server to have written a log entry with exactly this message.
func waitForLogMessage(t *testing.T, db *sql.DB, message string) {
	t.Helper()

	waitForLog(t, db, message, func(found string) bool { return found == message })
}

// waitForLogMentioning waits for a log entry whose message contains this fragment, which is how a journey
// waits for a run to have reached a particular recording without repeating the parts of the line that the
// recording's own configuration decides.
func waitForLogMentioning(t *testing.T, db *sql.DB, fragment string) {
	t.Helper()

	waitForLog(t, db, fragment, func(found string) bool { return strings.Contains(found, fragment) })
}

// waitForLog waits for the server to have written a log entry that matches, because some of what it writes
// is something that happens on its own schedule rather than the answer to a request.
func waitForLog(t *testing.T, db *sql.DB, what string, matches func(string) bool) {
	t.Helper()

	deadline := time.Now().Add(20 * time.Second)

	for {
		rows, err := db.Query(`SELECT "message" FROM "logs"`)
		if err != nil {
			t.Fatalf("reading the server log: %v", err)
		}

		for rows.Next() {
			var message string

			if err := rows.Scan(&message); err != nil {
				rows.Close()

				t.Fatalf("reading a server log entry: %v", err)
			}

			if matches(message) {
				rows.Close()

				return
			}
		}

		rows.Close()

		if err := rows.Err(); err != nil {
			t.Fatalf("reading the server log: %v", err)
		}

		if time.Now().After(deadline) {
			t.Fatalf("the server never logged %s", what)
		}

		time.Sleep(100 * time.Millisecond)
	}
}

// assertEveryServerLogEntry is the guard over a real server's log view: every
// row names the subsystem that wrote it and no row begins with the internal
// name of the code that wrote it, read back out of the writing store.
func assertEveryServerLogEntry(t *testing.T, db *sql.DB) {
	t.Helper()

	const query = `SELECT "source", "message" FROM "logs" ORDER BY "logid"`

	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("reading the server's own log view: %v", err)
	}

	defer rows.Close()

	written := 0

	for rows.Next() {
		var source, message string

		if err := rows.Scan(&source, &message); err != nil {
			t.Fatalf("reading an entry from the server's own log view: %v", err)
		}

		written++

		if message == "" {
			t.Errorf("the server wrote an entry with no message")
			continue
		}

		if source == "" {
			t.Errorf("the server wrote %q without naming the part of the server that wrote it", message)
			continue
		}

		if !domain.IsKnownLogSource(domain.LogSource(source)) {
			t.Errorf("the server wrote %q under %q, which is not a part of this server", message, source)
			continue
		}

		if prefix := internalPrefixAt(message); prefix != "" {
			t.Errorf("the server wrote %q, which begins with the internal name %q instead of saying what happened",
				message, prefix)
		}
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("reading the server's own log view: %v", err)
	}

	if written == 0 {
		t.Errorf("the server wrote nothing to its own log view, so this guard proves nothing about what it writes")
	}
}

// internalPrefixAt is the front-of-message shape the wording rule forbids:
// a token naming a subsystem and an operation, or a bare subsystem name,
// ending in a colon; only the front counts, the failure's own text after it
// names the statement or syscall that failed and is kept verbatim.
func internalPrefixAt(message string) string {
	first := message

	if space := strings.Index(message, " "); space >= 0 {
		first = message[:space]
	}

	if strings.HasSuffix(first, ":") {
		return first
	}

	return ""
}

// compareCanonical compares two things by putting both in the shape JSON writes them in, with the keys of
// every object in the same order, so that a difference is a difference in what the servers did and not in
// what order a map happened to be walked.
func compareCanonical(t *testing.T, what string, want json.RawMessage, got any) {
	t.Helper()

	var decoded any

	if err := json.Unmarshal(want, &decoded); err != nil {
		t.Fatalf("the captured %s is not something JSON can hold: %v", what, err)
	}

	wanted, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("writing the captured %s: %v", what, err)
	}

	gotten, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("writing %s: %v", what, err)
	}

	if string(wanted) != string(gotten) {
		t.Errorf("%s\ncaptured: %s\nthis server: %s", what, wanted, gotten)
	}
}

// rowsOf reads back one of the tables a run is expected to have left
// behind; the log entries name watched directories under the temporary
// folder, rewritten to <base> here and in the capture, because the capture
// is of what the run did and not of which folder this test used.
func rowsOf(t *testing.T, db *sql.DB, base, table string) []map[string]any {
	t.Helper()

	queries := map[string]string{
		"calls": `SELECT "callId", "audioFilename", "audioMime", "siteRef", "systemId", "talkgroupId", "timestamp", length("audio") AS "audioBytes" FROM "calls" ORDER BY "callId"`,
		// the comparison query skips log entries whose source is the server itself:
		// those have no counterpart in a recorded run, and each one is pinned by its
		// own test instead.
		"logs": `SELECT "level", "message" FROM "logs" WHERE "source" != 'server' ORDER BY "logid"`,
	}

	query, ok := queries[table]
	if !ok {
		query = fmt.Sprintf(`SELECT * FROM "%s" ORDER BY 1`, table)
	}

	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("reading the %s the server left behind: %v", table, err)
	}

	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		t.Fatalf("reading the %s the server left behind: %v", table, err)
	}

	found := []map[string]any{}

	for rows.Next() {
		values := make([]any, len(columns))
		holders := make([]any, len(columns))

		for i := range values {
			holders[i] = &values[i]
		}

		if err := rows.Scan(holders...); err != nil {
			t.Fatalf("reading a %s row: %v", table, err)
		}

		row := map[string]any{}

		for i, column := range columns {
			// A driver that hands back text as bytes is the same text as far as the recorded run is concerned.
			if text, ok := values[i].([]byte); ok {
				row[column] = string(text)

				continue
			}

			row[column] = values[i]
		}

		if table == "logs" {
			if message, ok := row["message"].(string); ok {
				row["message"] = strings.ReplaceAll(message, base, "<base>")
			}
		}

		found = append(found, row)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("reading the %s the server left behind: %v", table, err)
	}

	return found
}

// findFiles lists the files left in a watched directory and the directories under it.
func findFiles(t *testing.T, directory string) []string {
	t.Helper()

	left := []string{}

	err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !entry.IsDir() {
			left = append(left, path)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("looking at what the watch left behind in %s: %v", directory, err)
	}

	return left
}

// recording is audio for a watched file. Nothing in these runs looks inside the recording — a watch reads a
// recording's name and its sidecar, and the ingest path only asks whether there is more audio than a
// container header — so what matters is that it is as long as the recording the recorded run was captured
// from.
func recording(bytes int) []byte {
	audio := make([]byte, bytes)

	for i := range audio {
		audio[i] = byte(i % 251)
	}

	return audio
}

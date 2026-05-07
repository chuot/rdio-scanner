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
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/bcrypt"
)

func strconvAtoiUint(s string) (uint, error) {
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, err
	}
	return uint(n), nil
}

type Admin struct {
	Attempts         AdminLoginAttempts
	AttemptsMax      uint
	AttemptsMaxDelay time.Duration
	Broadcast        chan *[]byte
	Conns            map[*websocket.Conn]bool
	Controller       *Controller
	Register         chan *websocket.Conn
	Tokens           []string
	Unregister       chan *websocket.Conn
	mutex            sync.Mutex
	running          bool
}

type AdminLoginAttempt struct {
	Count uint
	Date  time.Time
}

type AdminLoginAttempts map[string]*AdminLoginAttempt

const adminTokenLifetime = 24 * time.Hour

func NewAdmin(controller *Controller) *Admin {
	return &Admin{
		Attempts:         AdminLoginAttempts{},
		AttemptsMax:      uint(3),
		AttemptsMaxDelay: 10 * time.Minute,
		Broadcast:        make(chan *[]byte),
		Conns:            make(map[*websocket.Conn]bool),
		Controller:       controller,
		Register:         make(chan *websocket.Conn),
		Tokens:           []string{},
		Unregister:       make(chan *websocket.Conn),
		mutex:            sync.Mutex{},
	}
}

// BroadcastConfig hands the serialized config off to the Start() loop
// which is the only goroutine that reads/writes admin.Conns. Iterating
// admin.Conns directly here used to race with Register/Unregister.
// Callers already wrap this in a goroutine (controller.EmitConfig), so
// blocking on the unbuffered channel is fine.
func (admin *Admin) BroadcastConfig() {
	if b, err := json.Marshal(admin.GetConfig()); err == nil {
		admin.Broadcast <- &b
	}
}

func (admin *Admin) ChangePassword(currentPassword any, newPassword string) error {
	var (
		err  error
		hash []byte
	)

	if len(newPassword) == 0 {
		return errors.New("newPassword is empty")
	}

	switch v := currentPassword.(type) {
	case string:
		if err = bcrypt.CompareHashAndPassword([]byte(admin.Controller.Options.adminPassword), []byte(v)); err != nil {
			return err
		}
	}

	if hash, err = bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost); err != nil {
		return err
	}

	admin.Controller.Options.adminPassword = string(hash)
	admin.Controller.Options.adminPasswordNeedChange = newPassword == defaults.adminPassword

	if err := admin.Controller.Options.Write(admin.Controller.Database); err != nil {
		return err
	}

	if err := admin.Controller.Options.Read(admin.Controller.Database); err != nil {
		return err
	}

	admin.Controller.Logs.LogEvent(LogLevelWarn, "admin password changed.")

	return nil
}

func (admin *Admin) ConfigHandler(w http.ResponseWriter, r *http.Request) {
	if strings.EqualFold(r.Header.Get("upgrade"), "websocket") {
		// Same-origin enforcement on the admin event stream. The default
		// gorilla CheckOrigin only blocks cross-origin if Origin differs
		// from Host, but only when set explicitly. Make it explicit so a
		// future gorilla default change can't silently open this up.
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				if origin == "" {
					return true // non-browser client (e.g. curl)
				}
				u, err := url.Parse(origin)
				if err != nil {
					return false
				}
				return strings.EqualFold(stripPort(r.Host), u.Hostname())
			},
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		go func() {
			const (
				authWait    = 10 * time.Second
				idleTimeout = 5 * time.Minute
			)

			// Authenticate first. The previous flow registered the
			// connection on admin.Register before reading the token, so
			// any anonymous client that completed the WebSocket
			// handshake would receive the next BroadcastConfig (which
			// includes API keys, dirwatch paths, etc.) before being
			// disconnected. Now: read the token under a short deadline,
			// validate, and only then enroll in the broadcast set.
			conn.SetReadDeadline(time.Now().Add(authWait))
			_, first, err := conn.ReadMessage()
			if err != nil || !admin.ValidateToken(string(first)) {
				conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "auth"))
				conn.Close()
				return
			}

			admin.Register <- conn

			defer func() {
				admin.Unregister <- conn
				conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(1000, ""))
			}()

			for {
				// Bound how long a stale client can hold a slot. A
				// disconnected admin used to leak the conn forever.
				conn.SetReadDeadline(time.Now().Add(idleTimeout))

				_, b, err := conn.ReadMessage()
				if err != nil {
					return
				}

				if !admin.ValidateToken(string(b)) {
					return
				}
			}
		}()

	} else {
		logError := func(err error) {
			admin.Controller.Logs.LogEvent(LogLevelError, fmt.Sprintf("admin.confighandler.put: %s", err.Error()))
		}

		t := admin.GetAuthorization(r)
		if !admin.ValidateToken(t) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		switch r.Method {
		case http.MethodGet:
			admin.SendConfig(w)

		case http.MethodPut:
			m := map[string]any{}
			err := json.NewDecoder(r.Body).Decode(&m)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			admin.mutex.Lock()
			defer admin.mutex.Unlock()

			admin.Controller.Dirwatches.Stop()

			switch v := m["access"].(type) {
			case []any:
				admin.Controller.Accesses.FromMap(v)
				err := admin.Controller.Accesses.Write(admin.Controller.Database)
				if err != nil {
					logError(err)
				} else {
					err = admin.Controller.Accesses.Read(admin.Controller.Database)
					if err != nil {
						logError(err)
					}
				}
			}

			switch v := m["apiKeys"].(type) {
			case []any:
				admin.Controller.Apikeys.FromMap(v)
				err = admin.Controller.Apikeys.Write(admin.Controller.Database)
				if err != nil {
					logError(err)
				} else {
					err = admin.Controller.Apikeys.Read(admin.Controller.Database)
					if err != nil {
						logError(err)
					}
				}
			}

			switch v := m["dirWatch"].(type) {
			case []any:
				admin.Controller.Dirwatches.FromMap(v)
				err = admin.Controller.Dirwatches.Write(admin.Controller.Database)
				if err != nil {
					logError(err)
				} else {
					err = admin.Controller.Dirwatches.Read(admin.Controller.Database)
					if err != nil {
						logError(err)
					}
				}
			}

			switch v := m["downstreams"].(type) {
			case []any:
				admin.Controller.Downstreams.FromMap(v)
				err = admin.Controller.Downstreams.Write(admin.Controller.Database)
				if err != nil {
					logError(err)
				} else {
					err = admin.Controller.Downstreams.Read(admin.Controller.Database)
					if err != nil {
						logError(err)
					}
				}
			}

			switch v := m["groups"].(type) {
			case []any:
				admin.Controller.Groups.FromMap(v)
				err = admin.Controller.Groups.Write(admin.Controller.Database)
				if err != nil {
					logError(err)
				} else {
					err = admin.Controller.Groups.Read(admin.Controller.Database)
					if err != nil {
						logError(err)
					}
				}
			}

			switch v := m["options"].(type) {
			case map[string]any:
				admin.Controller.Options.FromMap(v)
				err = admin.Controller.Options.Write(admin.Controller.Database)
				if err != nil {
					logError(err)
				}
			}

			switch v := m["systems"].(type) {
			case []any:
				admin.Controller.Systems.FromMap(v)
				err = admin.Controller.Systems.Write(admin.Controller.Database)
				if err != nil {
					logError(err)
				} else {
					err = admin.Controller.Systems.Read(admin.Controller.Database)
					if err != nil {
						logError(err)
					}
				}
			}

			switch v := m["tags"].(type) {
			case []any:
				admin.Controller.Tags.FromMap(v)
				err = admin.Controller.Tags.Write(admin.Controller.Database)
				if err != nil {
					logError(err)
				} else {
					err = admin.Controller.Tags.Read(admin.Controller.Database)
					if err != nil {
						logError(err)
					}
				}
			}

			admin.Controller.EmitConfig()
			admin.Controller.Dirwatches.Start(admin.Controller)

			admin.SendConfig(w)

			admin.Controller.Logs.LogEvent(LogLevelWarn, "configuration changed")

		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

func (admin *Admin) GetAuthorization(r *http.Request) string {
	return r.Header.Get("Authorization")
}

func (admin *Admin) GetConfig() map[string]any {
	systems := []map[string]any{}
	for _, system := range admin.Controller.Systems.List {
		systems = append(systems, map[string]any{
			"_id":          system.RowId,
			"autoPopulate": system.AutoPopulate,
			"blacklists":   system.Blacklists,
			"id":           system.Id,
			"label":        system.Label,
			"led":          system.Led,
			"order":        system.Order,
			"talkgroups":   system.Talkgroups.List,
			"units":        system.Units.List,
		})
	}

	return map[string]any{
		"access":      admin.Controller.Accesses.List,
		"apiKeys":     admin.Controller.Apikeys.List,
		"dirWatch":    admin.Controller.Dirwatches.List,
		"downstreams": admin.Controller.Downstreams.List,
		"groups":      admin.Controller.Groups.List,
		"options":     admin.Controller.Options,
		"systems":     systems,
		"tags":        admin.Controller.Tags.List,
	}
}

func (admin *Admin) LogsHandler(w http.ResponseWriter, r *http.Request) {
	t := admin.GetAuthorization(r)
	if !admin.ValidateToken(t) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodPost:
		m := map[string]any{}
		err := json.NewDecoder(r.Body).Decode(&m)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		logOptions := NewLogSearchOptions().FromMap(m)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		r, err := admin.Controller.Logs.Search(logOptions, admin.Controller.Database)
		if err != nil {
			admin.Controller.Logs.LogEvent(LogLevelError, err.Error())
			w.WriteHeader(http.StatusExpectationFailed)
			return
		}

		b, err := json.Marshal(r)
		if err != nil {
			admin.Controller.Logs.LogEvent(LogLevelError, err.Error())
			w.WriteHeader(http.StatusExpectationFailed)
			return
		}

		w.Write(b)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (admin *Admin) LoginHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		m := map[string]any{}

		if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		remoteAddr := GetRemoteAddr(r, admin.Controller.Config.TrustProxy)

		attempt := admin.Attempts[remoteAddr]

		if attempt != nil && attempt.Count >= admin.AttemptsMax && time.Since(attempt.Date) < admin.AttemptsMaxDelay {
			if attempt.Count == admin.AttemptsMax {
				attempt.Count++
				admin.Controller.Logs.LogEvent(LogLevelWarn, fmt.Sprintf("too many login attempts for ip=\"%v\"", remoteAddr))
			}

			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		if attempt == nil || time.Since(attempt.Date) >= admin.AttemptsMaxDelay {
			admin.Attempts[remoteAddr] = &AdminLoginAttempt{
				Count: 1,
				Date:  time.Now(),
			}
			attempt = admin.Attempts[remoteAddr]
		} else {
			attempt.Count++
			attempt.Date = time.Now()
		}

		ok := false

		switch v := m["password"].(type) {
		case string:
			if len(v) > 0 {
				if err := bcrypt.CompareHashAndPassword([]byte(admin.Controller.Options.adminPassword), []byte(v)); err == nil {
					ok = true
				}
			}
		}

		if !ok {
			admin.Controller.Logs.LogEvent(LogLevelWarn, fmt.Sprintf("invalid login attempt for ip %v", remoteAddr))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		delete(admin.Attempts, remoteAddr)

		id, err := uuid.NewRandom()

		if err != nil {
			w.WriteHeader(http.StatusExpectationFailed)
			return
		}

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
			ID:        id.String(),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(adminTokenLifetime)),
		})
		sToken, err := token.SignedString([]byte(admin.Controller.Options.secret))

		if err != nil {
			w.WriteHeader(http.StatusExpectationFailed)
			return
		}

		admin.mutex.Lock()
		if len(admin.Tokens) < 5 {
			admin.Tokens = append(admin.Tokens, sToken)
		} else {
			admin.Tokens = append(admin.Tokens[1:], sToken)
		}
		admin.mutex.Unlock()

		b, err := json.Marshal(map[string]any{
			"passwordNeedChange": admin.Controller.Options.adminPasswordNeedChange,
			"token":              sToken,
		})
		if err != nil {
			w.WriteHeader(http.StatusExpectationFailed)
			return
		}

		for k, v := range admin.Attempts {
			if time.Since(v.Date) > admin.AttemptsMaxDelay {
				delete(admin.Attempts, k)
			}
		}

		w.Write(b)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (admin *Admin) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		t := admin.GetAuthorization(r)
		if !admin.ValidateToken(t) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		admin.mutex.Lock()
		for k, v := range admin.Tokens {
			if v == t {
				admin.Tokens = append(admin.Tokens[:k], admin.Tokens[k+1:]...)
				break
			}
		}
		admin.mutex.Unlock()
		w.WriteHeader(http.StatusOK)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (admin *Admin) PasswordHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var (
			b               []byte
			currentPassword any
			newPassword     string
		)

		logError := func(err error) {
			admin.Controller.Logs.LogEvent(LogLevelError, fmt.Sprintf("admin.passwordhandler.post: %s", err.Error()))
		}

		t := admin.GetAuthorization(r)
		if !admin.ValidateToken(t) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		m := map[string]any{}
		err := json.NewDecoder(r.Body).Decode(&m)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		switch v := m["currentPassword"].(type) {
		case string:
			currentPassword = v
		}

		switch v := m["newPassword"].(type) {
		case string:
			newPassword = v
		default:
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if err = admin.ChangePassword(currentPassword, newPassword); err != nil {
			logError(errors.New("unable to change admin password, current password is invalid"))
			w.WriteHeader(http.StatusExpectationFailed)
			return
		}

		if b, err = json.Marshal(map[string]any{"passwordNeedChange": admin.Controller.Options.adminPasswordNeedChange}); err == nil {
			w.Write(b)
		} else {
			w.WriteHeader(http.StatusExpectationFailed)
		}

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (admin *Admin) SendConfig(w http.ResponseWriter) {
	var m map[string]any
	_, docker := os.LookupEnv("DOCKER")
	if docker {
		m = map[string]any{
			"config":             admin.GetConfig(),
			"docker":             docker,
			"passwordNeedChange": admin.Controller.Options.adminPasswordNeedChange,
		}
	} else {
		m = map[string]any{
			"config":             admin.GetConfig(),
			"passwordNeedChange": admin.Controller.Options.adminPasswordNeedChange,
		}
	}
	if b, err := json.Marshal(m); err == nil {
		w.Write(b)
	} else {
		w.WriteHeader(http.StatusExpectationFailed)
	}
}

func (admin *Admin) Start() error {
	if admin.running {
		return errors.New("admin already running")
	} else {
		admin.running = true
	}

	go func() {
		for {
			select {
			case data, ok := <-admin.Broadcast:
				if !ok {
					return
				}

				for conn := range admin.Conns {
					err := conn.WriteMessage(websocket.TextMessage, *data)
					if err != nil {
						admin.Unregister <- conn
					}
				}

			case conn := <-admin.Register:
				admin.Conns[conn] = true

			case conn := <-admin.Unregister:
				if _, ok := admin.Conns[conn]; ok {
					delete(admin.Conns, conn)
					conn.Close()
				}
			}
		}
	}()

	return nil
}

func (admin *Admin) DatabaseCompactHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		t := admin.GetAuthorization(r)
		if !admin.ValidateToken(t) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		writeError := func(status int, msg string) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			b, _ := json.Marshal(map[string]any{"error": msg})
			w.Write(b)
		}

		db := admin.Controller.Database
		if db == nil || db.Sql == nil {
			writeError(http.StatusServiceUnavailable, "database not initialized")
			return
		}

		// Vacuuming a busy SQLite database can fail with "database is locked".
		// Serialize against everything else by holding the admin mutex.
		admin.mutex.Lock()
		defer admin.mutex.Unlock()

		started := time.Now()

		switch db.Config.DbType {
		case DbTypeSqlite:
			if _, err := db.Sql.Exec("VACUUM"); err != nil {
				admin.Controller.Logs.LogEvent(LogLevelError, fmt.Sprintf("admin.database.compact: %s", err.Error()))
				writeError(http.StatusInternalServerError, err.Error())
				return
			}

		case DbTypeMariadb, DbTypeMysql:
			tables := []string{
				"rdioScannerAccesses", "rdioScannerApiKeys", "rdioScannerCalls",
				"rdioScannerConfigs", "rdioScannerDirWatches", "rdioScannerDownstreams",
				"rdioScannerGroups", "rdioScannerLogs", "rdioScannerMeta",
				"rdioScannerSystems", "rdioScannerTags", "rdioScannerTalkgroups",
				"rdioScannerUnits",
			}
			for _, tbl := range tables {
				if _, err := db.Sql.Exec(fmt.Sprintf("OPTIMIZE TABLE `%s`", tbl)); err != nil {
					admin.Controller.Logs.LogEvent(LogLevelError, fmt.Sprintf("admin.database.compact %s: %s", tbl, err.Error()))
					writeError(http.StatusInternalServerError, err.Error())
					return
				}
			}

		default:
			writeError(http.StatusNotImplemented, fmt.Sprintf("compact not supported for db type %q", db.Config.DbType))
			return
		}

		elapsed := time.Since(started).Round(time.Millisecond)
		admin.Controller.Logs.LogEvent(LogLevelInfo, fmt.Sprintf("admin.database.compact: completed in %s", elapsed))

		w.Header().Set("Content-Type", "application/json")
		b, _ := json.Marshal(map[string]any{
			"ok":         true,
			"durationMs": elapsed.Milliseconds(),
			"engine":     db.Config.DbType,
		})
		w.Write(b)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (admin *Admin) DatabasePruneHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		t := admin.GetAuthorization(r)
		if !admin.ValidateToken(t) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		writeError := func(status int, msg string) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			b, _ := json.Marshal(map[string]any{"error": msg})
			w.Write(b)
		}

		body := map[string]any{}
		if r.Body != nil {
			_ = json.NewDecoder(io.LimitReader(r.Body, 1<<14)).Decode(&body)
		}

		days := admin.Controller.Options.PruneDays
		if v, ok := body["days"].(float64); ok && v >= 0 {
			days = uint(v)
		}

		if err := admin.Controller.Calls.Prune(admin.Controller.Database, days); err != nil {
			admin.Controller.Logs.LogEvent(LogLevelError, fmt.Sprintf("admin.database.prune: %s", err.Error()))
			writeError(http.StatusInternalServerError, err.Error())
			return
		}

		admin.Controller.Logs.LogEvent(LogLevelInfo, fmt.Sprintf("admin.database.prune: removed calls older than %d day(s)", days))

		w.Header().Set("Content-Type", "application/json")
		b, _ := json.Marshal(map[string]any{"ok": true, "days": days})
		w.Write(b)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (admin *Admin) RadioReferenceTalkgroupsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		t := admin.GetAuthorization(r)
		if !admin.ValidateToken(t) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		m := map[string]any{}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<14)).Decode(&m); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		creds := &RadioReferenceCredentials{}
		if v, ok := m["username"].(string); ok {
			creds.Username = strings.TrimSpace(v)
		}
		if v, ok := m["password"].(string); ok {
			creds.Password = v
		}
		if v, ok := m["appKey"].(string); ok {
			creds.AppKey = strings.TrimSpace(v)
		}

		var sid uint
		switch v := m["sid"].(type) {
		case float64:
			if v > 0 {
				sid = uint(v)
			}
		case string:
			s := strings.TrimSpace(v)
			if s != "" {
				if n, err := strconvAtoiUint(s); err == nil {
					sid = n
				}
			}
		}

		if creds.Username == "" || creds.Password == "" || creds.AppKey == "" || sid == 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			b, _ := json.Marshal(map[string]any{"error": "username, password, appKey and sid are required"})
			w.Write(b)
			return
		}

		talkgroups, err := RadioReferenceImportTalkgroups(creds, sid)
		if err != nil {
			admin.Controller.Logs.LogEvent(LogLevelWarn, fmt.Sprintf("admin.radioreference.import: %s", err.Error()))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			b, _ := json.Marshal(map[string]any{"error": err.Error()})
			w.Write(b)
			return
		}

		out := make([]map[string]any, 0, len(talkgroups))
		for _, tg := range talkgroups {
			out = append(out, map[string]any{
				"id":          tg.Dec,
				"hex":         tg.Hex,
				"alphaTag":    tg.Alpha,
				"mode":        tg.Mode,
				"description": tg.Descr,
				"tag":         tg.Tag,
				"group":       tg.Category,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		b, _ := json.Marshal(map[string]any{"talkgroups": out})
		w.Write(b)

		admin.Controller.Logs.LogEvent(LogLevelInfo, fmt.Sprintf("imported %d talkgroups from Radio Reference sid=%d", len(out), sid))

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (admin *Admin) UserAddHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		logError := func(err error) {
			admin.Controller.Logs.LogEvent(LogLevelError, fmt.Sprintf("admin.useraddhandler.post: %s", err.Error()))
		}

		t := admin.GetAuthorization(r)
		if !admin.ValidateToken(t) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		m := map[string]any{}
		err := json.NewDecoder(r.Body).Decode(&m)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		admin.Controller.Accesses.Add(NewAccess().FromMap(m))

		if err := admin.Controller.Accesses.Write(admin.Controller.Database); err == nil {
			if err := admin.Controller.Accesses.Read(admin.Controller.Database); err == nil {
				admin.BroadcastConfig()
				w.WriteHeader(http.StatusOK)
			} else {
				logError(err)
				w.WriteHeader(http.StatusExpectationFailed)
			}
		} else {
			logError(err)
			w.WriteHeader(http.StatusExpectationFailed)
		}

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (admin *Admin) UserRemoveHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		logError := func(err error) {
			admin.Controller.Logs.LogEvent(LogLevelError, fmt.Sprintf("admin.userremovehandler.post: %s", err.Error()))
		}

		t := admin.GetAuthorization(r)
		if !admin.ValidateToken(t) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		m := map[string]any{}
		err := json.NewDecoder(r.Body).Decode(&m)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if _, ok := admin.Controller.Accesses.Remove(NewAccess().FromMap(m)); ok {
			if err := admin.Controller.Accesses.Write(admin.Controller.Database); err == nil {
				if err := admin.Controller.Accesses.Read(admin.Controller.Database); err == nil {
					admin.BroadcastConfig()
					w.WriteHeader(http.StatusOK)
				} else {
					logError(err)
					w.WriteHeader(http.StatusExpectationFailed)
				}
			} else {
				logError(err)
				w.WriteHeader(http.StatusExpectationFailed)
			}
		}

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (admin *Admin) ValidateToken(sToken string) bool {
	if sToken == "" {
		return false
	}

	// Constant-time match across the issued-tokens slice. The previous ==
	// compare leaked which prefix matched on each attempt, letting an
	// attacker recover an active token byte-by-byte against the linear
	// scan. We OR the bit results so the loop runtime depends only on
	// len(Tokens), not on which one matched.
	candidate := []byte(sToken)
	var match int
	admin.mutex.Lock()
	for _, t := range admin.Tokens {
		if len(t) != len(sToken) {
			// ConstantTimeCompare returns 0 on length mismatch but
			// also takes a length-dependent path; skipping is fine
			// because length-mismatched tokens cannot match.
			continue
		}
		match |= subtle.ConstantTimeCompare(candidate, []byte(t))
	}
	admin.mutex.Unlock()
	if match != 1 {
		return false
	}

	token, err := jwt.Parse(sToken, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		return []byte(admin.Controller.Options.secret), nil
	})
	if err != nil {
		return false
	}

	return token.Valid
}

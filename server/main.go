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
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	const defaultAddr = "0.0.0.0"

	var (
		addr     string
		port     string
		hostname string
		sslAddr  string
		sslPort  string
	)

	config := NewConfig()

	if config.newAdminPassword == "" {
		fmt.Printf("\nRdio Scanner v%s\n", Version)
		fmt.Printf("----------------------------------\n")
	}

	controller := NewController(config)

	// Resolve -admin_password_file before -admin_password so that a
	// file-supplied secret wins. The file path keeps the password off
	// the process command line (-admin_password is visible in ps).
	if config.newAdminPasswordFile != "" {
		b, err := os.ReadFile(config.newAdminPasswordFile)
		if err != nil {
			log.Fatalf("failed to read admin password file: %v", err)
		}
		config.newAdminPassword = strings.TrimSpace(string(b))
		if config.newAdminPassword == "" {
			log.Fatal("admin password file is empty")
		}
	} else if config.newAdminPassword != "" {
		log.Println("warning: -admin_password is visible in the process list; prefer -admin_password_file")
	}

	if config.newAdminPassword != "" {
		if hash, err := bcrypt.GenerateFromPassword([]byte(config.newAdminPassword), bcrypt.DefaultCost); err == nil {
			if err := controller.Options.Read(controller.Database); err != nil {
				log.Fatal(err)
			}

			controller.Options.adminPassword = string(hash)
			controller.Options.adminPasswordNeedChange = config.newAdminPassword == defaults.adminPassword

			if err := controller.Options.Write(controller.Database); err != nil {
				log.Fatal(err)
			}

			controller.Logs.LogEvent(LogLevelInfo, "admin password changed.")

			os.Exit(0)

		} else {
			log.Fatal(err)
		}
	}

	if err := controller.Start(); err != nil {
		log.Fatal(err)
	}

	if h, err := os.Hostname(); err == nil {
		hostname = h
	} else {
		hostname = defaultAddr
	}

	if s := strings.Split(config.Listen, ":"); len(s) > 1 {
		addr = s[0]
		port = s[1]
	} else {
		addr = s[0]
		port = "3000"
	}
	if len(addr) == 0 {
		addr = defaultAddr
	}

	if s := strings.Split(config.SslListen, ":"); len(s) > 1 {
		sslAddr = s[0]
		sslPort = s[1]
	} else {
		sslAddr = s[0]
		sslPort = "3000"
	}
	if len(sslAddr) == 0 {
		sslAddr = defaultAddr
	}

	http.HandleFunc("/api/admin/alerts", controller.Admin.AlertsHandler)

	http.HandleFunc("/api/admin/config", controller.Admin.ConfigHandler)

	http.HandleFunc("/api/admin/login", controller.Admin.LoginHandler)

	http.HandleFunc("/api/admin/logout", controller.Admin.LogoutHandler)

	http.HandleFunc("/api/admin/logs", controller.Admin.LogsHandler)

	http.HandleFunc("/api/admin/password", controller.Admin.PasswordHandler)

	http.HandleFunc("/api/admin/user-add", controller.Admin.UserAddHandler)

	http.HandleFunc("/api/admin/user-remove", controller.Admin.UserRemoveHandler)

	http.HandleFunc("/api/call-upload", controller.Api.CallUploadHandler)

	http.HandleFunc("/api/trunk-recorder-call-upload", controller.Api.TrunkRecorderCallUploadHandler)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		url := r.URL.Path[1:]

		if strings.EqualFold(r.Header.Get("upgrade"), "websocket") {
			upgrader := websocket.Upgrader{
				CheckOrigin:     sameOriginCheck,
				ReadBufferSize:  1024,
				WriteBufferSize: 1024,
			}

			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				log.Println(err)
			}

			client := &Client{}
			if err = client.Init(controller, r, conn); err != nil {
				log.Println(err)
			}

		} else {
			if url == "" {
				url = "index.html"
			}

			if b, err := webapp.ReadFile(path.Join("webapp", url)); err == nil {
				var t string
				switch path.Ext(url) {
				case ".js":
					t = "text/javascript" // see https://github.com/golang/go/issues/32350
				default:
					t = mime.TypeByExtension(path.Ext(url))
				}
				w.Header().Set("Content-Type", t)
				w.Write(b)

			} else if url[:len(url)-1] != "/" {
				if b, err := webapp.ReadFile("webapp/index.html"); err == nil {
					w.Write(b)

				} else {
					w.WriteHeader(http.StatusNotFound)
				}

			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		}
	})

	if port == "80" {
		log.Printf("main interface at http://%s", hostname)
	} else {
		log.Printf("main interface at http://%s:%s", hostname, port)
	}

	sslPrintInfo := func() {
		if sslPort == "443" {
			log.Printf("main interface at https://%s", hostname)
			log.Printf("admin interface at https://%s/admin", hostname)

		} else {
			log.Printf("main interface at https://%s:%s", hostname, sslPort)
			log.Printf("admin interface at https://%s:%s/admin", hostname, sslPort)
		}
	}

	// hardenedTLSConfig sets a minimum of TLS 1.2 and lets Go's defaults
	// pick the cipher suites (which exclude anything weak as of recent
	// releases). If a caller passed a base config (e.g. autocert), we
	// merge on top of it.
	hardenedTLSConfig := func(base *tls.Config) *tls.Config {
		if base == nil {
			base = &tls.Config{}
		}
		if base.MinVersion == 0 {
			base.MinVersion = tls.VersionTLS12
		}
		return base
	}

	newServer := func(addr string, tlsConfig *tls.Config, handler http.Handler) *http.Server {
		s := &http.Server{
			Addr:         addr,
			Handler:      handler,
			TLSConfig:    tlsConfig,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
			ErrorLog:     log.New(io.Discard, "", 0),
		}

		s.SetKeepAlivesEnabled(true)

		return s
	}

	// hstsWrapper wraps a handler to add Strict-Transport-Security on every
	// response served over TLS. Only attached to the HTTPS listener.
	hstsWrapper := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			h.ServeHTTP(w, r)
		})
	}

	// httpsRedirectHandler unconditionally redirects the request to the
	// configured TLS listener, preserving path and query. Used as the
	// HTTP handler when TLS is enabled so /api/admin/login can't be
	// answered cleartext alongside the encrypted route.
	httpsRedirectHandler := func() http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host := r.Host
			if h, _, err := net.SplitHostPort(host); err == nil {
				host = h
			}
			target := "https://" + host
			if sslPort != "443" {
				target += ":" + sslPort
			}
			target += r.URL.RequestURI()
			http.Redirect(w, r, target, http.StatusMovedPermanently)
		})
	}

	tlsEnabled := (len(config.SslCertFile) > 0 && len(config.SslKeyFile) > 0) || config.SslAutoCert != ""

	if len(config.SslCertFile) > 0 && len(config.SslKeyFile) > 0 {
		go func() {
			sslPrintInfo()

			sslCert := config.GetSslCertFilePath()
			sslKey := config.GetSslKeyFilePath()

			server := newServer(fmt.Sprintf("%s:%s", sslAddr, sslPort), hardenedTLSConfig(nil), hstsWrapper(http.DefaultServeMux))

			if err := server.ListenAndServeTLS(sslCert, sslKey); err != nil {
				log.Fatal(err)
			}
		}()

	} else if config.SslAutoCert != "" {
		go func() {
			sslPrintInfo()

			manager := &autocert.Manager{
				Cache:      autocert.DirCache("autocert"),
				Prompt:     autocert.AcceptTOS,
				HostPolicy: autocert.HostWhitelist(config.SslAutoCert),
			}

			server := newServer(fmt.Sprintf("%s:%s", sslAddr, sslPort), hardenedTLSConfig(manager.TLSConfig()), hstsWrapper(http.DefaultServeMux))

			if err := server.ListenAndServeTLS("", ""); err != nil {
				log.Fatal(err)
			}
		}()

	} else if port == "80" {
		log.Printf("admin interface at http://%s/admin", hostname)

	} else {
		log.Printf("admin interface at http://%s:%s/admin", hostname, port)
	}

	log.Println("please consider sponsoring the project at https://github.com/sponsors/chuot")

	// When TLS is enabled, the plaintext listener becomes a redirect-only
	// endpoint so /api/admin/login (and listener PIN entry) cannot be
	// answered over cleartext alongside the encrypted route.
	var httpHandler http.Handler = http.DefaultServeMux
	if tlsEnabled {
		log.Printf("plaintext listener will 301-redirect to https://%s:%s", hostname, sslPort)
		httpHandler = httpsRedirectHandler()
	}
	server := newServer(fmt.Sprintf("%s:%s", addr, port), nil, httpHandler)

	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

// sameOriginCheck blocks cross-origin WebSocket upgrades. The Gorilla
// upgrader's default is to reject when Origin != Host; this restores that
// behavior (the previous implementation unconditionally returned true,
// allowing cross-site WebSocket hijacking against authenticated users).
// Requests without an Origin header (curl, server-to-server) are allowed.
func sameOriginCheck(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

// GetRemoteAddr returns the caller's IP address. X-Forwarded-For is only
// honored when the direct TCP peer is on a loopback or RFC1918 network —
// i.e. when the server is plausibly behind a reverse proxy. Public-internet
// peers asserting X-Forwarded-For are ignored, because the prior unconditional
// trust let attackers spoof their IP to evade rate limiters.
func GetRemoteAddr(r *http.Request) string {
	directHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		directHost = r.RemoteAddr
	}

	if isTrustedProxyHost(directHost) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// The leftmost entry is the original client. Trim ports and whitespace.
			first := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
			if host, _, splitErr := net.SplitHostPort(first); splitErr == nil {
				first = host
			}
			if first != "" {
				return first
			}
		}
	}

	return directHost
}

// isTrustedProxyHost reports whether a TCP peer address can be trusted to
// have set X-Forwarded-For honestly. Conservative default: loopback +
// RFC1918 + IPv6 ULA. Operators with proxies outside these ranges should
// terminate TLS at the proxy and let it rewrite RemoteAddr.
func isTrustedProxyHost(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		return true
	}
	return false
}

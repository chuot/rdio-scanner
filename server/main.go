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
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"log"
	"mime"
	"net/http"
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

	controller := NewController(config)

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

			controller.Logs.LogEvent(controller.Database, LogLevelInfo, "admin password changed.")

			os.Exit(0)

		} else {
			log.Fatal(err)
		}
	}

	fmt.Printf("\nRdio Scanner v%s\n", Version)
	fmt.Printf("----------------------------------\n")

	if err := controller.Start(); err != nil {
		log.Fatal(err)
	}

	if h, err := os.Hostname(); err == nil {
		hostname = h
	} else {
		hostname = defaultAddr
	}

	if s := strings.Split(controller.Config.Listen, ":"); len(s) > 1 {
		addr = s[0]
		port = s[1]
	} else {
		addr = s[0]
		port = "3000"
	}
	if len(addr) == 0 {
		addr = defaultAddr
	}

	if s := strings.Split(controller.Config.SslListen, ":"); len(s) > 1 {
		sslAddr = s[0]
		sslPort = s[1]
	} else {
		sslAddr = s[0]
		sslPort = "3000"
	}
	if len(sslAddr) == 0 {
		sslAddr = defaultAddr
	}

	http.HandleFunc("/api/admin/config", controller.Admin.ConfigHandler)

	http.HandleFunc("/api/admin/login", controller.Admin.LoginHandler)

	http.HandleFunc("/api/admin/logout", controller.Admin.LogoutHandler)

	http.HandleFunc("/api/admin/logs", controller.Admin.LogsHandler)

	http.HandleFunc("/api/admin/password", controller.Admin.PasswordHandler)

	http.HandleFunc("/api/call-upload", controller.Api.CallUploadHandler)

	http.HandleFunc("/api/trunk-recorder-call-upload", controller.Api.TrunkRecorderCallUploadHandler)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		url := r.URL.Path[1:]

		if strings.EqualFold(r.Header.Get("upgrade"), "websocket") {
			upgrader := websocket.Upgrader{
				CheckOrigin: func(r *http.Request) bool {
					return true
				},
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

	newServer := func(addr string, tlsConfig *tls.Config) *http.Server {
		s := &http.Server{
			Addr:         addr,
			TLSConfig:    tlsConfig,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
			ErrorLog:     log.New(ioutil.Discard, "", 0),
		}

		s.SetKeepAlivesEnabled(true)

		return s
	}

	if len(controller.Config.SslCertFile) > 0 && len(controller.Config.SslKeyFile) > 0 {
		go func() {
			sslPrintInfo()

			sslCert := controller.Config.GetSslCertFilePath()
			sslKey := controller.Config.GetSslKeyFilePath()

			server := newServer(fmt.Sprintf("%s:%s", sslAddr, sslPort), nil)

			if err := server.ListenAndServeTLS(sslCert, sslKey); err != nil {
				log.Fatal(err)
			}
		}()

	} else if controller.Config.SslAutoCert != "" {
		go func() {
			sslPrintInfo()

			manager := &autocert.Manager{
				Cache:      autocert.DirCache("autocert"),
				Prompt:     autocert.AcceptTOS,
				HostPolicy: autocert.HostWhitelist(controller.Config.SslAutoCert),
			}

			server := newServer(fmt.Sprintf("%s:%s", sslAddr, sslPort), manager.TLSConfig())

			if err := server.ListenAndServeTLS("", ""); err != nil {
				log.Fatal(err)
			}
		}()

	} else if port == "80" {
		log.Printf("admin interface at http://%s/admin", hostname)

	} else {
		log.Printf("admin interface at http://%s:%s/admin", hostname, port)
	}

	server := newServer(fmt.Sprintf("%s:%s", addr, port), nil)

	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func GetRemoteAddr(r *http.Request) string {
	for _, addr := range strings.Split(r.Header.Get("X-Forwarded-For"), ",") {
		if ip := strings.Split(addr, ":"); len(ip[0]) > 0 {
			return ip[0]
		}
	}

	if ip := strings.Split(r.RemoteAddr, ":"); len(ip[0]) > 0 {
		return ip[0]
	}

	return "unknown"
}

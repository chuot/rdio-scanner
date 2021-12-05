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
	"fmt"
	"log"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"

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

	config := &Config{}
	ok, err := config.Init()
	if err != nil {
		log.Fatal(err)
	}
	if !ok {
		os.Exit(0)
	}

	database := &Database{}
	if err = database.Init(config); err != nil {
		log.Fatal(err)
	}

	controller := &Controller{}
	if err = controller.Init(config, database); err != nil {
		log.Fatal(err)
	}

	if config.newAdminPassword != "" {
		if hash, err := bcrypt.GenerateFromPassword([]byte(config.newAdminPassword), bcrypt.DefaultCost); err == nil {
			controller.Options.adminPassword = string(hash)
			controller.Options.adminPasswordNeedChange = config.newAdminPassword == defaults.adminPassword

			if err := controller.Options.Write(controller.Database); err != nil {
				log.Fatal(err)
			}

			if err := controller.Options.Read(controller.Database); err != nil {
				log.Fatal(err)
			}

			LogEvent(controller.Database, LogLevelInfo, "admin password changed.")

			os.Exit(0)

		} else {
			log.Fatal(err)
		}
	}

	fmt.Printf("\nRdio Scanner v%s\n", Version)
	fmt.Printf("----------------------------------\n")

	LogEvent(controller.Database, LogLevelWarn, "server started")

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

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		url := r.URL.Path[1:]

		switch url {
		case AdminUrlConfig:
			controller.Admin.ConfigHandler(w, r)

		case AdminUrlLogin:
			controller.Admin.LoginHandler(w, r)

		case AdminUrlLogout:
			controller.Admin.LogoutHandler(w, r)

		case AdminUrlLogs:
			controller.Admin.LogsHandler(w, r)

		case AdminUrlPassword:
			controller.Admin.PasswordHandler(w, r)

		case ApiUrlCallUpload:
			controller.Api.CallUploadHandler(w, r)

		case ApiUrlTrunkRecorderCallUpload:
			controller.Api.TrunkRecorderCallUploadHandler(w, r)

		default:
			if strings.EqualFold(r.Header.Get("upgrade"), "websocket") {
				upgrader := websocket.Upgrader{}

				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					log.Println(err)
				}

				client := &Client{}
				if err = client.Init(controller, conn); err != nil {
					log.Println(err)
				}

			} else {
				if url == "" {
					url = "index.html"
				}

				if b, err := webapp.ReadFile(path.Join("webapp", url)); err == nil {
					w.Header().Set("Content-Type", mime.TypeByExtension(path.Ext(url)))
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

	if len(controller.Config.SslCertFile) > 0 && len(controller.Config.SslKeyFile) > 0 {
		go func() {
			sslPrintInfo()

			sslCert := controller.Config.GetSslCertFilePath()
			sslKey := controller.Config.GetSslKeyFilePath()

			if err := http.ListenAndServeTLS(fmt.Sprintf("%s:%s", sslAddr, sslPort), sslCert, sslKey, nil); err != nil {
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

			server := &http.Server{
				Addr:      fmt.Sprintf("%s:%s", sslAddr, sslPort),
				TLSConfig: manager.TLSConfig(),
			}

			if err := server.ListenAndServeTLS("", ""); err != nil {
				log.Fatal(err)
			}
		}()

	} else if port == "80" {
		log.Printf("admin interface at http://%s/admin", hostname)

	} else {
		log.Printf("admin interface at http://%s:%s/admin", hostname, port)
	}

	if err := http.ListenAndServe(fmt.Sprintf("%s:%s", addr, port), nil); err != nil {
		log.Fatal(err)
	}
}

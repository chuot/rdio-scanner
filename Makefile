################################################################################
## Copyright (C) 2019-2021 Chrystian Huot <chrystian.huot@saubeo.solutions>
##
## This program is free software: you can redistribute it and/or modify
## it under the terms of the GNU General Public License as published by
## the Free Software Foundation, either version 3 of the License, or
## (at your option) any later version.
##
## This program is distributed in the hope that it will be useful,
## but WITHOUT ANY WARRANTY; without even the implied warranty of
## MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
## GNU General Public License for more details.
##
## You should have received a copy of the GNU General Public License
## along with this program.  If not, see <http://www.gnu.org/licenses/>
################################################################################

app := rdio-scanner
ver := 6.0.3

client := $(wildcard client/*.json client/*.ts)
server := $(wildcard server/*.go)

build = @cd server && GOOS=$(1) GOARCH=$(2) go build -o ../dist/$(1)-$(2)/$(3)
pandoc = @test -d dist/$(1)-$(2) || mkdir -p dist/$(1)-$(2) && pandoc -f markdown -o dist/$(1)-$(2)/$(3) --resource-path docs:docs/platforms $(4) docs/webapp.md CHANGELOG.md
zip = @cd dist/$(1)-$(2) && zip -q ../$(app)-$(1)-$(2)-v$(ver).zip * && cd .. && rm -fr $(1)-$(2)

.PHONY: all clean dist setver
.PHONY: darwin darwin-amd64 darwin-arm64
.PHONY: freebsd freebsd-amd64
.PHONY: linux linux-386 linux-amd64 linux-arm linux-arm64
.PHONY: windows windows-amd64

all: clean dist

clean:
	@rm -fr client/node_modules dist server/webapp

dist: darwin freebsd linux windows

setver:
	@sed -i -re "s/^(\s*\"version\":).*$$/\1 \"$(ver)\"/" client/package.json
	@sed -i -re "s/^(const\s+Version\s+=).*$$/\1 \"$(ver)\"/" server/version.go
	@sed -i -re "s/v[0-9]\.[0-9]\.[0-9]/v$(ver)/" docs/platforms/*.md

webapp: server/webapp/index.html

server/webapp/index.html: $(client)
	@cd client && test -d node_modules || npm ci --loglevel=error --no-progress
	@cd client && npm run build

darwin: darwin-amd64 darwin-arm64
darwin-amd64: webapp dist/$(app)-darwin-amd64-v$(ver).zip
darwin-arm64: webapp dist/$(app)-darwin-arm64-v$(ver).zip

dist/$(app)-darwin-amd64-v$(ver).zip: $(server)
	$(call pandoc,darwin,amd64,rdio-scanner.pdf,docs/platforms/darwin.md,CHANGELOG.md)
	$(call build,darwin,amd64,$(app))
	$(call zip,darwin,amd64,$(app))

dist/$(app)-darwin-arm64-v$(ver).zip: $(server)
	$(call pandoc,darwin,arm64,rdio-scanner.pdf,docs/platforms/darwin.md,CHANGELOG.md)
	$(call build,darwin,arm64,$(app))
	$(call zip,darwin,arm64,$(app))

freebsd: freebsd-amd64
freebsd-amd64: webapp dist/$(app)-freebsd-amd64-v$(ver).zip

dist/$(app)-freebsd-amd64-v$(ver).zip: $(server)
	$(call pandoc,freebsd,amd64,rdio-scanner.pdf,docs/platforms/freebsd.md,CHANGELOG.md)
	$(call build,freebsd,amd64,$(app))
	$(call zip,freebsd,amd64,$(app))

linux: linux-386 linux-amd64 linux-arm linux-arm64
linux-386: webapp dist/$(app)-linux-386-v$(ver).zip
linux-amd64: webapp dist/$(app)-linux-amd64-v$(ver).zip
linux-arm: webapp dist/$(app)-linux-arm-v$(ver).zip
linux-arm64: webapp dist/$(app)-linux-arm64-v$(ver).zip

dist/$(app)-linux-386-v$(ver).zip: $(server)
	$(call pandoc,linux,386,rdio-scanner.pdf,docs/platforms/linux.md,CHANGELOG.md)
	$(call build,linux,386,$(app))
	$(call zip,linux,386,$(app))

dist/$(app)-linux-amd64-v$(ver).zip: $(server)
	$(call pandoc,linux,amd64,rdio-scanner.pdf,docs/platforms/linux.md,CHANGELOG.md)
	$(call build,linux,amd64,$(app))
	$(call zip,linux,amd64,$(app))

dist/$(app)-linux-arm-v$(ver).zip: $(server)
	$(call pandoc,linux,arm,rdio-scanner.pdf,docs/platforms/linux.md,CHANGELOG.md)
	$(call build,linux,arm,$(app))
	$(call zip,linux,arm,$(app))

dist/$(app)-linux-arm64-v$(ver).zip: $(server)
	$(call pandoc,linux,arm64,rdio-scanner.pdf,docs/platforms/linux.md,CHANGELOG.md)
	$(call build,linux,arm64,$(app))
	$(call zip,linux,arm64,$(app))

windows: windows-amd64
windows-amd64: webapp dist/$(app)-windows-amd64-v$(ver).zip

dist/$(app)-windows-amd64-v$(ver).zip: $(server)
	$(call pandoc,windows,amd64,rdio-scanner.pdf,docs/platforms/windows.md,CHANGELOG.md)
	$(call build,windows,amd64,$(app).exe)
	$(call zip,windows,amd64,$(app))

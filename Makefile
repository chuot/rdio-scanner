################################################################################
## Copyright (C) 2019-2026 Chrystian Huot <chrystian.huot@saubeo.solutions>
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
date := 2026/05/16
ver := 7.0.0-dev

clientSrc := $(wildcard internal/presentation/clients/*.json internal/presentation/clients/*.mjs) \
	$(shell find internal/presentation/clients/scanner internal/presentation/clients/admin internal/presentation/clients/shared -type f)
server := $(shell find cmd internal -type f -name '*.go')

build = @GOOS=$(1) GOARCH=$(3) go build -o dist/$(2)-$(3)/$(4) ./cmd/rdio-scanner
pandoc = @test -d dist/$(1)-$(2) || mkdir -p dist/$(1)-$(2) && pandoc -f markdown -o dist/$(1)-$(2)/$(3) --resource-path docs:docs/platforms $(4) docs/webapp.md docs/faq.md CHANGELOG.md
zip = @cd dist/$(1)-$(2) && zip -q ../$(app)-$(1)-$(2)-v$(ver).zip * && cd ..

.PHONY: all clean client container dist scanner admin sed
.PHONY: freebsd freebsd-amd64
.PHONY: linux linux-386 linux-amd64 linux-arm linux-arm64
.PHONY: macos macos-amd64 macos-arm64
.PHONY: windows windows-amd64

all: clean dist

clean:
	@rm -fr internal/presentation/clients/node_modules dist
	@find internal/presentation/serving/scanner/bundle internal/presentation/serving/admin/bundle -mindepth 1 -not -name .gitkeep -delete

container: client linux-amd64
	@podman build --pull -t $(app):latest .

dist: freebsd linux macos windows

sed:
	@sed -i -re "s|^(\s*\"version\":).*$$|\1 \"$(ver)\"|" internal/presentation/clients/package.json
	@sed -i -re "s|^(const\s+Version\s+=).*$$|\1 \"$(ver)\"|" cmd/rdio-scanner/version.go
	@sed -i -re "s|v[0-9]+\.[0-9]+\.[0-9]+|v$(ver)|" COMPILING.md README.md docs/docker/README.md docs/platforms/*.md
	@sed -i -re "s|[0-9]{4}/[0-9]{2}/[0-9]{2}|$(date)|" docs/docker/README.md docs/platforms/*.md

client: scanner admin

scanner: internal/presentation/serving/scanner/bundle/index.html

admin: internal/presentation/serving/admin/bundle/index.html

# Each bundle is built by the rule for the Go package that embeds it, so `make scanner` cannot leave the
# admin bundle stale and `make admin` cannot leave the scanner bundle stale. Both rules watch the same
# client sources, because the two applications share modules and a stylesheet.
internal/presentation/serving/scanner/bundle/index.html: $(clientSrc)
	@cd internal/presentation/clients && test -d node_modules || npm ci --loglevel=error --no-progress
	@cd internal/presentation/clients && npm run build:scanner

internal/presentation/serving/admin/bundle/index.html: $(clientSrc)
	@cd internal/presentation/clients && test -d node_modules || npm ci --loglevel=error --no-progress
	@cd internal/presentation/clients && npm run build:admin

freebsd: freebsd-amd64
freebsd-amd64: client dist/$(app)-freebsd-amd64-v$(ver).zip

dist/$(app)-freebsd-amd64-v$(ver).zip: $(server)
	$(call pandoc,freebsd,amd64,rdio-scanner.pdf,docs/platforms/freebsd.md)
	$(call build,freebsd,freebsd,amd64,$(app))
	$(call zip,freebsd,amd64,$(app))

linux: linux-386 linux-amd64 linux-arm linux-arm64
linux-386: client dist/$(app)-linux-386-v$(ver).zip
linux-amd64: client dist/$(app)-linux-amd64-v$(ver).zip
linux-arm: client dist/$(app)-linux-arm-v$(ver).zip
linux-arm64: client dist/$(app)-linux-arm64-v$(ver).zip

dist/$(app)-linux-386-v$(ver).zip: $(server)
	$(call pandoc,linux,386,rdio-scanner.pdf,docs/platforms/linux.md)
	$(call build,linux,linux,386,$(app))
	$(call zip,linux,386,$(app))

dist/$(app)-linux-amd64-v$(ver).zip: $(server)
	$(call pandoc,linux,amd64,rdio-scanner.pdf,docs/platforms/linux.md)
	$(call build,linux,linux,amd64,$(app))
	$(call zip,linux,amd64,$(app))

dist/$(app)-linux-arm-v$(ver).zip: $(server)
	$(call pandoc,linux,arm,rdio-scanner.pdf,docs/platforms/linux.md)
	$(call build,linux,linux,arm,$(app))
	$(call zip,linux,arm,$(app))

dist/$(app)-linux-arm64-v$(ver).zip: $(server)
	$(call pandoc,linux,arm64,rdio-scanner.pdf,docs/platforms/linux.md)
	$(call build,linux,linux,arm64,$(app))
	$(call zip,linux,arm64,$(app))

macos: macos-amd64 macos-arm64
macos-amd64: client dist/$(app)-macos-amd64-v$(ver).zip
macos-arm64: client dist/$(app)-macos-arm64-v$(ver).zip

dist/$(app)-macos-amd64-v$(ver).zip: $(server)
	$(call pandoc,macos,amd64,rdio-scanner.pdf,docs/platforms/macos.md)
	$(call build,darwin,macos,amd64,$(app))
	$(call zip,macos,amd64,$(app))

dist/$(app)-macos-arm64-v$(ver).zip: $(server)
	$(call pandoc,macos,arm64,rdio-scanner.pdf,docs/platforms/macos.md)
	$(call build,darwin,macos,arm64,$(app))
	$(call zip,macos,arm64,$(app))

windows: windows-amd64
windows-amd64: client dist/$(app)-windows-amd64-v$(ver).zip

dist/$(app)-windows-amd64-v$(ver).zip: $(server)
	$(call pandoc,windows,amd64,rdio-scanner.pdf,docs/platforms/windows.md)
	$(call build,windows,windows,amd64,$(app).exe)
	$(call zip,windows,amd64,$(app))

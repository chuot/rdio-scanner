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
# The version this repository builds, read from the one place it is declared. GNU Make reads the file
# itself: no shell is started, and the value comes back without the trailing newline.
ver := $(file < VERSION.txt)

# The sources each bundle is built from. The built bundle sits inside the application's own folder,
# so it is excluded: a rule that watched its own output would rebuild on every run.
clientSrc := $(wildcard internal/presentation/*.json internal/presentation/*.mjs) \
	$(shell find internal/presentation/scanner/frontend internal/presentation/admin/frontend internal/presentation/common/frontend -type f -not -path '*/bundle/*')
server := $(shell find cmd internal -type f -name '*.go')

# A server binary is built for one target, and the ffmpeg that target carries is compiled first: the
# server embeds it, so a server built before the ffmpeg it is going to carry exists is a server that
# quietly ships without one. A target this project compiles no ffmpeg for says so and is built the way it
# always was, with the ffmpeg its host installs.
#
# The licence text the build wrote next to the bundle travels into the archive with the binary, because a
# binary that carries an ffmpeg owes whoever receives it a statement of what that ffmpeg is.
define build
	@go run ./cmd/build ffmpeg $(2)-$(3)
	@GOOS=$(1) GOARCH=$(3) go build -ldflags "-X main.Version=$(ver)" -o dist/$(2)-$(3)/$(4) ./cmd/rdio-scanner
	@test ! -e internal/infrastructure/ffmpeg/bundle/$(2)-$(3)/LICENCE.md || cp internal/infrastructure/ffmpeg/bundle/$(2)-$(3)/LICENCE.md dist/$(2)-$(3)/FFMPEG.md
endef
pandoc = @test -d dist/$(1)-$(2) || mkdir -p dist/$(1)-$(2) && pandoc -f markdown -o dist/$(1)-$(2)/$(3) --resource-path docs:docs/platforms $(4) docs/webapp.md docs/faq.md CHANGELOG.md
zip = @cd dist/$(1)-$(2) && zip -q ../$(app)-$(1)-$(2)-v$(ver).zip * && cd ..

.PHONY: all clean client container dist scanner admin sed
.PHONY: freebsd freebsd-amd64
.PHONY: linux linux-386 linux-amd64 linux-arm linux-arm64
.PHONY: macos macos-amd64 macos-arm64
.PHONY: windows windows-amd64

all: clean dist

clean:
	@rm -fr internal/presentation/node_modules dist
	@find internal/presentation/scanner/frontend/bundle internal/presentation/admin/frontend/bundle -mindepth 1 -not -name .gitkeep -delete

container: client linux-amd64
	@podman build --pull --build-arg VERSION=$(ver) -t $(app):latest .

dist: freebsd linux macos windows

sed:
	@sed -i -re "s|v[0-9]+\.[0-9]+\.[0-9]+|v$(ver)|" COMPILING.md README.md docs/docker/README.md docs/platforms/*.md
	@sed -i -re "s|[0-9]{4}/[0-9]{2}/[0-9]{2}|$(date)|" docs/docker/README.md docs/platforms/*.md

client: scanner admin

scanner: internal/presentation/scanner/frontend/bundle/index.html

admin: internal/presentation/admin/frontend/bundle/index.html

# Each bundle is built by the rule for the Go package that embeds it, so `make scanner` cannot leave the
# admin bundle stale and `make admin` cannot leave the scanner bundle stale. Both rules watch the same
# client sources, because the two applications share modules and a stylesheet.
internal/presentation/scanner/frontend/bundle/index.html: $(clientSrc)
	@cd internal/presentation && test -d node_modules || npm ci --loglevel=error --no-progress
	@cd internal/presentation && npm run build:scanner

internal/presentation/admin/frontend/bundle/index.html: $(clientSrc)
	@cd internal/presentation && test -d node_modules || npm ci --loglevel=error --no-progress
	@cd internal/presentation && npm run build:admin

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

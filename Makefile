SHELL := /usr/bin/env bash
.SHELLFLAGS := -euo pipefail -c

VERSION := $(shell cat VERSION)
GO ?= go
PYTHON ?= python3
SOURCE_DATE_EPOCH ?= 0

.PHONY: all build build-hub test-hub test-hub-probe clean fmt check test test-race coverage e2e release install

all: build

build-hub:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -trimpath -o bin/worldbisect-hub ./cmd/worldbisect-hub

test-hub:
	$(GO) test -race ./internal/hub ./cmd/worldbisect-hub ./web/hub -count=1
	$(PYTHON) -m unittest discover -s scripts -p 'test_*hub*.py'
	node --check web/hub/app.js
	node --test web/hub/app.test.js

test-hub-probe: build-hub
	$(PYTHON) -m unittest discover -s scripts -p 'test_hub_probe.py'
	$(PYTHON) scripts/hub-probe-e2e.py --binary bin/worldbisect-hub

build:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w -X github.com/ClusterPilot-System/worldbisect/internal/version.Version=$(VERSION)" -o bin/worldbisect ./cmd/worldbisect
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w -X github.com/ClusterPilot-System/worldbisect/internal/version.Version=$(VERSION)" -o bin/worldbisectd ./cmd/worldbisectd

fmt:
	gofmt -w $$(find . -name '*.go' -type f)

check:
	./scripts/release-check.sh

test:
	$(GO) test ./... -count=1

test-race:
	$(GO) test -race ./... -count=1

coverage:
	$(GO) test ./... -coverprofile=coverage.out -count=1
	$(GO) tool cover -func=coverage.out

e2e:
	./scripts/e2e.sh

release:
	SOURCE_DATE_EPOCH=$(SOURCE_DATE_EPOCH) ./scripts/package.sh

install: build
	install -Dm0755 bin/worldbisect $(DESTDIR)/usr/bin/worldbisect
	install -Dm0755 bin/worldbisectd $(DESTDIR)/usr/bin/worldbisectd
	install -Dm0644 docs/man/worldbisect.1 $(DESTDIR)/usr/share/man/man1/worldbisect.1
	install -Dm0644 docs/man/worldbisectd.8 $(DESTDIR)/usr/share/man/man8/worldbisectd.8
	install -Dm0644 docs/man/worldbisect.conf.5 $(DESTDIR)/usr/share/man/man5/worldbisect.conf.5
	install -Dm0644 packaging/systemd/worldbisectd.service $(DESTDIR)/usr/lib/systemd/system/worldbisectd.service
	install -Dm0644 packaging/sysusers.d/worldbisect.conf $(DESTDIR)/usr/lib/sysusers.d/worldbisect.conf
	install -Dm0644 packaging/tmpfiles.d/worldbisect.conf $(DESTDIR)/usr/lib/tmpfiles.d/worldbisect.conf
	install -Dm0644 LICENSE $(DESTDIR)/usr/share/doc/worldbisect/LICENSE
	install -Dm0644 NOTICE $(DESTDIR)/usr/share/doc/worldbisect/NOTICE

clean:
	rm -rf bin build dist coverage.out

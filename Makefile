PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin
DESTDIR ?=
INSTALL ?= install

PROG := kandor

# VERSION is stamped into the binary by release builds. Leave it empty to use
# the version declared in internal/app/app.go.
VERSION ?=
LDFLAGS ?=
ifneq ($(VERSION),)
LDFLAGS += -X github.com/dlawregiets/kandor/internal/app.version=$(VERSION)
endif

.PHONY: all help build install uninstall test check

all: help

help:
	@printf '%s\n' \
	  'Usage: make [target]' \
	  '' \
	  'Targets:' \
	  '  build      Build bin/$(PROG)' \
	  '  install    Build and install $(PROG) to $(BINDIR)' \
	  '  uninstall  Remove $(PROG) from $(BINDIR)' \
	  '  test       Run go vet and the Go tests' \
	  '  check      Run build, test, the static check and the smoke tests' \
	  '  help       Show this message' \
	  '' \
	  'Variables:' \
	  '  PREFIX     Installation prefix [default: ~/.local]' \
	  '  BINDIR     Binary directory [default: $$(PREFIX)/bin]' \
	  '  DESTDIR    Staging root for package builds [default: empty]' \
	  '  VERSION    Version to stamp into the binary [default: internal/app/app.go]'

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(PROG) ./cmd/kandor

install: build
	$(INSTALL) -d "$(DESTDIR)$(BINDIR)"
	$(INSTALL) -m 0755 "bin/$(PROG)" "$(DESTDIR)$(BINDIR)/$(PROG)"

uninstall:
	rm -f "$(DESTDIR)$(BINDIR)/$(PROG)"

test:
	go vet ./...
	go test -count=1 ./...

check: build test
	@file bin/$(PROG) | grep -q 'statically linked' || \
	  { echo 'bin/$(PROG) is not statically linked' >&2; exit 1; }
	./tests/smoke.sh

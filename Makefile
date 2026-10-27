PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin
DESTDIR ?=
INSTALL ?= install

PROG := kandor

.PHONY: all help build install uninstall check

all: help

help:
	@printf '%s\n' \
	  'Usage: make [target]' \
	  '' \
	  'Targets:' \
	  '  build      Build bin/$(PROG)' \
	  '  install    Build and install $(PROG) to $(BINDIR)' \
	  '  uninstall  Remove $(PROG) from $(BINDIR)' \
	  '  check      Run the unit and smoke tests' \
	  '  help       Show this message' \
	  '' \
	  'Variables:' \
	  '  PREFIX     Installation prefix [default: ~/.local]' \
	  '  BINDIR     Binary directory [default: $$(PREFIX)/bin]' \
	  '  DESTDIR    Staging root for package builds [default: empty]'

build:
	go build -trimpath -o bin/$(PROG) ./cmd/kandor

install: build
	$(INSTALL) -d "$(DESTDIR)$(BINDIR)"
	$(INSTALL) -m 0755 "bin/$(PROG)" "$(DESTDIR)$(BINDIR)/$(PROG)"

uninstall:
	rm -f "$(DESTDIR)$(BINDIR)/$(PROG)"

check:
	go vet ./...
	go test ./...
	./tests/unit.sh
	./tests/smoke.sh

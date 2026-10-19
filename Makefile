PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin
DESTDIR ?=
INSTALL ?= install

PROG := kandor

.PHONY: all help install uninstall check

all: help

help:
	@printf '%s\n' \
	  'Usage: make [target]' \
	  '' \
	  'Targets:' \
	  '  install    Install $(PROG) to $(BINDIR)' \
	  '  uninstall  Remove $(PROG) from $(BINDIR)' \
	  '  check      Run the smoke tests' \
	  '  help       Show this message' \
	  '' \
	  'Variables:' \
	  '  PREFIX     Installation prefix [default: ~/.local]' \
	  '  BINDIR     Binary directory [default: $$(PREFIX)/bin]' \
	  '  DESTDIR    Staging root for package builds [default: empty]'

install: $(PROG)
	$(INSTALL) -d "$(DESTDIR)$(BINDIR)"
	$(INSTALL) -m 0755 "$(PROG)" "$(DESTDIR)$(BINDIR)/$(PROG)"

uninstall:
	rm -f "$(DESTDIR)$(BINDIR)/$(PROG)"

check:
	./tests/smoke.sh

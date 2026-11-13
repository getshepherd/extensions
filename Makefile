# paddock is a library; it has nothing to run on its own. The targets below build
# and run an example extension from examples/, which shows how a page plugs into
# Shepherd's Extensions. Pick one with EXAMPLE=<dir>. go.work builds it against this checkout.
EXAMPLE ?= demo
BIN := bin/$(EXAMPLE)

.PHONY: test example run install url status logs uninstall clean

test:
	go test ./... ./examples/$(EXAMPLE)/...

example:
	go build -o $(BIN) ./examples/$(EXAMPLE)

# в foreground, без сервиса; Ctrl-C чтобы остановить. url создаёт порт и токен, serve берёт те же.
# ponytail: если пример уже установлен и запущен, порт занят: make uninstall или `$(BIN) stop`.
run: example
	@echo "open: $$($(BIN) url)"
	$(BIN) serve

# копирует бинарник в extensions Shepherd и запускает; печатает URL.
# START=shepherd — только пока открыт Shepherd, без launchd/systemd.
install: example
	$(BIN) install $(if $(START),--start $(START))

url status logs uninstall: example
	$(BIN) $@

clean:
	rm -rf bin

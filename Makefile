.PHONY: build test serve

PORT ?= 8080

build:
	mkdir -p bin
	go build -o bin/trackerator .

test:
	go test ./...

serve: build
	@set -eu; \
	deck_bin="$(AGENT_DECK_BIN)"; \
	if [ -z "$$deck_bin" ]; then deck_bin=$$(command -v agent-deck 2>/dev/null || true); fi; \
	if [ -z "$$deck_bin" ]; then deck_bin="$(HOME)/.local/bin/agent-deck"; fi; \
	if [ ! -x "$$deck_bin" ]; then echo "Agent Deck not found; set AGENT_DECK_BIN=/path/to/agent-deck" >&2; exit 1; fi; \
	deck_pid=; \
	listener_pid=$$(lsof -nP -tiTCP:8420 -sTCP:LISTEN 2>/dev/null | head -n 1); \
	if [ -n "$$listener_pid" ]; then \
		listener_command=$$(ps -p "$$listener_pid" -o comm=); \
		case "$$listener_command" in *agent-deck*) echo "Agent Deck already listening on 127.0.0.1:8420";; \
			*) echo "Port 8420 is in use by $$listener_command, not Agent Deck" >&2; exit 1;; esac; \
	else \
		echo "Starting Agent Deck on 127.0.0.1:8420"; \
		"$$deck_bin" web --no-tui --listen 127.0.0.1:8420 & deck_pid=$$!; \
		trap 'kill "$$deck_pid" 2>/dev/null || true' EXIT; \
		ready=; \
		for attempt in 1 2 3 4 5 6 7 8 9 10; do \
			if curl --silent --max-time 1 --output /dev/null http://127.0.0.1:8420/; then ready=1; break; fi; \
			if ! kill -0 "$$deck_pid" 2>/dev/null; then break; fi; \
			sleep 0.5; \
		done; \
		if [ -z "$$ready" ]; then echo "Agent Deck did not start on port 8420" >&2; exit 1; fi; \
	fi; \
	echo "Starting Trackerator on 127.0.0.1:$(PORT)"; \
	TRACKERATOR_AGENT_DECK_BIN="$$deck_bin" ./bin/trackerator serve -port "$(PORT)"

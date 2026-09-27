APP := local-llm
BIN := bin/$(APP)
CONFIG := config/models.yaml
ENV_FILE := .env

.PHONY: build run status test tidy setup allow-firewall install-launchd uninstall-launchd clean

build:
	mkdir -p bin
	go build -o $(BIN) ./cmd/local-llm
	codesign -s - --force $(BIN) >/dev/null 2>&1 || true

run: build
	./$(BIN) serve --config $(CONFIG) --env-file $(ENV_FILE)

status: build
	./$(BIN) status --config $(CONFIG) --env-file $(ENV_FILE)

test:
	go test ./...

tidy:
	go mod tidy

setup:
	./scripts/setup.sh

allow-firewall:
	./scripts/allow-firewall.sh

install-launchd: build
	@test -f $(ENV_FILE) || (echo "missing $(ENV_FILE); run make setup first" && exit 1)
	sed -e "s|__REPO_ROOT__|$(CURDIR)|g" \
		-e "s|__USER__|$(USER)|g" \
		launchd/com.local-llm.serve.plist \
		> "$(HOME)/Library/LaunchAgents/com.local-llm.serve.plist"
	launchctl unload "$(HOME)/Library/LaunchAgents/com.local-llm.serve.plist" 2>/dev/null || true
	launchctl load "$(HOME)/Library/LaunchAgents/com.local-llm.serve.plist"
	@echo "Loaded com.local-llm.serve — check: launchctl list | grep local-llm"

uninstall-launchd:
	launchctl unload "$(HOME)/Library/LaunchAgents/com.local-llm.serve.plist" 2>/dev/null || true
	rm -f "$(HOME)/Library/LaunchAgents/com.local-llm.serve.plist"
	@echo "Removed com.local-llm.serve"

clean:
	rm -rf bin

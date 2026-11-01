# Soliton Rudder: common tasks. See docs/DEVELOPMENT.md.
SHELL := /bin/bash
# explicit package list: `./...` would also pick up Go files inside frontend/node_modules
GOPKGS := . ./cmd/... ./internal/...
export PATH := $(PATH):$(shell go env GOPATH)/bin

.PHONY: help dev build dmg test lint vuln fmt docs notices clean set-repo check-placeholders branding

help: ## list targets
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | sed 's/:.*##/ -/' | sort

dev: ## run the app with hot reload
	wails dev

build: ## build the .app into build/bin
	wails build

dmg: ## build a signed (ad-hoc by default) universal DMG into dist/  (see docs/RELEASING.md)
	./scripts/build-dmg.sh

test: ## Go (race) + frontend tests
	go test -race $(GOPKGS)
	cd frontend && npm test

lint: ## go vet, staticcheck, gofmt, eslint, tsc
	go vet $(GOPKGS)
	go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 $(GOPKGS)
	@test -z "$$(gofmt -l . | grep -v node_modules)" || { echo "gofmt needed:"; gofmt -l . | grep -v node_modules; exit 1; }
	cd frontend && npm run lint && npx tsc --noEmit

vuln: ## known vulnerabilities in Go and npm dependencies (needs network)
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 $(GOPKGS)
	cd frontend && npm audit --audit-level=high

fmt: ## format Go and frontend code
	gofmt -w $$(git ls-files '*.go' 2>/dev/null || find . -name '*.go' -not -path './frontend/*')
	cd frontend && npm run format

docs: ## regenerate generated documentation
	go run ./cmd/gendocs

notices: ## regenerate THIRD_PARTY_NOTICES.txt (needs go-licenses; see script header)
	python3 scripts/gen_notices.py

branding: ## regenerate every logo asset from assets/branding/logo-source.png (needs pillow and numpy)
	python3 scripts/gen_branding.py

check-placeholders: ## fail while OWNER / TODO(maintainer) placeholders remain (run before the first release)
	./scripts/check-placeholders.sh

set-repo: ## point the project at its GitHub repo: make set-repo REPO=owner/name
	./scripts/set-repo.sh $(REPO)

clean: ## remove build output
	rm -rf build/bin dist frontend/dist/assets frontend/dist/index.html

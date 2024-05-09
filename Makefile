# Get the latest commit branch, hash, and date
TAG=$(shell git describe --tags --abbrev=0 --exact-match 2>/dev/null)
BRANCH=$(if $(TAG),$(TAG),$(shell git rev-parse --abbrev-ref HEAD 2>/dev/null))
HASH=$(shell git rev-parse --short=7 HEAD 2>/dev/null)
TIMESTAMP=$(shell git log -1 --format=%ct HEAD 2>/dev/null | xargs -I{} date -u -r {} +%Y%m%dT%H%M%S)
GIT_REV=$(shell printf "%s-%s-%s" "$(BRANCH)" "$(HASH)" "$(TIMESTAMP)")
REV=$(if $(filter --,$(GIT_REV)),latest,$(GIT_REV)) # fallback to latest if not in git repo


docker:
	docker build -t umputun/tg-spam .

race_test:
	go test -race -timeout=60s -count 1 ./...

prep_site:
	cp -fv README.md site/docs/index.md
	sed -i '' 's|https:\/\/github.com\/umputun\/tg-spam\/raw\/master\/site\/tg-spam-bg.png|logo.png|' site/docs/index.md
	sed -i '' 's|^.*/workflows/ci.yml.*$$||' site/docs/index.md

release:
	@echo release to .bin
	goreleaser --snapshot --skip-publish --clean
	ls -l .bin

build:
	mkdir -p .bin
	cd app && go build -ldflags "-X main.revision=$(REV) -s -w" -o ../.bin/tg-spam.$(BRANCH)
	cp .bin/tg-spam.$(BRANCH) .bin/tg-spam

test:
	go clean -testcache
	go test -race -coverprofile=coverage.out ./...
	grep -v "_mock.go" coverage.out | grep -v mocks > coverage_no_mocks.out
	go tool cover -func=coverage_no_mocks.out
	rm coverage.out coverage_no_mocks.out

build_site: prep_site
	cd site &&\
	pip3 install -r requirements.txt &&\
	mkdocs build -d public


.PHONY: docker race_test prep_site release build test
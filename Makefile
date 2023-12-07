docker:
	docker build -t umputun/tg-spam .

race_test:
	cd app && go test -race -mod=vendor -timeout=60s -count 1 ./...

prep_site:
	cp -fv README.md site/docs/index.md
	sed -i '' 's|https:\/\/github.com\/umputun\/tg-spam\/raw\/master\/site\/tg-spam-bg.png|logo.png|' site/docs/index.md
	sed -i '' 's|^.*/workflows/ci.yml.*$$||' site/docs/index.md

.PHONY: docker race_test prep_site
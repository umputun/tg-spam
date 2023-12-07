docker:
	docker build -t umputun/tg-spam .

race_test:
	cd app && go test -race -mod=vendor -timeout=60s -count 1 ./...

prep_site:
	cp -fv README.md site/docs/index.md
	sed -i 's|https://raw.githubusercontent.com/umputun/tg-spam/master/site/docs/logo.png|logo.png|' site/docs/index.md
	sed -i 's|^.*https://github.com/umputun/tg-spam/workflows/build/badge.svg.*$$||' site/docs/index.md
	cd site && mkdocs build


.PHONY: docker race_test prep_site
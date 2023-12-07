FROM umputun/baseimage:buildgo-latest as build

ARG GIT_BRANCH
ARG GITHUB_SHA
ARG CI

ENV GOFLAGS="-mod=vendor"

ADD . /build
WORKDIR /build

RUN \
    if [ -z "$CI" ] ; then \
    echo "runs outside of CI" && version=$(git rev-parse --abbrev-ref HEAD)-$(git log -1 --format=%h)-$(date +%Y%m%dT%H:%M:%S); \
    else version=${GIT_BRANCH}-${GITHUB_SHA:0:7}-$(date +%Y%m%dT%H:%M:%S); fi && \
    echo "version=$version" && \
    cd app && go build -o /build/tg-spam -ldflags "-X main.revision=${version} -s -w"


FROM ghcr.io/umputun/baseimage/app:v1.11.0 as base

FROM scratch

COPY --from=build /build/tg-spam /srv/tg-spam
COPY --from=base /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=base /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=base /etc/passwd /etc/passwd
COPY --from=base /etc/group /etc/group

USER app
COPY data/* /data/
VOLUME /data
WORKDIR /srv
EXPOSE 8080
ENTRYPOINT ["/srv/tg-spam"]

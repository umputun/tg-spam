FROM ghcr.io/umputun/baseimage/buildgo:latest as build

ARG GIT_BRANCH
ARG GITHUB_SHA
ARG CI

ADD . /build
WORKDIR /build

RUN go version

RUN \
    if [ -z "$CI" ] ; then \
    echo "runs outside of CI" && version=$(git rev-parse --abbrev-ref HEAD)-$(git log -1 --format=%h)-$(date +%Y%m%dT%H:%M:%S); \
    else version=${GIT_BRANCH}-${GITHUB_SHA:0:7}-$(date +%Y%m%dT%H:%M:%S); fi && \
    echo "version=$version" && \
    cd app && go build -o /build/tg-spam -ldflags "-X main.revision=${version} -s -w"


FROM alpine:3.19
# enables automatic changelog generation by tools like Dependabot
LABEL org.opencontainers.image.source="https://github.com/umputun/tg-spam"
ENV TGSPAM_IN_DOCKER=1
RUN apk add --no-cache tzdata
COPY --from=build /build/tg-spam /srv/tg-spam
COPY data /srv/data
RUN \
    adduser -s /bin/sh -D -u 1000 app && chown -R app:app /home/app && \
    chown -R app:app /srv/data && \
    chmod -R 775 /srv/data && \
    ls -la /srv/data

USER app
WORKDIR /srv
EXPOSE 8080
ENTRYPOINT ["/srv/tg-spam"]

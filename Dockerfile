FROM ghcr.io/umputun/baseimage/buildgo:v1.15.0 AS build

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


FROM alpine:3.21
# enables automatic changelog generation by tools like Dependabot
LABEL org.opencontainers.image.source="https://github.com/umputun/tg-spam"
ENV TGSPAM_IN_DOCKER=1
RUN apk add --no-cache tzdata
COPY --from=build /build/tg-spam /srv/tg-spam

COPY data /srv/preset
COPY data/.not_mounted /srv/data/.not_mounted
COPY entrypoint.sh /srv/entrypoint.sh

RUN \
 adduser -s /bin/sh -D -u 1000 app && chown -R app:app /home/app && \
 chown -R app:app /srv/preset /srv/data && \
 chmod -R 777 /srv/preset && \
 chmod -R 775 /srv/data && \
 chmod +x /srv/entrypoint.sh && \
 ls -la /srv/preset

USER app
WORKDIR /srv

RUN \
 /srv/tg-spam --convert=only --files.dynamic=/srv/preset --files.samples=/srv/preset && \
 sh -c 'for f in /srv/preset/*.txt.loaded; do mv -vf "$f" "${f%.loaded}"; done' && \
 echo "preset files converted" && \
 ls -la /srv/preset && \
 ls -la /srv/data

EXPOSE 8080
ENTRYPOINT ["/srv/entrypoint.sh"]

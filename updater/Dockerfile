FROM ghcr.io/umputun/baseimage/app:latest as base

ENV UPDATER_IN_DOCKER=1
COPY update-git.sh /srv/update-git.sh
RUN chmod +x /srv/update-git.sh
RUN apk add --no-cache --update git tzdata ca-certificates rsync
COPY update-git.sh /srv/update-git.sh

USER app

VOLUME /data
WORKDIR /srv
ENTRYPOINT ["/init.sh", "/srv/update-git.sh"]
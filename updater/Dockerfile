FROM ghcr.io/umputun/baseimage/app:latest as base

ENV UPDATER_IN_DOCKER=1
COPY update-git.sh /srv/update-git.sh
RUN chmod +x /srv/update-git.sh
RUN apk add --no-cache --update git tzdata ca-certificates rsync

WORKDIR /srv
ENTRYPOINT ["/init.sh", "/srv/update-git.sh"]
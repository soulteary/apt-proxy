ARG APK_CLIENT_IMAGE=alpine:3.22
FROM ${APK_CLIENT_IMAGE}
RUN apk add --no-cache alpine-sdk bash ca-certificates curl openssl python3 strace
COPY apt-proxy /usr/local/bin/apt-proxy
COPY fixture.py run.sh /tests/
ENTRYPOINT ["bash", "/tests/run.sh"]

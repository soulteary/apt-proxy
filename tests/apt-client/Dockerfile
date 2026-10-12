ARG APT_CLIENT_IMAGE=debian:bookworm-slim
FROM ${APT_CLIENT_IMAGE}
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl dpkg-dev gnupg openssl python3 xz-utils \
    && rm -rf /var/lib/apt/lists/*
COPY apt-proxy /usr/local/bin/apt-proxy
COPY fixture.py run.sh /tests/
ENTRYPOINT ["bash", "/tests/run.sh"]

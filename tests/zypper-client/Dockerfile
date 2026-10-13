ARG ZYPPER_CLIENT_IMAGE=opensuse/leap:16.0
FROM ${ZYPPER_CLIENT_IMAGE}
RUN zypper --non-interactive install --no-recommends ca-certificates curl \
    createrepo_c gpg2 openssl python3 rpm-build strace \
    && zypper clean --all
COPY apt-proxy /usr/local/bin/apt-proxy
COPY fixture.py run.sh /tests/
ENTRYPOINT ["bash", "/tests/run.sh"]

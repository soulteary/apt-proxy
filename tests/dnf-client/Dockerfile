ARG DNF_CLIENT_IMAGE=quay.io/centos/centos:stream9
FROM ${DNF_CLIENT_IMAGE}
RUN dnf install -y --allowerasing ca-certificates curl createrepo_c gnupg2 \
    openssl python3 rpm-build rpm-sign strace \
    && dnf clean all
COPY apt-proxy /usr/local/bin/apt-proxy
COPY fixture.py run.sh /tests/
ENTRYPOINT ["bash", "/tests/run.sh"]

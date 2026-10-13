ARG PACMAN_CLIENT_IMAGE=archlinux:base
FROM ${PACMAN_CLIENT_IMAGE}
RUN pacman -Syu --noconfirm \
    && pacman -S --noconfirm --needed diffutils fakeroot python openssl sudo \
    && pacman -Scc --noconfirm
COPY apt-proxy /usr/local/bin/apt-proxy
COPY fixture.py run.sh /tests/
ENTRYPOINT ["bash", "/tests/run.sh"]

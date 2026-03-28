FROM golang:1-trixie

ENV DEBIAN_FRONTEND=noninteractive
ENV LANG=en_US.utf8

# System packages (requires root).
RUN apt-get update && apt-get install -y \
    bats \
    build-essential ca-certificates curl gdb git gpg \
    htop jq sudo tmux vim

# Create non-root user (golang image only ships root).
RUN groupadd --gid 1000 dev \
    && useradd --uid 1000 --gid 1000 -m dev \
    && echo "dev ALL=(root) NOPASSWD:ALL" > /etc/sudoers.d/dev \
    && chmod 0440 /etc/sudoers.d/dev

# act (run GitHub Actions locally).
RUN curl --proto '=https' --tlsv1.2 -sSf https://raw.githubusercontent.com/nektos/act/master/install.sh | bash

# Pre-create directories that the non-root user needs to own.
# /commandhistory is a volume mount point for persistent zsh history (see devcontainer.json).
RUN mkdir -p /commandhistory \
    && chown dev:dev /commandhistory

# ── Everything below runs as dev ──────────────────────────────
USER dev

# act default image config (avoid interactive prompt).
RUN mkdir -p /home/dev/.config/act \
    && echo '-P ubuntu-latest=catthehacker/ubuntu:act-latest' > /home/dev/.config/act/actrc

# Go module cache in a predictable location.
ENV GOPATH="/home/dev/go"
ENV PATH="${GOPATH}/bin:${PATH}"

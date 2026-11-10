# Image for the end-to-end tests: a bare distro plus Claude Code, running as
# an unprivileged user with an empty home. quotamaxxer itself is mounted in
# by run.sh, so the image only needs rebuilding to pick up a new Claude Code.
ARG BASE=alpine:latest
FROM ${BASE}

# libgcc, libstdc++ and ripgrep are what Claude Code needs on musl.
# util-linux provides script(1), used to give interactive sessions a pty.
RUN if command -v apk >/dev/null; then \
      apk add --no-cache bash ca-certificates curl jq util-linux libgcc libstdc++ ripgrep; \
    else \
      apt-get update \
      && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
           bash ca-certificates curl jq util-linux procps \
      && rm -rf /var/lib/apt/lists/*; \
    fi

RUN if command -v useradd >/dev/null; then useradd -m -s /bin/bash tester; \
    else adduser -D -s /bin/bash tester; fi
USER tester
ENV HOME=/home/tester
ENV PATH=/home/tester/.claude/ralph-quotamaxxer/bin:/home/tester/.local/bin:$PATH
# Alpine: use the system ripgrep (the bundled one is built for glibc).
ENV USE_BUILTIN_RIPGREP=0

ARG CLAUDE_VERSION=latest
RUN curl -fsSL https://claude.ai/install.sh | bash -s -- "${CLAUDE_VERSION}"
WORKDIR /home/tester

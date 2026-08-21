###############################################################################
# Stage 1 - Build Go development tools
###############################################################################

FROM golang:1.26.0-bookworm AS tools

ENV GOBIN=/opt/go-tools/bin

RUN mkdir -p ${GOBIN}

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail

TOOLS=(
    "golang.org/x/tools/gopls@latest"
    "golang.org/x/tools/cmd/goimports@latest"
    "mvdan.cc/gofumpt@latest"
    "github.com/golangci/golangci-lint/cmd/golangci-lint@latest"
    "honnef.co/go/tools/cmd/staticcheck@latest"
    "golang.org/x/vuln/cmd/govulncheck@latest"
    "github.com/go-delve/delve/cmd/dlv@latest"
    "github.com/air-verse/air@latest"
    "gotest.tools/gotestsum@latest"
    "github.com/kyoh86/richgo@latest"
)

for tool in "${TOOLS[@]}"; do
    echo
    echo "============================================================"
    echo "Installing ${tool}"
    echo "============================================================"

    go install "${tool}"
done

echo
echo "Go tools installation completed."
EOF


###############################################################################
# Stage 2 - Development container
###############################################################################

FROM mcr.microsoft.com/devcontainers/base:ubuntu-24.04


ENV DEBIAN_FRONTEND=noninteractive


###############################################################################
# System packages
###############################################################################

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    gcc \
    g++ \
    make \
    curl \
    wget \
    unzip \
    ca-certificates \
    gnupg \
    sqlite3 \
    libsqlite3-dev \
    ripgrep \
    fd-find \
    bat \
    git \
    git-delta \
    hyperfine \
    jq \
    tree \
    htop \
    lsof \
    && rm -rf /var/lib/apt/lists/*


###############################################################################
# GitHub CLI
###############################################################################

RUN mkdir -p /etc/apt/keyrings && \
    curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
        -o /etc/apt/keyrings/githubcli-archive-keyring.gpg && \
    chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg && \
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
        > /etc/apt/sources.list.d/github-cli.list && \
    apt-get update && \
    apt-get install -y gh && \
    rm -rf /var/lib/apt/lists/*


###############################################################################
# just command runner
###############################################################################

RUN curl --proto '=https' --tlsv1.2 -sSf \
    https://just.systems/install.sh \
    | bash -s -- --to /usr/local/bin


###############################################################################
# Go SDK
###############################################################################

ARG GO_VERSION=1.26.0

RUN wget -q https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz && \
    rm -rf /usr/local/go && \
    tar -C /usr/local -xzf go${GO_VERSION}.linux-amd64.tar.gz && \
    rm go${GO_VERSION}.linux-amd64.tar.gz


###############################################################################
# Copy Go tools
###############################################################################

COPY --from=tools /opt/go-tools/bin/ /usr/local/bin/


###############################################################################
# Environment
###############################################################################

ENV PATH="/usr/local/go/bin:/usr/local/bin:${PATH}"

ENV GO111MODULE=on \
    CGO_ENABLED=1 \
    GOPROXY=https://proxy.golang.org,direct \
    GOSUMDB=sum.golang.org


###############################################################################
# Shell helpers
###############################################################################

RUN cat >> /etc/bash.bashrc <<'EOF'

alias ll='ls -lah'
alias fmt='gofumpt -w .'
alias lint='golangci-lint run'
alias test='gotestsum'
alias air='air'

EOF


###############################################################################
# Workspace
###############################################################################

USER vscode

WORKDIR /workspaces


###############################################################################
# Validation
###############################################################################

RUN go version && \
    git --version && \
    gh --version && \
    just --version && \
    gopls version && \
    gofumpt -version && \
    golangci-lint version && \
    staticcheck -debug.version && \
    govulncheck -version && \
    dlv version


###############################################################################
# Container health
###############################################################################

HEALTHCHECK \
    --interval=5m \
    --timeout=10s \
    --start-period=30s \
    --retries=3 \
    CMD go version >/dev/null 2>&1 && \
        gopls version >/dev/null 2>&1 && \
        golangci-lint version >/dev/null 2>&1 || exit 1
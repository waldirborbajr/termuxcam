###############################################################################
# Super GOLANG Dev Container
#
# Versão do Go é lida automaticamente do go.mod do projeto (linha `go` ou
# `toolchain`, com resolução de patch caso só major.minor seja informado).
###############################################################################

###############################################################################
# Stage 1 - Bootstrap Go e build das dev tools
###############################################################################

FROM debian:bookworm AS tools

ENV DEBIAN_FRONTEND=noninteractive

ARG TARGETARCH=amd64

WORKDIR /tmp

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        wget \
        git \
        bash \
        jq \
        build-essential \
    && rm -rf /var/lib/apt/lists/*

COPY go.mod /tmp/go.mod

RUN set -eux; \
    TOOLCHAIN_LINE="$(grep -E '^toolchain[[:space:]]+go[0-9]' /tmp/go.mod || true)"; \
    if [ -n "${TOOLCHAIN_LINE}" ]; then \
        GO_VERSION="$(echo "${TOOLCHAIN_LINE}" | sed -E 's/^toolchain[[:space:]]+go//')"; \
    else \
        GO_VERSION="$(sed -nE 's/^go[[:space:]]+([0-9]+\.[0-9]+(\.[0-9]+)?).*$/\1/p' /tmp/go.mod | head -n1)"; \
    fi; \
    if [ -z "${GO_VERSION}" ]; then \
        echo "ERRO: não foi possível determinar a versão do Go a partir do go.mod"; \
        exit 1; \
    fi; \
    case "${GO_VERSION}" in \
        *.*.*) : ;; \
        *) \
            echo "go.mod só especifica major.minor (${GO_VERSION}) — resolvendo último patch estável..."; \
            GO_VERSION="$(curl -sSf 'https://go.dev/dl/?mode=json&include=all' \
                | jq -r --arg v "go${GO_VERSION}." \
                  '[.[] | select(.stable==true) | .version | select(startswith($v))] | sort | last' \
                | sed 's/^go//')"; \
            if [ -z "${GO_VERSION}" ]; then \
                echo "ERRO: não achei um patch estável correspondente"; exit 1; \
            fi ;; \
    esac; \
    echo "Versão Go resolvida: ${GO_VERSION}"; \
    wget -q "https://go.dev/dl/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz" -O /tmp/go.tar.gz; \
    rm -rf /usr/local/go; \
    tar -C /usr/local -xzf /tmp/go.tar.gz; \
    rm /tmp/go.tar.gz; \
    /usr/local/go/bin/go version

ENV PATH="/usr/local/go/bin:/usr/local/bin:${PATH}"

ENV GO111MODULE=on \
    CGO_ENABLED=1 \
    GOPROXY=https://proxy.golang.org,direct \
    GOSUMDB=sum.golang.org \
    GOBIN=/opt/go-tools/bin

RUN mkdir -p "${GOBIN}"

###############################################################################
# golangci-lint: versão pinada (não "latest") para evitar pegar uma release
# recém-publicada com checksum/CDN ainda dessincronizado. Se o script de
# binário mesmo assim falhar (problema já visto: raw.githubusercontent.com
# e GitHub Releases CDN saem de sincronia por alguns minutos após o
# publish), cai para `go install` como fallback — mais lento, mas robusto.
###############################################################################

ARG GOLANGCI_LINT_VERSION=v2.13.1

RUN set -eux; \
    if curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh \
        | sh -s -- -b "${GOBIN}" "${GOLANGCI_LINT_VERSION}"; then \
        echo "golangci-lint instalado via binário (${GOLANGCI_LINT_VERSION})"; \
    else \
        echo "Script de binário falhou (provável mismatch de checksum/CDN) — usando go install como fallback"; \
        GOFLAGS=-mod=mod go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION}"; \
    fi

RUN curl -sSfL https://raw.githubusercontent.com/air-verse/air/master/install.sh \
        | sh -s -- -b "${GOBIN}"

###############################################################################
# Ferramentas sem binário oficial publicado — compiladas via `go install`.
###############################################################################

RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    set -eux; \
    TOOLS="\
        golang.org/x/tools/gopls@latest \
        golang.org/x/tools/cmd/goimports@latest \
        mvdan.cc/gofumpt@latest \
        honnef.co/go/tools/cmd/staticcheck@latest \
        golang.org/x/vuln/cmd/govulncheck@latest \
        github.com/go-delve/delve/cmd/dlv@latest \
        gotest.tools/gotestsum@latest \
        github.com/kyoh86/richgo@latest \
    "; \
    for tool in ${TOOLS}; do \
        echo "Instalando ${tool}"; \
        go install "${tool}"; \
    done; \
    echo "Ferramentas instaladas:"; \
    ls -lah "${GOBIN}"

###############################################################################
# Stage 2 - Container de desenvolvimento
###############################################################################

FROM mcr.microsoft.com/devcontainers/base:ubuntu-24.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
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
    && rm -rf /var/lib/apt/lists/* \
    && ln -s /usr/bin/fdfind /usr/local/bin/fd \
    && ln -s /usr/bin/batcat /usr/local/bin/bat

RUN mkdir -p /etc/apt/keyrings && \
    curl -fsSL \
        https://cli.github.com/packages/githubcli-archive-keyring.gpg \
        -o /etc/apt/keyrings/githubcli-archive-keyring.gpg && \
    chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg && \
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
        > /etc/apt/sources.list.d/github-cli.list && \
    apt-get update && \
    apt-get install -y --no-install-recommends gh && \
    rm -rf /var/lib/apt/lists/*

RUN curl --proto '=https' --tlsv1.2 -sSf \
    https://just.systems/install.sh \
    | bash -s -- --to /usr/local/bin

COPY --from=tools /usr/local/go /usr/local/go
COPY --from=tools /opt/go-tools/bin/ /usr/local/bin/

ENV PATH="/usr/local/go/bin:/usr/local/bin:${PATH}"

ENV GO111MODULE=on \
    CGO_ENABLED=1 \
    GOPROXY=https://proxy.golang.org,direct \
    GOSUMDB=sum.golang.org

RUN mkdir -p /home/vscode/go/pkg/mod /home/vscode/.cache/go-build \
    && chown -R vscode:vscode /home/vscode/go /home/vscode/.cache

RUN cat >> /etc/bash.bashrc <<'EOF'

alias ll='ls -lah'
alias fmt='gofumpt -w .'
alias lint='golangci-lint run'
alias gotest='gotestsum'

EOF

USER vscode

WORKDIR /workspaces

RUN set -eux; \
    go version; \
    git --version; \
    gh --version; \
    just --version; \
    gopls version; \
    gofumpt -version; \
    golangci-lint version; \
    staticcheck -debug.version; \
    govulncheck -version; \
    dlv version; \
    air -v

HEALTHCHECK \
    --interval=5m \
    --timeout=10s \
    --start-period=30s \
    --retries=3 \
    CMD \
        go version >/dev/null 2>&1 && \
        gopls version >/dev/null 2>&1 && \
        golangci-lint version >/dev/null 2>&1 \
        || exit 1
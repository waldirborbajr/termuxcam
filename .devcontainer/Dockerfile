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

# Populado automaticamente pelo BuildKit (amd64/arm64) — sem hardcode.
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

###############################################################################
# Resolve versão do Go: prioriza `toolchain goX.Y.Z` (pin exato); se só
# houver `go X.Y` (sem patch) no go.mod, resolve o último patch estável
# via API oficial do go.dev.
###############################################################################

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
# Ferramentas com BINÁRIO pré-compilado oficial (evita compilar árvores
# de dependência grandes — golangci-lint em especial é conhecido por ser
# lento via `go install` por causa dos dezenas de linters embutidos).
###############################################################################

RUN curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh \
        | sh -s -- -b "${GOBIN}" \
    && curl -sSfL https://raw.githubusercontent.com/air-verse/air/master/install.sh \
        | sh -s -- -b "${GOBIN}"

###############################################################################
# Ferramentas sem binário oficial publicado — compiladas via `go install`.
# Isso é aceitável no Go (diferente do Rust): o compilador é rápido e essas
# ferramentas são pequenas; com o cache de módulos/build abaixo, leva
# segundos, não minutos.
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
    # Debian nomeia diferente — cria os aliases de comando esperados.
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

# Pastas de cache do usuário vscode, já com dono certo — é nelas que os
# volumes do devcontainer.json vão montar (senão o cache do `go mod
# download` evapora a cada rebuild do container).
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

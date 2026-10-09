# Fork PoC Dockerfile:
#  - signal-cli is built FROM SOURCE of the gthieleb/signal-cli fork
#    (pinned commit 218d625302b9e15e8e4213de4db371100e64bc57), which adds the
#    message-history transfer feature (AsamK PR #2134: importHistory on
#    startLink/finishLink). The upstream AsamK release tarball is NOT used.
#  - amd64-only on purpose (target deployment is qnap-k3s x86_64); the
#    upstream arm64/armv7 branches are deliberately dropped for simplicity.
ARG SIGNAL_CLI_VERSION=0.14.9
ARG SIGNAL_CLI_FORK_COMMIT=218d625302b9e15e8e4213de4db371100e64bc57

ARG SWAG_VERSION=1.16.4

ARG BUILD_VERSION_ARG=unset

# Stage 1: build the forked signal-cli (JVM distribution) from source
FROM eclipse-temurin:25-jdk AS signalcli

ARG SIGNAL_CLI_VERSION
ARG SIGNAL_CLI_FORK_COMMIT

RUN apt-get update \
	&& apt-get -y install --no-install-recommends git wget \
	&& rm -rf /var/lib/apt/lists/*

RUN git clone https://github.com/gthieleb/signal-cli.git /tmp/src \
	&& git -C /tmp/src checkout ${SIGNAL_CLI_FORK_COMMIT}

# installDist produces build/install/signal-cli (JVM dist incl. all jars)
RUN cd /tmp/src && ./gradlew --no-daemon installDist

RUN mv /tmp/src/build/install/signal-cli /opt/signal-cli-${SIGNAL_CLI_VERSION}

# Stage 2: build the signal-cli-rest-api Go wrapper (unchanged upstream flow)
FROM golang:1.26-trixie AS buildcontainer

ARG SIGNAL_CLI_VERSION
ARG SWAG_VERSION
ARG BUILD_VERSION_ARG

RUN dpkg-reconfigure debconf --frontend=noninteractive \
	&& apt-get update \
	&& apt-get -y install --no-install-recommends \
		wget git locales zip unzip \
		file build-essential libz-dev zlib1g-dev binutils \
	&& rm -rf /var/lib/apt/lists/*

RUN sed -i -e 's/# en_US.UTF-8 UTF-8/en_US.UTF-8 UTF-8/' /etc/locale.gen && \
    dpkg-reconfigure --frontend=noninteractive locales && \
    update-locale LANG=en_US.UTF-8

ENV LANG=en_US.UTF-8

RUN go install github.com/swaggo/swag/cmd/swag@v${SWAG_VERSION}

COPY src/api /tmp/signal-cli-rest-api-src/api
COPY src/client /tmp/signal-cli-rest-api-src/client
COPY src/datastructs /tmp/signal-cli-rest-api-src/datastructs
COPY src/utils /tmp/signal-cli-rest-api-src/utils
COPY src/scripts /tmp/signal-cli-rest-api-src/scripts
COPY src/storage /tmp/signal-cli-rest-api-src/storage
COPY src/main.go /tmp/signal-cli-rest-api-src/
COPY src/go.mod /tmp/signal-cli-rest-api-src/
COPY src/go.sum /tmp/signal-cli-rest-api-src/
COPY src/plugin_loader.go /tmp/signal-cli-rest-api-src/
COPY src/docs/add_v1_receive_schemas.go /tmp/signal-cli-rest-api-src/docs/add_v1_receive_schemas.go

RUN ls -la /tmp/signal-cli-rest-api-src

# build the docs
RUN cd /tmp/signal-cli-rest-api-src && ${GOPATH}/bin/swag init --requiredByDefault --outputTypes "go,json"

# manually add the json schemas for the receive V1 endpoint to the docs
RUN cd /tmp/signal-cli-rest-api-src/docs \
	&& wget https://github.com/AsamK/signal-cli/releases/download/v${SIGNAL_CLI_VERSION}/signal-cli-${SIGNAL_CLI_VERSION}-json-schemas.tar.gz \
	&& mkdir signal-cli-schemas \
	&& tar xf signal-cli-${SIGNAL_CLI_VERSION}-json-schemas.tar.gz -C signal-cli-schemas \
	&& go run add_v1_receive_schemas.go signal-cli-schemas

# build signal-cli-rest-api
RUN cd /tmp/signal-cli-rest-api-src && go build -o signal-cli-rest-api main.go
RUN cd /tmp/signal-cli-rest-api-src && go test ./client -v && go test ./utils -v && go test ./storage -v

# build supervisorctl_config_creator
RUN cd /tmp/signal-cli-rest-api-src/scripts && go build -o jsonrpc2-helper

# build plugin_loader
RUN cd /tmp/signal-cli-rest-api-src && go build -buildmode=plugin -o signal-cli-rest-api_plugin_loader.so plugin_loader.go

# Start a fresh container for release

FROM ubuntu:noble

ENV GIN_MODE=release

ENV PORT=8080

ARG SIGNAL_CLI_VERSION
ARG BUILD_VERSION_ARG

ENV BUILD_VERSION=$BUILD_VERSION_ARG
ENV SIGNAL_CLI_REST_API_PLUGIN_SHARED_OBJ_DIR=/usr/bin/

RUN dpkg-reconfigure debconf --frontend=noninteractive \
	&& apt-get update \
	&& apt-get install -y --no-install-recommends util-linux supervisor openjdk-25-jre curl locales \
	&& rm -rf /var/lib/apt/lists/*

COPY --from=signalcli /opt/signal-cli-${SIGNAL_CLI_VERSION} /opt/signal-cli-${SIGNAL_CLI_VERSION}
COPY --from=buildcontainer /tmp/signal-cli-rest-api-src/signal-cli-rest-api /usr/bin/signal-cli-rest-api
COPY --from=buildcontainer /tmp/signal-cli-rest-api-src/scripts/jsonrpc2-helper /usr/bin/jsonrpc2-helper
COPY --from=buildcontainer /tmp/signal-cli-rest-api-src/signal-cli-rest-api_plugin_loader.so /usr/bin/signal-cli-rest-api_plugin_loader.so
COPY entrypoint.sh /entrypoint.sh

RUN userdel ubuntu -r \
	&& groupadd -g 1000 signal-api \
	&& useradd --no-log-init -M -d /home -s /bin/bash -u 1000 -g 1000 signal-api \
	&& ln -s /opt/signal-cli-${SIGNAL_CLI_VERSION}/bin/signal-cli /usr/bin/signal-cli \
	&& mkdir -p /signal-cli-config/ \
	&& mkdir -p /home/.local/share/signal-cli

RUN sed -i -e 's/# en_US.UTF-8 UTF-8/en_US.UTF-8 UTF-8/' /etc/locale.gen && \
    dpkg-reconfigure --frontend=noninteractive locales && \
    update-locale LANG=en_US.UTF-8

ENV LANG=en_US.UTF-8

EXPOSE ${PORT}

ENV SIGNAL_CLI_CONFIG_DIR=/home/.local/share/signal-cli
ENV SIGNAL_CLI_UID=1000
ENV SIGNAL_CLI_GID=1000
ENV SIGNAL_CLI_CHOWN_ON_STARTUP=true

ENTRYPOINT ["/entrypoint.sh"]

HEALTHCHECK --interval=20s --timeout=10s --retries=3 \
    CMD curl -f http://localhost:${PORT}/v1/health || exit 1
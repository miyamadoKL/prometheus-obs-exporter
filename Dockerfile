# syntax=docker/dockerfile:1

# --- builder ---------------------------------------------------------------
FROM golang:1.26 AS builder

WORKDIR /src

# ARGs mirror the ldflags injected by Makefile / .goreleaser.yml into
# github.com/prometheus/common/version, so `obs-exporter --version` reports
# meaningful values instead of "unknown" when built via Docker.
ARG VERSION=unknown
ARG REVISION=unknown
ARG BRANCH=unknown
ARG BUILD_USER=docker
ARG BUILD_DATE=unknown

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build \
    -o /out/obs-exporter \
    -ldflags "-s -w \
      -X github.com/prometheus/common/version.Version=${VERSION} \
      -X github.com/prometheus/common/version.Revision=${REVISION} \
      -X github.com/prometheus/common/version.Branch=${BRANCH} \
      -X github.com/prometheus/common/version.BuildUser=${BUILD_USER} \
      -X github.com/prometheus/common/version.BuildDate=${BUILD_DATE}" \
    ./cmd/obs-exporter

# --- runtime -----------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/obs-exporter /obs-exporter

USER nonroot:nonroot

EXPOSE 9438

ENTRYPOINT ["/obs-exporter"]

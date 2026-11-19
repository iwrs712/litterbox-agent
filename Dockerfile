# syntax=docker/dockerfile:1.7

FROM golang:1.25-bookworm AS build

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS="${TARGETOS:-linux}" GOARCH="${TARGETARCH:-amd64}" \
    go build -trimpath -ldflags='-s -w' -o /out/envd ./cmd/envd

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl git ripgrep \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/envd /usr/local/bin/envd

WORKDIR /workspace
EXPOSE 49983
HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=3 \
  CMD ["curl", "-fsS", "http://127.0.0.1:49983/health"]

ENTRYPOINT ["/usr/local/bin/envd"]
CMD ["server", "--host", "0.0.0.0", "--port", "49983"]

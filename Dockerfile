# syntax=docker/dockerfile:1.7

FROM golang:1.23.4 AS builder

WORKDIR /app

ARG TARGETOS
ARG TARGETARCH

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY cmd ./cmd

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /server ./cmd/server

FROM scratch

ENV PORT=8080

COPY --from=builder /server /server

EXPOSE 8080

USER 65532:65532
ENTRYPOINT ["/server"]

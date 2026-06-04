FROM golang:1.26 AS builder

WORKDIR /app

ARG TARGETOS
ARG TARGETARCH

COPY go.mod ./
COPY cmd ./cmd

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /server ./cmd/server

FROM scratch

ENV PORT=8080

COPY --from=builder /server /server

EXPOSE 8080

USER 65532:65532
ENTRYPOINT ["/server"]

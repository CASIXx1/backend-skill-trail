FROM golang:1.18 AS builder

WORKDIR /app

COPY go.mod ./
COPY cmd ./cmd

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /server ./cmd/server

FROM scratch

ENV PORT=8080

COPY --from=builder /server /server

EXPOSE 8080

USER 65532:65532
ENTRYPOINT ["/server"]

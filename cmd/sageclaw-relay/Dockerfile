FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /sageclaw-relay ./cmd/sageclaw-relay/

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /sageclaw-relay /usr/local/bin/sageclaw-relay
EXPOSE 8080
ENTRYPOINT ["sageclaw-relay"]
CMD ["/etc/sageclaw-relay/relay.yaml"]

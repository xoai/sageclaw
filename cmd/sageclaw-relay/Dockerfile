FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY sageclaw-relay /usr/local/bin/sageclaw-relay
EXPOSE 8080
ENTRYPOINT ["sageclaw-relay"]
CMD ["/etc/sageclaw-relay/relay.yaml"]

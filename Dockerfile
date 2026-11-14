FROM golang:1.26-alpine AS build

RUN apk add --no-cache ca-certificates
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY config /config
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mince ./cmd/mince
RUN mkdir -p /out/cache/mince/ccr && chown -R 65532:65532 /out/cache

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/mince /mince
COPY --from=build --chown=65532:65532 /out/cache /var/cache
USER 65532:65532
ENV XDG_CACHE_HOME=/var/cache
EXPOSE 8787
ENTRYPOINT ["/mince"]

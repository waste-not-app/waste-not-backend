FROM golang:1.27.4-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download && go mod verify
COPY . .
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/api ./cmd/api && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/migrate ./cmd/migrate && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/guides ./cmd/guides && \
    mkdir -p /out/var/lib/wastenot-photos

FROM gcr.io/distroless/static-debian12:nonroot
ARG VERSION=dev
LABEL org.opencontainers.image.title="wastenot-api" \
      org.opencontainers.image.description="WasteNot REST API" \
      org.opencontainers.image.source="https://github.com/waste-not-app/waste-not-backend" \
      org.opencontainers.image.version="${VERSION}"
COPY --from=build /out/api /out/migrate /out/guides /
COPY --from=build --chown=65532:65532 /out/var/lib/wastenot-photos /var/lib/wastenot-photos
ENV HTTP_ADDR=0.0.0.0:8080 \
    WASTENOT_RELEASE=${VERSION}
EXPOSE 8080
USER nonroot:nonroot
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 CMD ["/api", "healthcheck"]
ENTRYPOINT ["/api"]

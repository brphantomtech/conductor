# Conductor — Profile C (cloud) image (SPEC §3.2).
#
# Multi-stage build that produces a minimal single-binary image. Build flags
# mirror the Makefile `build` target (-trimpath, -ldflags '-s -w').
#
#   docker build -t conductor:latest .
#
# The dashboard embed (web/build via go:embed) is wired in a later phase; once
# present, add a `bun run build` stage and copy web/build/ into the builder
# before `go build`. Today the binary builds standalone.

# ---- build stage ----------------------------------------------------------
FROM golang:1.25-alpine AS build

# git is needed for module fetches that resolve via VCS; ca-certificates for
# HTTPS module proxies.
RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Cache module downloads independently of source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO disabled for a fully static binary that runs in the scratch image.
ARG VERSION=dev
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags '-s -w' -o /out/conductor ./cmd/conductor

# ---- runtime stage --------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot AS runtime

# Copy CA bundle so outbound HTTPS (tracker/provider APIs) works.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/conductor /usr/local/bin/conductor

# Default config and workspace roots; mount real config/state at runtime.
WORKDIR /var/lib/conductor
USER nonroot:nonroot

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/conductor"]
CMD ["start"]

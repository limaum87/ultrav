# Build context is the REPOSITORY ROOT:
#   docker build -f backend/Dockerfile .
#
# The libvirt client is loaded at RUNTIME via dlopen (build tag libvirt_dlopen),
# so the build has no libvirt-dev dependency and the same binary serves both
# the mock provider and HYPERVISOR_PROVIDER=libvirt (when libvirt.so exists).
FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
COPY backend/internal ./internal
COPY backend/cmd ./cmd
RUN CGO_ENABLED=1 go build -tags libvirt_dlopen -trimpath -o /out/ultrav ./cmd/ultrav

FROM golang:1.23-bookworm AS test
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
COPY backend/internal ./internal
COPY backend/cmd ./cmd
RUN go test -tags libvirt_dlopen ./...

# Runtime: debian + libvirt client library (used only when connecting to a
# real daemon; the mock provider runs without it).
FROM debian:12-slim
RUN apt-get update -qq && apt-get install -y -qq --no-install-recommends \
      libvirt0 ca-certificates wget \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/ultrav /usr/local/bin/ultrav
ENV ULTRAV_PORT=8080
EXPOSE 8080
USER nobody
ENTRYPOINT ["ultrav"]

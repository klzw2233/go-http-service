# syntax=docker/dockerfile:1

# ---- build stage ----
# Pin the major Go version rather than a moving tag like :latest. The patch
# level is governed by go.mod (go 1.26.6); the image tracks the same line.
FROM golang:1.26 AS build
WORKDIR /src

# Copy the module files first so `go mod download` forms its own layer.
# Source changes then invalidate only the build step, not the dependency
# download, which is the slow part of rebuilding after a one-line edit.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    go mod download

# Now the rest of the source.
COPY . .

# VERSION is injected at build time into model.Version (the /api/info field).
# The build stage has git available to compute it; the distroless runtime does
# not, so it must be resolved here. Defaults to "dev" for local builds.
ARG VERSION=dev

# CGO disabled -> static binary, the only kind that runs on distroless/static.
# -trimpath strips the build machine's path prefixes from the binary.
# -s -w strips the symbol table and DWARF debug info; a server never needs them
#   and they roughly halve the image size.
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags "-s -w -X go-http-service/internal/model.Version=${VERSION}" \
      -o /out/server ./cmd/server

# ---- runtime stage ----
# distroless/static: ~2MB base, no package manager, no shell. The :nonroot
# variant ships a nonroot user (uid 65532); the binary runs as it without
# needing useradd, which the base cannot do anyway (no shell).
FROM gcr.io/distroless/static-debian12:nonroot

# Copy only the compiled binary. Everything else (Go toolchain, source, build
# caches) stays in the build stage and never reaches the runtime image.
COPY --from=build /out/server /server

# Run as the non-root user the base image provides. Stating it explicitly means
# the guarantee survives even if someone swaps the base to one that defaults
# to root.
USER nonroot

# Documentation only: a container does not listen on this port unless a host
# port is mapped (docker -p, or compose ports). Stating it lets tooling know
# which port to forward.
EXPOSE 8080

# exec (not shell) form is mandatory here: distroless has no shell to interpret
# the shell form, and exec form avoids a shell entirely.
ENTRYPOINT ["/server"]

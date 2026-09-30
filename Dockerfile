# ---- build ------------------------------------------------------------------
FROM golang:1.26-alpine AS build

WORKDIR /src

# Copy the module files alone first. Docker caches each layer, so dependencies
# are only re-downloaded when go.mod/go.sum actually change — not on every
# source edit.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 produces a statically linked binary with no libc dependency,
# which is what lets the final image below contain nothing but the binary.
#
# -ldflags "-s -w" strips the symbol table and DWARF debug info (~25% smaller).
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

# ---- run --------------------------------------------------------------------
# "static" holds a CA bundle, /etc/passwd and timezone data, and nothing else:
# no shell, no package manager, no busybox. If someone finds a way to execute
# commands in this container, there is nothing here to execute.
FROM gcr.io/distroless/static-debian12:nonroot

# The UI and the .sql migrations are embedded in the binary, so this really is
# the whole application.
COPY --from=build /out/api /api

# nonroot (uid 65532) is baked into the image tag above. A process that cannot
# write to its own filesystem cannot be persisted into.
USER nonroot:nonroot

EXPOSE 8080

ENTRYPOINT ["/api"]

# One image for all four backend binaries; compose picks which one runs
# via `command`. Lua scripts and migrations are go:embed-ed, so the final
# stage needs nothing but the static binaries.

FROM golang:1.26.7-alpine AS build
WORKDIR /src

# Module download is its own layer so a source-only change reuses it.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/ \
      ./cmd/api ./cmd/relay ./cmd/ledger-worker ./cmd/migrate

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ /app/
USER nonroot

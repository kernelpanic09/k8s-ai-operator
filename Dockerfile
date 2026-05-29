# Stage 1: build
FROM golang:1.22-alpine AS builder

WORKDIR /workspace

# Fetch dependencies before copying source so this layer caches across
# changes that don't touch go.mod/go.sum.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-w -s" -o manager ./cmd/manager

# Stage 2: runtime
# distroless/static:nonroot gives us a minimal, non-root image with no shell.
FROM gcr.io/distroless/static:nonroot

WORKDIR /

COPY --from=builder /workspace/manager .

# UID 65532 is the nonroot user baked into distroless/static:nonroot.
USER 65532:65532

ENTRYPOINT ["/manager"]

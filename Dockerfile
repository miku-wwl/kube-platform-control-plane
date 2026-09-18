FROM golang:1.24-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/platform-control-plane ./cmd/controller

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/platform-control-plane /platform-control-plane
USER 65532:65532
ENTRYPOINT ["/platform-control-plane"]

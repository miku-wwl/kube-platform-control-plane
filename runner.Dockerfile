FROM golang:1.24-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY api ./api
COPY internal ./internal
COPY cmd/terraform-runner ./cmd/terraform-runner
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/terraform-runner ./cmd/terraform-runner

FROM hashicorp/terraform:1.14.0@sha256:3abcdb56739bf9c61a0290cfd1a2e41ef9c3799c0e6fa7f3c467f883367d3ecb
COPY --from=build /out/terraform-runner /usr/local/bin/terraform-runner
ENTRYPOINT ["/usr/local/bin/terraform-runner"]

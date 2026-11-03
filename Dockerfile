FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS build
ARG TARGETOS=linux
ARG TARGETARCH=amd64
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
ARG COMPONENT=core
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/homelab-monitor ./cmd/${COMPONENT}
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/homelab-monitor /homelab-monitor
USER nonroot:nonroot
ENTRYPOINT ["/homelab-monitor"]

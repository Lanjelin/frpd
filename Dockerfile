FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X main.version=$VERSION" -o /out/frpd ./cmd/frpd

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/frpd /frpd
EXPOSE 8080
ENTRYPOINT ["/frpd"]
CMD ["-c", "/config/frpd.toml"]

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src
COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /node ./cmd/node


FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /node /ChordDHT-Node

ENTRYPOINT ["/ChordDHT-Node"]

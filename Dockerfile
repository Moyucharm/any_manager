FROM golang:1.23 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN /usr/local/go/bin/go mod download

COPY . .
RUN CGO_ENABLED=0 /usr/local/go/bin/go test ./... && \
    CGO_ENABLED=0 /usr/local/go/bin/go build -buildvcs=false -o /out/anymanager ./cmd/server

FROM alpine:3.20

RUN apk add --no-cache ca-certificates && mkdir -p /app/data

WORKDIR /app

COPY --from=build /out/anymanager /app/anymanager
COPY SCHEMA.sql /app/SCHEMA.sql

EXPOSE 8080 8081

CMD ["/app/anymanager"]

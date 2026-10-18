FROM golang:1.27-alpine AS build

ARG COMPONENT=metanode

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -o /out/dfsha ./cmd/${COMPONENT}

FROM alpine:3.20

RUN apk add --no-cache ca-certificates # certificados para TLS a futuro

WORKDIR /app

COPY --from=build /out/dfsha /app/dfsha

ENTRYPOINT ["/app/dfsha"]
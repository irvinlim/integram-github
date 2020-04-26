FROM golang:1.14 AS builder

# Install go dep
RUN curl -fsSL https://raw.githubusercontent.com/golang/dep/master/install.sh | sh

ENV PKG github.com/irvinlim/integram-github
WORKDIR /go/src/${PKG}

# Install dependencies
COPY Gopkg.toml Gopkg.lock ./
RUN dep ensure -vendor-only

# Add application code and install binary
COPY . ./
RUN CGO_ENABLED=0 GOOS=linux go build -installsuffix cgo -o /go/app ${PKG}/cmd

# Move the built binary into the tiny alpine linux image
FROM alpine:latest

RUN apk --no-cache add ca-certificates && rm -rf /var/cache/apk/*
WORKDIR /app

COPY --from=builder /go/app .
CMD ["./app"]

# From https://github.com/jahkeup/updater53/blob/master/Dockerfile
###############################################################################

FROM golang:1.17-alpine as builder

WORKDIR /go/src/github.com/SumoLogic/docker-logging-driver
COPY . .

ARG GOOS=linux
ARG GOARM=

RUN apk add --no-cache git mercurial \
    && go get -d -v ./... \
    && apk del git mercurial
RUN CGO_ENABLED=0 go build -v -a -installsuffix cgo -o docker-logging-driver

###############################################################################

FROM debian:latest as certs

RUN apt-get update &&  \
    apt-get install -y ca-certificates && \
    rm -rf /var/lib/apt/lists/*

RUN cp /etc/ca-certificates.conf /tmp/caconf && cat /tmp/caconf | \
  grep -v "mozilla/CNNIC_ROOT\.crt" > /etc/ca-certificates.conf && \
update-ca-certificates --fresh

###############################################################################

FROM scratch

COPY --from=builder /go/src/github.com/SumoLogic/docker-logging-driver/docker-logging-driver /usr/bin/docker-logging-driver
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

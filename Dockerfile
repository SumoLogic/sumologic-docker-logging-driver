FROM  golang:1.7

COPY . /go/src/github.com/SumoLogic/docker-logging-driver
RUN cd /go/src/github.com/SumoLogic/docker-logging-driver && go get && go build --ldflags '-extldflags "-static"' -o /usr/bin/docker-logging-driver

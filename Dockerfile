FROM golang:alpine AS builder

RUN apk update && apk add --no-cache git
COPY . /go/src/github.com/wish/kubetel
WORKDIR /go/src/github.com/wish/kubetel
RUN export GO111MODULE=on && GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o /go/bin/kubetel

FROM alpine

VOLUME /go/src

RUN mkdir -p /kubetel
COPY --from=builder /go/src/github.com/wish/kubetel/deploy/ /kubetel/deploy/
COPY --from=builder /go/bin/kubetel /kubetel/

WORKDIR /kubetel

ENTRYPOINT ["/kubetel/kubetel"]
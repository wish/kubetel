FROM golang:alpine

ADD . /go/src/github.com/Wish/kubetel
RUN go install github.com/Wish/kubetel

RUN rm -r /go/src/github.com/Wish/kubetel

VOLUME /go/src


WORKDIR /kubetel

USER 1001

ENTRYPOINT ["kubetel"]

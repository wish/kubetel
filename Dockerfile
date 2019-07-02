FROM golang:alpine

ADD . /go/src/github.com/Wish/kubetel
RUN go install github.com/Wish/kubetel

RUN rm -r /go/src/github.com/Wish/kubetel

VOLUME /go/src

# TODO: this is dodgy it expects k8s files to always be available from runtime directory

# need to packae the yaml n version file using tool chains properly
RUN mkdir -p /kubetel
ADD ./deploy /kubetel/deploy/
ADD kubetel /kubetel/

WORKDIR /kubetel

ENTRYPOINT ["kubetel"]
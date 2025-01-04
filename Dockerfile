FROM golang:1.23.2-alpine AS builder
ADD . /go/src/github.com/guru-docker/docker-volume-sshfs
WORKDIR /go/src/github.com/guru-docker/docker-volume-sshfs

RUN apk add --no-cache --virtual .build-deps gcc libc-dev
RUN go install --ldflags '-extldflags "-static"'
RUN apk del .build-deps

CMD ["/go/bin/docker-volume-sshfs"]


FROM alpine

RUN apk update && apk add sshfs
RUN mkdir -p /run/docker/plugins /mnt/state /mnt/volumes

COPY --from=builder /go/bin/docker-volume-sshfs .
CMD ["docker-volume-sshfs"]

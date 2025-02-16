FROM golang:alpine AS builder
WORKDIR /go/src/app

ARG BUILD_DATE
ARG BUILD_USER
ARG GIT_BRANCH
ARG GIT_REVISION
ARG GO111MODULE
ARG VERSION

COPY . .
RUN apk --update --no-cache add git && \
        go mod tidy && \
        go install \
            -ldflags "-X github.com/prometheus/common/version.BuildDate=${BUILD_DATE} \
                        -X github.com/prometheus/common/version.BuildUser=${BUILD_USER} \
                        -X github.com/prometheus/common/version.Branch=${GIT_BRANCH} \
                        -X github.com/prometheus/common/version.Revision=${GIT_REVISION} \
                        -X github.com/prometheus/common/version.Version=${VERSION}" && \
						touch config.yml

FROM alpine:latest
RUN apk --update --no-cache add ca-certificates
ENTRYPOINT ["/firefly-iii-simplefin-importer"]
EXPOSE 9717/tcp
USER nobody
COPY --from=builder /go/bin/firefly-iii-simplefin-importer .

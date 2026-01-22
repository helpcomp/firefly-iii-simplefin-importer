FROM golang:alpine
WORKDIR /go/src/app
COPY go.mod .
RUN go mod download
COPY . .
ENV GOCACHE=/root/.cache/go-build

ARG REVISION
ARG BRANCH
ARG BUILD_USER
ARG BUILD_DATE


RUN apk --no-cache add git bash
RUN --mount=type=cache,target=/root/.cache/go-build \
    VERSION=$(cat VERSION) && \
    REVISION=$(git rev-parse HEAD 2>/dev/null || echo "none") && \
    BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "none") && \
    BUILD_USER="$(whoami)@$(hostname)" && \
    BUILD_DATE=$(date -u +'%Y-%m-%dT%H:%M:%SZ') && \
    go build -ldflags "\
      -X github.com/prometheus/common/version.Version=$VERSION \
      -X github.com/prometheus/common/version.Revision=$REVISION \
      -X github.com/prometheus/common/version.Branch=$BRANCH \
      -X github.com/prometheus/common/version.BuildUser=$BUILD_USER \
      -X github.com/prometheus/common/version.BuildDate=$BUILD_DATE" \
      -o /go/bin/firefly-iii-simplefin-importer .

FROM alpine:latest
RUN apk --update --no-cache add ca-certificates tzdata
ENV TZ=America/New_York
USER nobody
COPY --from=0 /go/bin/firefly-iii-simplefin-importer .
EXPOSE 9717/tcp
ENTRYPOINT ["/firefly-iii-simplefin-importer"]

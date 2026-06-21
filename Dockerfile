FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/sinq /usr/local/bin/sinq

ENTRYPOINT ["sinq"]

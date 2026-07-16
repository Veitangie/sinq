FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata && adduser -D sinq
USER sinq

WORKDIR /home/sinq
LABEL org.opencontainers.image.source="https://github.com/Veitangie/sinq"

ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/sinq /usr/local/bin/sinq

ENTRYPOINT ["sinq"]

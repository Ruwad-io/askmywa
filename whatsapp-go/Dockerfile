# askmywa as a send-only server (for example Mīzān's WhatsApp sends on Railway).
# It keeps no message: only the WhatsApp session and the contact names, on the
# volume mounted at /data. The HTTP door listens on the private network only;
# give the service no public domain. Required at run time: ASKMYWA_HTTP_TOKEN
# (32+ characters). Pair once with: askmywa link --phone <your number>
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=server
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/askmywa .

FROM alpine:3.22
RUN apk add --no-cache su-exec && adduser -D -u 10001 askmywa && mkdir -p /data && chown askmywa /data
COPY --from=build /out/askmywa /usr/local/bin/askmywa
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
ENV ASKMYWA_DATA_DIR=/data \
    ASKMYWA_SEND_ONLY=1 \
    ASKMYWA_HTTP_ADDR=[::]:8080
# No VOLUME instruction: hosts such as Railway refuse it and mount their own
# volume at /data (docker run: -v askmywa-data:/data).
EXPOSE 8080
# The entrypoint gives the volume to the askmywa user, then drops root.
ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["askmywa", "daemon", "--data-dir", "/data", "--pair", "wait"]

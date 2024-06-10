FROM golang:1.23-alpine as builder

WORKDIR /app

COPY . .

RUN apk add --no-cache upx chromium libstdc++ libx11 libxcomposite libxrandr libxi libxdamage mesa-gl glib ca-certificates && \
    go build -o netflix-household-autovalidator && \
    upx --best --lzma netflix-household-autovalidator


FROM debian:bookworm-slim

WORKDIR /

RUN apt-get update && apt-get install -y --no-install-recommends \
     chromium wget ca-certificates && \
     apt-get clean && rm -rf /var/lib/apt/lists/*

COPY --from=builder /app/netflix-household-autovalidator /netflix-household-autovalidator

ENTRYPOINT ["/netflix-household-autovalidator"]
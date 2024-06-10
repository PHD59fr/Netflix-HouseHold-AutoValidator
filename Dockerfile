FROM golang:1.20-alpine as builder

WORKDIR /app

COPY . .

RUN go build -o netflix-household-autovalidator

RUN apk add --no-cache upx && upx --best --lzma netflix-household-autovalidator

FROM scratch

COPY --from=builder /app/netflix-household-autovalidator /netflix-household-autovalidator

ENTRYPOINT ["/netflix-household-autovalidator"]


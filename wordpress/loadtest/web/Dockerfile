FROM golang:1.17-alpine

WORKDIR /web
COPY . .

RUN CGO_ENABLED=0 go build ./src/main.go

CMD ["./main"]



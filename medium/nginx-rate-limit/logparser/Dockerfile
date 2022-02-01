FROM golang:1.17-alpine

WORKDIR /logparser
COPY . .

RUN go get gopkg.in/fsnotify.v1
RUN go install github.com/hpcloud/tail/...
RUN CGO_ENABLED=0 go build ./src/main.go

CMD ["./main"]



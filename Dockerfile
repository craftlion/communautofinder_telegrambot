# syntax=docker/dockerfile:1

FROM golang:latest

WORKDIR /app

COPY go.mod ./
COPY go.sum ./
RUN go mod download

COPY *.go ./

RUN go build -o ./communautoFinderBot

EXPOSE 8443

CMD [ "./communautoFinderBot" ]
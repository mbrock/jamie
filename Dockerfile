FROM golang:1.22

WORKDIR /src
RUN apt-get update && apt-get install -y libopusenc-dev
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN make


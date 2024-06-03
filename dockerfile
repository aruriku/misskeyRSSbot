FROM golang:alpine3.20
WORKDIR /docker
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
RUN go build
CMD ["./misskeyBOT"]

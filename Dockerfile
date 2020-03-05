FROM golang:1.14

WORKDIR /app

COPY go.mod ./
RUN go mod download

# Install source
COPY . .

# Build application
RUN go build -o main .

EXPOSE 80

CMD ["./main"]
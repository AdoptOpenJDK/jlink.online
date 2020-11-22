FROM golang:1.14

WORKDIR /app

COPY go.mod ./
RUN go mod download

# Install source
COPY . .

# Build application
RUN go build -o main .

ENV GIN_MODE=release
ENV MAVEN_CENTRAL=false

EXPOSE 8080

CMD ["./main"]

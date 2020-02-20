FROM golang:1.13

WORKDIR /app

# Install dependencies
RUN apt-get update && apt-get install -y bsdtar
RUN apt-get clean && rm -rf /var/lib/apt/lists/*

COPY go.mod ./
RUN go mod download

# Install source
COPY . .

# Build application
RUN go build -o main .

EXPOSE 80

CMD ["./main"]
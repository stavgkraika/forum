FROM golang:1.22-alpine
WORKDIR /app
RUN apk add --no-cache gcc musl-dev sqlite-dev
COPY . .
RUN go build -o forum ./cmd/server
EXPOSE 8080
CMD ["./forum"]

FROM golang:1.24.2 AS builder

WORKDIR /app

COPY . .

RUN go mod tidy

RUN go build -o student-management-api main.go

FROM golang:1.24.2

WORKDIR /app

COPY --from=builder /app/student-management-api .

COPY .env .env

EXPOSE 8080

CMD ["./student-management-api"]
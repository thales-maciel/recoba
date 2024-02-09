FROM golang:1.22.0-alpine3.19 as build

RUN mkdir /opt/build
WORKDIR /opt/build

COPY go.mod go.sum ./

RUN go mod download

COPY main.go ./

RUN CGO_ENABLED=0 GOOS=linux go build -o /opt/build/recoba


FROM debian:buster-slim

RUN mkdir -p /opt/app/recoba
WORKDIR /opt/app/recoba

RUN apt-get update -y && apt-get install -y libpq5

COPY --from=build /opt/build/recoba ./

EXPOSE 8080

CMD [ "./recoba" ]


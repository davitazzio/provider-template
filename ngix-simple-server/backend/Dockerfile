FROM golang:1.13 AS build

WORKDIR /hello-ngix
COPY hello-ngix.go /hello-ngix/
RUN CGO_ENABLED=0 go build -o backend hello-ngix.go

EXPOSE 80



FROM scratch
COPY --from=build /hello-ngix/backend /usr/local/bin/backend
CMD ["/usr/local/bin/backend"]




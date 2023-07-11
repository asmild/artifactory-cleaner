FROM golang:1.19 AS build
ENV OS="linux darwin windows"
WORKDIR /go/src/app
COPY . .

RUN go get -d -v . && \
    go install -v . && \
    ls -la

CMD ["/go/bin/artifactory-go-cleaner"]

#FROM scratch AS save
FROM debian AS save
COPY --from=build /go/bin/artifactory-go-cleaner .
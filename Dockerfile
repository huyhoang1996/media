FROM golang:1.12

RUN mkdir /go/src/github.com/
RUN cd /go/src/github.com/
RUN git clone https://github.com/huyhoang1996/media.git
#Set working directory
RUN cd media/
WORKDIR /go/src/github.com/media/

RUN go get -u github.com/golang/dep/cmd/dep
COPY . .
RUN dep ensure -v
RUN go build

CMD ["./media"]
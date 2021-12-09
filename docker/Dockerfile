FROM golang:1.15 as builder

run export GO111MODULE=on &&  go get github.com/gobuffalo/packr/packr@v1.30.1 && cp -f /go/bin/packr* /bin/packr  
run apt update && apt install vim -y
run export PATH=$PATH:/go/bin &&  cd / && git clone https://github.com/sjqzhang/chatbot.git chatbot && cd chatbot/cli/server && cp -rf /chatbot/static /chatbot/cli/server/
run cd /chatbot/cli/server && packr && sed -i 's#//router.StaticFS#router.StaticFS#' server.go && sed -i  '/http.Dir/d' server.go  
run cd /chatbot/cli/server && rm -rf chatserver  &&  go build -o chatserver *.go 



from sjqzhang/alpine-glibc:latest

run mkdir -p /opt/chatbot

copy --from=builder /chatbot/cli/server/chatserver /opt/chatbot/chatserver

workdir /opt/chatbot/

entrypoint ["/opt/chatbot/chatserver"]

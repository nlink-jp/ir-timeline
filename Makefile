APP_NAME := ir-timeline
VERSION  := 0.1.0
DIST     := dist
LDFLAGS  := -s -w -X main.version=$(VERSION)

.PHONY: build test clean build-all

build:
	@mkdir -p $(DIST)
	go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP_NAME) .

test:
	go test ./... -v

clean:
	rm -rf $(DIST)

build-all:
	@mkdir -p $(DIST)
	GOOS=linux   GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP_NAME)-linux-amd64 .
	GOOS=linux   GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP_NAME)-linux-arm64 .
	GOOS=darwin  GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP_NAME)-darwin-amd64 .
	GOOS=darwin  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP_NAME)-darwin-arm64 .
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(APP_NAME)-windows-amd64.exe .

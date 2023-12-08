all: build

ROOT_DIR:=$(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))

build:
	cd $(ROOT_DIR)/src; go build -o $(ROOT_DIR)/bin/main main.go

run: build
	$(ROOT_DIR)/bin/main
	
clean:
	rm -r bin

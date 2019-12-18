.PHONY: all
all: clean build


.PHONY: clean
clean:
	if [ -d '/tmp/go' ]; then	\
		rm -rf '/tmp/go';	\
	fi

	mkdir '/tmp/go'

	if [ -f 'staticserv' ]; then	\
		rm 'staticserv';	\
	fi


.PHONY: build
build:
	GOPATH='/tmp/go' GOBIN='/tmp/go/bin' go get
	GOPATH='/tmp/go' GOBIN='/tmp/go/bin' go build


.PHONY: run
run:
	./staticserv


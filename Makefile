.PHONY: all
all: clean install-dependencies build


.PHONY: install-dependencies
install-dependencies:
	go mod download


.PHONY: clean
clean:
	if [ -f 'go.sum' ]; then	\
		rm 'go.sum';		\
	fi

	if [ -f 'staticserv' ]; then	\
		rm 'staticserv';	\
	fi


.PHONY: build
build:
	go build -o 'staticserv'


.PHONY: run
run:
	./staticserv


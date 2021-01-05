.PHONY: all
all: clean install-dependencies build-native build-cross


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

	if [ -f 'staticserv (linux, amd64)' ]; then		\
		rm 'staticserv (linux, amd64)';			\
	fi

	if [ -f 'staticserv (linux, arm, 5)' ]; then		\
		rm 'staticserv (linux, arm, 5)';		\
	fi

	if [ -f 'staticserv (windows, 386).exe' ]; then		\
		rm 'staticserv (windows, 386).exe';		\
	fi

	if [ -f 'staticserv (windows, amd64).exe' ]; then	\
		rm 'staticserv (windows, amd64).exe';		\
	fi


.PHONY: build-native
build-native:
	go build -o 'staticserv'


.PHONY: build-cross
build-cross:
	GOOS=linux	GOARCH=amd64		go build -o 'staticserv (linux, amd64)'
	GOOS=linux	GOARCH=arm	GOARM=5	go build -o 'staticserv (linux, arm, 5)'
	GOOS=windows	GOARCH=386		go build -o 'staticserv (windows, 386).exe'
	GOOS=windows	GOARCH=amd64		go build -o 'staticserv (windows, amd64).exe'


.PHONY: run
run:
	./staticserv


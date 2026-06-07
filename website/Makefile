.PHONY: dev build clean install-air

dev:
	@air -c .air.toml

build:
	go build -o bee .

clean:
	rm -rf tmp

install-air:
	go install github.com/air-verse/air@latest
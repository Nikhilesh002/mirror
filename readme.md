To run:
```sh
docker run --rm -v "$PWD":/usr/src/myapp -w /usr/src/myapp golang:latest go run main.go  
```

To build for macOS:
```sh
docker run --rm -v "$PWD":/usr/src/myapp -w /usr/src/myapp -e GOOS=darwin -e GOARCH=arm64 golang:latest sh -c 'go build -v -o mirror'
```
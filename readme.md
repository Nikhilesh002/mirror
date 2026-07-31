Mirror is a small request capture app. It stores HTTP requests, shows them in a live list, and lets you inspect headers, body, and JSON details.

Use `/api/store` to save a request and `/api/mirror` to preview one without storing it.

To run:
```sh
docker run --rm -v "$PWD":/usr/src/myapp -w /usr/src/myapp golang:latest go run main.go  
```

To build for macOS:
```sh
docker run --rm -v "$PWD":/usr/src/myapp -w /usr/src/myapp -e GOOS=darwin -e GOARCH=arm64 golang:latest sh -c 'go build -v -o mirror'
```
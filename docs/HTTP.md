# HTTP Package

Trux provides a native HTTP/1.1 server built on Linux `io_uring` through the imported `http` standard package.

```trux
import "http"

http.listen(port int)
http.get(path string, handler)
http.post(path string, handler)
http.put(path string, handler)
http.delete(path string, handler)
http.patch(path string, handler)
http.route(method string, path string, handler)
http.notFound(handler)

http.use(handler)
http.useAt(prefix string, handler)

http.set(request int, key string, val string)
http.context(request int, key string) string

http.param(request int, name string) string
http.method(request int) string
http.path(request int) string
http.query(request int) string
http.header(request int, name string) string
http.body(request int) string

http.setHeader(request int, name string, val string)
http.text(request int, status int, body string)
http.json(request int, status int, body string)
http.status(request int, status int)
http.respond(request int, status int, contentType string, body string)
```

`http.listen` is statement-only and blocks forever. Route handlers and middleware must have the signature:

```trux
func handle(request int) int {
    http.text(request, 200, "ok\n")
    return 0
}
```

Example:

```trux
package main

import "http"

func index(request int) int {
    http.text(request, 200, "hello from trux\n")
    return 0
}

func health(request int) int {
    http.text(request, 200, "ok\n")
    return 0
}

func echo(request int) int {
    let body string = http.body(request)
    http.text(request, 200, body)
    return 0
}

func main() int {
    http.get("/", index)
    http.get("/health", health)
    http.post("/echo", echo)
    http.listen(8080)
    return 0
}
```

See [examples/projects/http_server](https://github.com/thea1lab/trux/tree/main/examples/projects/http_server) and [examples/projects/http_multi_file](https://github.com/thea1lab/trux/tree/main/examples/projects/http_multi_file) for runnable servers.

## Runtime Model

The generated C includes `trux_http_runtime.h` when HTTP calls are used. On Linux, the runtime employs a shared-nothing thread-per-core architecture:

1. **`SO_REUSEPORT`**: Each worker thread binds its own socket; incoming connections are load-balanced across threads in kernel space.
2. **`io_uring`**: Each thread runs its own submission and completion queue. When the kernel allows it, accepted sockets are direct descriptors, so they do not consume a process file descriptor. A thread adds another ring when its descriptor table is full. If direct descriptors are unavailable, the thread falls back to ordinary file descriptors.
3. **Kernel Buffer Rings (`io_uring_register_buf_ring`)**: The kernel selects receive buffers on packet arrival (`IOSQE_BUFFER_SELECT`). The connection copies the bytes out and returns that buffer. An idle connection does not keep a private read buffer. A large body grows the buffer up to the existing limit, and the buffer is released once the request has been handled.
4. **Accept and receive**: Each ring keeps one multishot accept and a small set of one-shot accepts in flight, because multishot accept can stall when many connections arrive together. Receive stays multishot for the life of the connection.
5. **Memory Isolation**: Each request receives its own arena and `rt_context`, isolating allocations between requests and worker threads.
6. **GPU requests**: A handler may call `gpu.finish` instead of responding. The arena stays alive until the continuation copies the response into the connection buffer. The worker keeps polling the ring while that kernel runs. See [GPU.md](GPU.md) and [TXM.md](TXM.md).

## HTTPS

The server speaks plaintext HTTP/1.1. It does not terminate TLS, and it has no certificate or private-key API.

For now, HTTPS works only through a reverse proxy. The proxy accepts HTTPS from clients and forwards plaintext HTTP to the Trux process. Caddy and Nginx both do this. A Caddy site block for a server on port 8080:

```
example.com {
    reverse_proxy 127.0.0.1:8080
}
```

`http.listen` binds `0.0.0.0`, so the plaintext port is reachable on every interface. To accept plaintext only from a proxy on the same machine, bind the loopback address with `http.serve`. A worker count of `0` uses one worker per core, the same choice `http.listen` makes:

```trux
http.serve("127.0.0.1", 8080, 0)
```

## Limits

- Linux is required. The HTTP runtime uses `io_uring`. Other systems stop with `http is supported on Linux only`.
- In Docker containers, require `--security-opt seccomp=unconfined --ulimit memlock=-1:-1` for kernel buffer ring allocation.
- No HTTPS in the server. Terminate TLS at a reverse proxy and forward plaintext HTTP to this process. See [HTTPS](#https).
- `port` must be between `1` and `65535`.
- Request bodies use `Content-Length` and are capped at 10 MiB. Chunked request bodies are rejected.
- If a handler returns without calling a respond helper, the runtime sends `500 text/plain`.
- If a handler calls a respond helper more than once, only the first response is sent.

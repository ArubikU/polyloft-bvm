# polyloft.http

`polyloft.http` is the typed HTTP facade layer built on top of the global runtime-backed `Http` module.

```pf
import polyloft.http { Http, HttpHeaders, HttpServer, HttpHandler, HttpServerRequest, HttpServerResponse }
```

## Overview

This module provides:

- client helpers through `Http`
- typed request and response wrappers
- a small route-based `HttpServer`
- handler interfaces for request processing and error handling

The underlying native work is still executed by the global `Http` module documented in [../stdlib.md](../stdlib.md).

## Interfaces

### HttpHandler

- `handle(request: HttpServerRequest, response: HttpServerResponse) -> void`

### HttpErrorHandler

- `handle(message: String, request: HttpServerRequest, response: HttpServerResponse) -> void`

## HttpHeaders

Constructors and factories:

- `HttpHeaders()`
- `HttpHeaders(values: map<string, String>)`
- `HttpHeaders.from(values: map<string, String>) -> HttpHeaders`

Methods:

- `get(name: String) -> String`
- `set(name: String, value: String) -> HttpHeaders`
- `contains(name: String) -> bool`
- `delete(name: String) -> bool`
- `asMap() -> map<string, String>`
- `keys() -> array<string>`
- `values() -> array<String>`
- `isEmpty() -> bool`

## HttpClientResponse

Fields:

- `statusCode: int`
- `body: String`
- `headers: HttpHeaders`

Factories and helpers:

- `HttpClientResponse.fromRaw(raw: map) -> HttpClientResponse`
- `isSuccess() -> bool`
- `isClientError() -> bool`
- `isServerError() -> bool`
- `json() -> any`

## HttpServerRequest

Fields:

- `method: String`
- `path: String`
- `body: String`
- `headers: HttpHeaders`

Factories and helpers:

- `HttpServerRequest.fromRaw(raw: map) -> HttpServerRequest`
- `json() -> any`

`json()` returns `nil` when the request body is empty.

## Http

Client helpers:

- `Http.get(url: String) -> HttpClientResponse`
- `Http.get(url: String, timeoutSecs: int) -> HttpClientResponse`
- `Http.post(url: String, data: any) -> HttpClientResponse`
- `Http.post(url: String, data: any, timeoutSecs: int) -> HttpClientResponse`
- `Http.put(url: String, data: any) -> HttpClientResponse`
- `Http.put(url: String, data: any, timeoutSecs: int) -> HttpClientResponse`
- `Http.delete(url: String) -> HttpClientResponse`
- `Http.delete(url: String, timeoutSecs: int) -> HttpClientResponse`
- `Http.request(method: String, url: String, body: any, timeoutMillis: int, headers: HttpHeaders) -> HttpClientResponse`
- `Http.createServer() -> HttpServer`

## HttpServerResponse

Fluent response builders:

- `status(code: int) -> HttpServerResponse`
- `header(name: String, value: String) -> HttpServerResponse`
- `json(data: any) -> HttpServerResponse`
- `send(content: String) -> HttpServerResponse`
- `html(content: String) -> HttpServerResponse`
- `ok(data: any) -> HttpServerResponse`
- `created(data: any) -> HttpServerResponse`
- `noContent() -> HttpServerResponse`
- `notFound() -> HttpServerResponse`
- `notFound(message: String) -> HttpServerResponse`
- `error(code: int, message: String) -> HttpServerResponse`
- `toRaw() -> map<string, any>`

## HttpServer

Server methods:

- `config(options: map) -> void`
- `use(middleware: any) -> void`
- `get(path: String, handler: HttpHandler) -> void`
- `post(path: String, handler: HttpHandler) -> void`
- `put(path: String, handler: HttpHandler) -> void`
- `delete(path: String, handler: HttpHandler) -> void`
- `onError(handler: HttpErrorHandler) -> void`
- `log(message: String, level: String) -> void`
- `listen(port: int) -> void`

Notes:

- routing keys are currently exact `METHOD:path` matches
- `config(...)` and `use(...)` are placeholders today and do not yet provide middleware behavior
- if no route matches and no error handler is installed, the server returns `notFound()` automatically

## Example

```pf
import polyloft.http { Http, HttpHandler }

let response = Http.get("https://example.com")
println(response.statusCode)

class HelloHandler implements HttpHandler:
    def handle(request, response) -> void:
        response.ok({ "path": request.path })
    end
end

let server = Http.createServer()
server.get("/hello", HelloHandler())
```
package c

const HTTPHeaderName = "trux_http_runtime.h"

const HTTPSource = `
#ifndef TRUX_HTTP_RUNTIME_H
#define TRUX_HTTP_RUNTIME_H

#if defined(__linux__)

#ifndef _GNU_SOURCE
#define _GNU_SOURCE
#endif

#include <arpa/inet.h>
#include <ctype.h>
#include <errno.h>
#include <fcntl.h>
#include <sys/types.h>
/* liburing.h includes <atomic> in C++ before its own extern "C" block.
   Wrapping the include in extern "C" makes those templates ill-formed. */
#include <liburing.h>
#include <poll.h>
#include <netinet/in.h>
#include <netinet/tcp.h>
#include <pthread.h>
#include <sched.h>
#include <signal.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/time.h>
#include <sys/uio.h>
#include <unistd.h>

#ifndef SO_REUSEPORT
#define SO_REUSEPORT 15
#endif

#define RT_HTTP_MAX_HEADER_BYTES 65536
#define RT_HTTP_MAX_BODY_BYTES 10485760
#define RT_HTTP_READ_TIMEOUT_SECONDS 15
#define RT_HTTP_MAX_ROUTES 128
#define RT_HTTP_MAX_MIDDLEWARES 64
#define RT_HTTP_MAX_PARAMS 16
#define RT_HTTP_MAX_CONTEXT 32
#define RT_HTTP_MAX_RESP_HEADERS 32
#define RT_HTTP_MAX_HEADERS 64

#define RT_URING_ENTRIES 2048
#define RT_BUF_RING_SIZE 1024
#define RT_BUF_SIZE 4096
#define RT_BUF_GROUP_ID 1
#define RT_MAX_CONNS 65536
#define RT_HTTP_MAX_RINGS 8
#define RT_HTTP_DIRECT_SLOTS_DEFAULT 65536
#define RT_HTTP_SINGLE_ACCEPTS 128
#define RT_HTTP_BUF_INITIAL 4096
#define RT_HTTP_BUF_POOL 16
#define RT_HTTP_BUF_POOL_MAX 65536
#define RT_HTTP_OP_RECV 1u
#define RT_HTTP_OP_ACCEPT 2u
#define RT_HTTP_OP_SEND 3u
#define RT_HTTP_OP_CLOSE 4u
#define RT_HTTP_OP_SOCKOPT 5u
#define RT_HTTP_OP_GPU 6u
#define RT_HTTP_OP_GPU_TIMEOUT 7u
#define RT_HTTP_OP_CANCEL 8u
#define RT_HTTP_OP_SHUTDOWN 9u
#ifndef SOCKET_URING_OP_SETSOCKOPT
#define SOCKET_URING_OP_SETSOCKOPT 3
#endif

typedef int64_t (*rt_http_handler_fn)(rt_context* trux_ctx, rt_arena* trux_result_arena, int64_t request);

typedef struct {
    const uint8_t* name;
    size_t name_len;
    const uint8_t* value;
    size_t value_len;
} rt_http_header_pair;

typedef struct {
    rt_string key;
    rt_string val;
} rt_http_kv;

typedef struct {
    rt_string method;
    rt_string path;
    rt_http_handler_fn handler;
    bool uses_gpu;
} rt_http_route_entry;

typedef struct {
    rt_string prefix;
    rt_http_handler_fn handler;
} rt_http_middleware_entry;

typedef struct {
    rt_http_route_entry routes[RT_HTTP_MAX_ROUTES];
    size_t route_count;
    rt_http_middleware_entry middlewares[RT_HTTP_MAX_MIDDLEWARES];
    size_t middleware_count;
    rt_http_handler_fn not_found_handler;
} rt_http_router;

static rt_http_router g_trux_http_router = {0};

typedef struct rt_http_ring rt_http_ring;
typedef struct rt_http_worker rt_http_worker;

typedef struct {
    int fd;
    int slot;
    int ring_index;
    uint8_t* buffer;
    size_t len;
    size_t cap;
    size_t header_scan_start;
    uint8_t* tx_buf;
    size_t tx_cap;
    size_t tx_sent;
    size_t tx_total;
    bool tx_in_flight;
    bool close_after_tx;
    bool in_use;
    bool closing;
    bool drain_after_tx;
    int next_ready_ring;
    int next_ready_slot;
    bool in_ready_queue;
} rt_http_conn;

struct rt_http_ring {
    struct io_uring ring;
    struct io_uring_buf_ring* br;
    uint8_t* buf_storage;
    rt_http_conn* conns;
    unsigned nslots;
    unsigned live;
    int index;
    int listen_fd;
    int singles;
    int multi_on;
    int pending_submit;
    bool fixed;
    bool active;
    rt_http_worker* worker;
};

struct rt_http_worker {
    rt_http_ring rings[RT_HTTP_MAX_RINGS];
    int nrings;
    int listen_fd;
    int wait_cursor;
    unsigned slots;
    bool fixed;
};

typedef struct {
    int fd;
    int ring_index;
    struct io_uring* ring;
    rt_http_ring* http_ring;
    bool* need_submit;
    rt_http_conn* conn;
    const uint8_t* method;
    size_t method_len;
    const uint8_t* path;
    size_t path_len;
    const uint8_t* query;
    size_t query_len;
    const uint8_t* body;
    size_t body_len;
    rt_http_header_pair headers[RT_HTTP_MAX_HEADERS];
    size_t header_count;

    rt_http_kv params[RT_HTTP_MAX_PARAMS];
    size_t param_count;

    rt_http_kv context[RT_HTTP_MAX_CONTEXT];
    size_t context_count;

    rt_http_kv resp_headers[RT_HTTP_MAX_RESP_HEADERS];
    size_t resp_header_count;

    bool responded;
    bool close_after_response;
    bool parked;
    rt_http_handler_fn park_cont;
} rt_http_request;

typedef struct {
    rt_http_request* req;
    rt_http_handler_fn cont;
    rt_http_park_slot slot;
} rt_http_gpu_park;

static __thread rt_http_gpu_park* g_rt_http_parked;
static __thread rt_http_gpu_park g_rt_http_park_storage;
static __thread rt_http_request g_rt_http_park_req_storage;
static __thread rt_context g_rt_http_worker_ctx;
static __thread int g_rt_http_ready_head_ring = -1;
static __thread int g_rt_http_ready_head_slot = -1;
static __thread int g_rt_http_ready_tail_ring = -1;
static __thread int g_rt_http_ready_tail_slot = -1;
static __thread rt_http_worker* g_rt_http_worker;
static __thread uint8_t* rt_http_pool_ptr[RT_HTTP_BUF_POOL];
static __thread size_t rt_http_pool_cap[RT_HTTP_BUF_POOL];
static __thread int rt_http_pool_n;
static int g_rt_http_tcp_nodelay = 1;
static __thread int g_rt_http_in_continuation;
static __thread uint32_t g_rt_http_gpu_gen;
static __thread uint32_t g_rt_http_gpu_timer_gen;
static __thread bool g_rt_http_gpu_timer_armed;
static __thread struct __kernel_timespec g_rt_http_gpu_ts;

static RT_UNUSED const char* rt_http_reason(int64_t status) {
    switch (status) {
    case 200: return "OK";
    case 201: return "Created";
    case 202: return "Accepted";
    case 204: return "No Content";
    case 301: return "Moved Permanently";
    case 302: return "Found";
    case 304: return "Not Modified";
    case 400: return "Bad Request";
    case 401: return "Unauthorized";
    case 403: return "Forbidden";
    case 404: return "Not Found";
    case 405: return "Method Not Allowed";
    case 408: return "Request Timeout";
    case 413: return "Payload Too Large";
    case 500: return "Internal Server Error";
    case 502: return "Bad Gateway";
    case 503: return "Service Unavailable";
    default: return "OK";
    }
}

static RT_UNUSED int rt_http_ascii_lower(int c) {
    if (c >= 'A' && c <= 'Z') return c + ('a' - 'A');
    return c;
}

static RT_UNUSED bool rt_http_token_equal_ci(const uint8_t* left, size_t left_len, const uint8_t* right, size_t right_len) {
    if (left_len != right_len) return false;
    for (size_t i = 0; i < left_len; i++) {
        if (rt_http_ascii_lower(left[i]) != rt_http_ascii_lower(right[i])) return false;
    }
    return true;
}

static RT_UNUSED bool rt_http_string_equal_ci(rt_string left, const uint8_t* right, size_t right_len) {
    return rt_http_token_equal_ci(left.data, left.len, right, right_len);
}

static RT_UNUSED void rt_http_trim_ows(uint8_t** data, size_t* len) {
    while (*len > 0 && ((*data)[0] == ' ' || (*data)[0] == '\t')) {
        (*data)++;
        (*len)--;
    }
    while (*len > 0 && ((*data)[*len - 1] == ' ' || (*data)[*len - 1] == '\t')) {
        (*len)--;
    }
}

static RT_UNUSED bool rt_http_parse_content_length(const uint8_t* data, size_t len, size_t* out) {
    size_t value = 0;
    if (len == 0) return false;
    for (size_t i = 0; i < len; i++) {
        if (data[i] < '0' || data[i] > '9') return false;
        size_t digit = (size_t)(data[i] - '0');
        if (value > (SIZE_MAX - digit) / 10) return false;
        value = value * 10 + digit;
    }
    *out = value;
    return true;
}

static RT_UNUSED bool rt_http_contains_token_ci(const uint8_t* data, size_t len, const uint8_t* token, size_t token_len) {
    const uint8_t* cursor = data;
    size_t remaining = len;
    for (;;) {
        const uint8_t* comma = (const uint8_t*)memchr(cursor, ',', remaining);
        size_t part_len = comma == NULL ? remaining : (size_t)(comma - cursor);
        uint8_t* part = (uint8_t*)cursor;
        rt_http_trim_ows(&part, &part_len);
        if (rt_http_token_equal_ci(part, part_len, token, token_len)) return true;
        if (comma == NULL) return false;
        size_t consumed = (size_t)(comma + 1 - cursor);
        cursor = comma + 1;
        remaining -= consumed;
    }
}

static RT_UNUSED ssize_t rt_http_find_header_end(const uint8_t* buf, size_t len, size_t start) {
    if (len < 4) return -1;
    if (start > 3) start -= 3;
    for (size_t i = start; i + 3 < len; i++) {
        if (buf[i] == '\r' && buf[i + 1] == '\n' && buf[i + 2] == '\r' && buf[i + 3] == '\n') {
            return (ssize_t)(i + 4);
        }
    }
    return -1;
}

static RT_UNUSED bool rt_http_match_route(rt_string pattern, const uint8_t* path, size_t path_len, rt_http_kv* params, size_t* param_count, size_t max_params) {
    size_t pat_idx = 0;
    size_t path_idx = 0;
    *param_count = 0;

    while (pat_idx < pattern.len || path_idx < path_len) {
        while (pat_idx < pattern.len && pattern.data[pat_idx] == '/') pat_idx++;
        while (path_idx < path_len && path[path_idx] == '/') path_idx++;

        if (pat_idx >= pattern.len && path_idx >= path_len) return true;
        if (pat_idx >= pattern.len || path_idx >= path_len) return false;

        size_t pat_start = pat_idx;
        while (pat_idx < pattern.len && pattern.data[pat_idx] != '/') pat_idx++;
        size_t pat_seg_len = pat_idx - pat_start;

        size_t path_start = path_idx;
        while (path_idx < path_len && path[path_idx] != '/') path_idx++;
        size_t path_seg_len = path_idx - path_start;

        if (pattern.data[pat_start] == ':') {
            if (*param_count < max_params) {
                params[*param_count].key.data = pattern.data + pat_start + 1;
                params[*param_count].key.len = pat_seg_len - 1;
                params[*param_count].val.data = path + path_start;
                params[*param_count].val.len = path_seg_len;
                (*param_count)++;
            }
        } else {
            if (pat_seg_len != path_seg_len || memcmp(pattern.data + pat_start, path + path_start, pat_seg_len) != 0) {
                return false;
            }
        }
    }
    return true;
}

static RT_UNUSED bool rt_http_match_prefix(rt_string prefix, const uint8_t* path, size_t path_len) {
    if (prefix.len == 0 || prefix.data == NULL) return true;
    if (path_len < prefix.len) return false;
    if (memcmp(path, prefix.data, prefix.len) != 0) return false;
    if (path_len == prefix.len || path[prefix.len] == '/') return true;
    return false;
}

static RT_UNUSED bool rt_http_send_all_iov(int fd, struct iovec* iov, int iovcnt) {
    while (iovcnt > 0) {
        ssize_t n = writev(fd, iov, iovcnt);
        if (n < 0) {
            if (errno == EINTR) continue;
            return false;
        }
        if (n == 0) return false;
        size_t sent = (size_t)n;
        while (iovcnt > 0 && sent >= iov[0].iov_len) {
            sent -= iov[0].iov_len;
            iov++;
            iovcnt--;
        }
        if (iovcnt > 0 && sent > 0) {
            iov[0].iov_base = (uint8_t*)iov[0].iov_base + sent;
            iov[0].iov_len -= sent;
        }
    }
    return true;
}

static RT_UNUSED rt_string rt_http_clone_string(rt_arena* arena, const uint8_t* data, size_t len) {
    if (len == 0 || data == NULL) return RT_LIT(rt_string, NULL, 0);
    uint8_t* out = (uint8_t*)rt_arena_alloc(arena, len);
    memcpy(out, data, len);
    return RT_LIT(rt_string, out, len);
}

static RT_UNUSED rt_http_request* rt_http_request_from_handle(int64_t handle) {
    rt_http_request* req = (rt_http_request*)(intptr_t)handle;
    if (req == NULL) rt_runtime_fail("invalid http request");
    return req;
}

static RT_UNUSED void rt_http_register_route(rt_string method, rt_string path, rt_http_handler_fn handler) {
    if (g_trux_http_router.route_count >= RT_HTTP_MAX_ROUTES) {
        rt_runtime_fail("exceeded maximum number of http routes");
    }
    g_trux_http_router.routes[g_trux_http_router.route_count++] = RT_LIT(rt_http_route_entry, method, path, handler, false);
}

static RT_UNUSED void rt_http_register_route_gpu(rt_string method, rt_string path, rt_http_handler_fn handler) {
    if (g_trux_http_router.route_count >= RT_HTTP_MAX_ROUTES) {
        rt_runtime_fail("exceeded maximum number of http routes");
    }
    g_trux_http_router.routes[g_trux_http_router.route_count++] = RT_LIT(rt_http_route_entry, method, path, handler, true);
}

static RT_UNUSED void rt_http_register_middleware(rt_string prefix, rt_http_handler_fn handler) {
    if (g_trux_http_router.middleware_count >= RT_HTTP_MAX_MIDDLEWARES) {
        rt_runtime_fail("exceeded maximum number of http middlewares");
    }
    g_trux_http_router.middlewares[g_trux_http_router.middleware_count++] = RT_LIT(rt_http_middleware_entry, prefix, handler);
}

static RT_UNUSED void rt_http_register_not_found(rt_http_handler_fn handler) {
    g_trux_http_router.not_found_handler = handler;
}

static RT_UNUSED rt_string rt_http_method(rt_arena* arena, int64_t handle) {
    (void)arena;
    rt_http_request* req = rt_http_request_from_handle(handle);
    return RT_LIT(rt_string, req->method, req->method_len);
}

static RT_UNUSED rt_string rt_http_path(rt_arena* arena, int64_t handle) {
    (void)arena;
    rt_http_request* req = rt_http_request_from_handle(handle);
    return RT_LIT(rt_string, req->path, req->path_len);
}

static RT_UNUSED rt_string rt_http_query(rt_arena* arena, int64_t handle) {
    (void)arena;
    rt_http_request* req = rt_http_request_from_handle(handle);
    return RT_LIT(rt_string, req->query, req->query_len);
}

static RT_UNUSED rt_string rt_http_body(rt_arena* arena, int64_t handle) {
    (void)arena;
    rt_http_request* req = rt_http_request_from_handle(handle);
    return RT_LIT(rt_string, req->body, req->body_len);
}

static RT_UNUSED rt_string rt_http_header(rt_arena* arena, int64_t handle, rt_string name) {
    (void)arena;
    rt_http_request* req = rt_http_request_from_handle(handle);
    for (size_t i = 0; i < req->header_count; i++) {
        rt_http_header_pair header = req->headers[i];
        if (rt_http_string_equal_ci(name, header.name, header.name_len)) {
            return RT_LIT(rt_string, header.value, header.value_len);
        }
    }
    return RT_LIT(rt_string, NULL, 0);
}

static RT_UNUSED rt_string rt_http_param(rt_arena* arena, int64_t handle, rt_string name) {
    (void)arena;
    rt_http_request* req = rt_http_request_from_handle(handle);
    for (size_t i = 0; i < req->param_count; i++) {
        if (req->params[i].key.len == name.len && memcmp(req->params[i].key.data, name.data, name.len) == 0) {
            return RT_LIT(rt_string, req->params[i].val.data, req->params[i].val.len);
        }
    }
    return RT_LIT(rt_string, NULL, 0);
}

static RT_UNUSED void rt_http_set_context(int64_t handle, rt_string key, rt_string val) {
    rt_http_request* req = rt_http_request_from_handle(handle);
    for (size_t i = 0; i < req->context_count; i++) {
        if (req->context[i].key.len == key.len && memcmp(req->context[i].key.data, key.data, key.len) == 0) {
            req->context[i].val = val;
            return;
        }
    }
    if (req->context_count < RT_HTTP_MAX_CONTEXT) {
        req->context[req->context_count++] = RT_LIT(rt_http_kv, key, val);
    }
}

static RT_UNUSED rt_string rt_http_context(rt_arena* arena, int64_t handle, rt_string key) {
    rt_http_request* req = rt_http_request_from_handle(handle);
    for (size_t i = 0; i < req->context_count; i++) {
        if (req->context[i].key.len == key.len && memcmp(req->context[i].key.data, key.data, key.len) == 0) {
            return rt_http_clone_string(arena, req->context[i].val.data, req->context[i].val.len);
        }
    }
    return RT_LIT(rt_string, NULL, 0);
}

static RT_UNUSED void rt_http_set_header(int64_t handle, rt_string name, rt_string val) {
    rt_http_request* req = rt_http_request_from_handle(handle);
    if (req->resp_header_count < RT_HTTP_MAX_RESP_HEADERS) {
        req->resp_headers[req->resp_header_count++] = RT_LIT(rt_http_kv, name, val);
    }
}

static RT_UNUSED uint64_t rt_http_ud(unsigned op, unsigned ring, unsigned slot) {
    return ((uint64_t)op << 56) | ((uint64_t)ring << 48) | (uint64_t)slot;
}

static RT_UNUSED unsigned rt_http_ud_op(uint64_t ud) {
    return (unsigned)(ud >> 56);
}

static RT_UNUSED unsigned rt_http_ud_ring(uint64_t ud) {
    return (unsigned)((ud >> 48) & 0xffu);
}

static RT_UNUSED unsigned rt_http_ud_slot(uint64_t ud) {
    return (unsigned)ud;
}

static RT_UNUSED void rt_http_buf_release(uint8_t* p, size_t cap) {
    if (p == NULL) return;
    if (cap == 0 || cap > RT_HTTP_BUF_POOL_MAX || rt_http_pool_n >= RT_HTTP_BUF_POOL) {
        free(p);
        return;
    }
    rt_http_pool_ptr[rt_http_pool_n] = p;
    rt_http_pool_cap[rt_http_pool_n] = cap;
    rt_http_pool_n++;
}

static RT_UNUSED uint8_t* rt_http_buf_acquire(size_t* cap) {
    if (rt_http_pool_n > 0) {
        rt_http_pool_n--;
        *cap = rt_http_pool_cap[rt_http_pool_n];
        uint8_t* p = rt_http_pool_ptr[rt_http_pool_n];
        rt_http_pool_ptr[rt_http_pool_n] = NULL;
        rt_http_pool_cap[rt_http_pool_n] = 0;
        return p;
    }
    *cap = RT_HTTP_BUF_INITIAL;
    return (uint8_t*)malloc(*cap);
}

static RT_UNUSED int rt_http_buf_reserve(rt_http_conn* c, size_t need) {
    if (c->buffer != NULL && need <= c->cap) return 1;
    size_t ncap = c->cap >= (size_t)RT_HTTP_BUF_INITIAL ? c->cap : (size_t)RT_HTTP_BUF_INITIAL;
    while (ncap < need) {
        if (ncap > (SIZE_MAX / 2)) return 0;
        ncap *= 2;
    }
    size_t limit = (size_t)RT_HTTP_MAX_HEADER_BYTES + (size_t)RT_HTTP_MAX_BODY_BYTES;
    if (need > limit) return 0;
    if (ncap > limit) ncap = limit;
    if (ncap < need) return 0;
    uint8_t* nbuf = (uint8_t*)realloc(c->buffer, ncap);
    if (nbuf == NULL) return 0;
    c->buffer = nbuf;
    c->cap = ncap;
    return 1;
}

static RT_UNUSED void rt_http_release_idle_in(rt_http_conn* c) {
    if (c->len != 0 || c->buffer == NULL) return;
    rt_http_buf_release(c->buffer, c->cap);
    c->buffer = NULL;
    c->cap = 0;
    c->header_scan_start = 0;
}

static RT_UNUSED void rt_http_release_tx(rt_http_conn* c) {
    if (c->tx_buf == NULL || c->tx_in_flight) return;
    rt_http_buf_release(c->tx_buf, c->tx_cap);
    c->tx_buf = NULL;
    c->tx_cap = 0;
    c->tx_sent = 0;
    c->tx_total = 0;
}

static RT_UNUSED void rt_http_conn_free_bufs(rt_http_conn* c) {
    c->tx_in_flight = false;
    if (c->buffer != NULL) {
        rt_http_buf_release(c->buffer, c->cap);
        c->buffer = NULL;
        c->cap = 0;
    }
    if (c->tx_buf != NULL) {
        rt_http_buf_release(c->tx_buf, c->tx_cap);
        c->tx_buf = NULL;
        c->tx_cap = 0;
    }
    c->len = 0;
    c->tx_sent = 0;
    c->tx_total = 0;
    c->header_scan_start = 0;
}

static RT_UNUSED int rt_http_tx_reserve(rt_http_conn* c, size_t need) {
    if (c->tx_buf != NULL && c->tx_cap >= need) return 1;
    if (c->tx_buf != NULL) {
        rt_http_buf_release(c->tx_buf, c->tx_cap);
        c->tx_buf = NULL;
        c->tx_cap = 0;
    }
    for (int i = 0; i < rt_http_pool_n; i++) {
        if (rt_http_pool_cap[i] >= need) {
            c->tx_buf = rt_http_pool_ptr[i];
            c->tx_cap = rt_http_pool_cap[i];
            rt_http_pool_n--;
            rt_http_pool_ptr[i] = rt_http_pool_ptr[rt_http_pool_n];
            rt_http_pool_cap[i] = rt_http_pool_cap[rt_http_pool_n];
            rt_http_pool_ptr[rt_http_pool_n] = NULL;
            rt_http_pool_cap[rt_http_pool_n] = 0;
            return 1;
        }
    }
    size_t cap = RT_HTTP_BUF_INITIAL;
    while (cap < need) {
        if (cap > (SIZE_MAX / 2)) return 0;
        cap *= 2;
    }
    c->tx_buf = (uint8_t*)malloc(cap);
    if (c->tx_buf == NULL) return 0;
    c->tx_cap = cap;
    return 1;
}

static RT_UNUSED struct io_uring_sqe* rt_http_get_sqe(rt_http_ring* r) {
    struct io_uring_sqe* sqe = io_uring_get_sqe(&r->ring);
    if (sqe == NULL) {
        io_uring_submit(&r->ring);
        r->pending_submit = 0;
        sqe = io_uring_get_sqe(&r->ring);
    }
    return sqe;
}

static RT_UNUSED void rt_http_mark_submit(rt_http_ring* r, bool* need_submit) {
    r->pending_submit = 1;
    if (need_submit != NULL) *need_submit = true;
}

static RT_UNUSED int rt_http_ring_has_room(const rt_http_ring* r) {
    unsigned inflight = (unsigned)r->singles + (r->multi_on ? 1u : 0u);
    return r->live + inflight < r->nslots;
}

static RT_UNUSED unsigned rt_http_single_target(const rt_http_ring* r) {
    unsigned want = RT_HTTP_SINGLE_ACCEPTS;
    if (r->nslots < want * 2u) {
        want = r->nslots / 4u;
        if (want < 1u) want = 1u;
    }
    return want;
}

static RT_UNUSED void rt_http_close_conn(rt_http_ring* r, rt_http_conn* c, bool* need_submit);

static RT_UNUSED bool rt_http_queue_send(rt_http_ring* r, rt_http_conn* c, bool* need_submit) {
    if (r == NULL || c == NULL || c->tx_buf == NULL || c->tx_total <= c->tx_sent) return false;
    struct io_uring_sqe* sqe = rt_http_get_sqe(r);
    if (sqe == NULL) return false;
    io_uring_prep_send(sqe, c->fd, c->tx_buf + c->tx_sent, c->tx_total - c->tx_sent, MSG_NOSIGNAL);
    if (r->fixed) sqe->flags |= IOSQE_FIXED_FILE;
    sqe->user_data = rt_http_ud(RT_HTTP_OP_SEND, (unsigned)r->index, (unsigned)c->slot);
    c->tx_in_flight = true;
    rt_http_mark_submit(r, need_submit);
    return true;
}

static RT_UNUSED void rt_http_arm_recv(rt_http_ring* r, rt_http_conn* c, bool* need_submit) {
    if (c == NULL || !c->in_use || c->closing) return;
    struct io_uring_sqe* sqe = rt_http_get_sqe(r);
    if (sqe == NULL) return;
    io_uring_prep_recv_multishot(sqe, c->fd, NULL, 0, 0);
    sqe->flags |= IOSQE_BUFFER_SELECT;
    if (r->fixed) sqe->flags |= IOSQE_FIXED_FILE;
    sqe->buf_group = RT_BUF_GROUP_ID;
    sqe->user_data = rt_http_ud(RT_HTTP_OP_RECV, (unsigned)r->index, (unsigned)c->slot);
    rt_http_mark_submit(r, need_submit);
}

static RT_UNUSED void rt_http_queue_nodelay(rt_http_ring* r, rt_http_conn* c, bool* need_submit) {
    struct io_uring_sqe* sqe = rt_http_get_sqe(r);
    if (sqe == NULL) return;
    memset(sqe, 0, sizeof(*sqe));
    sqe->opcode = IORING_OP_URING_CMD;
    sqe->fd = c->fd;
    sqe->cmd_op = SOCKET_URING_OP_SETSOCKOPT;
    sqe->addr = ((uint64_t)TCP_NODELAY << 32) | (uint32_t)IPPROTO_TCP;
    sqe->file_index = (uint32_t)sizeof(g_rt_http_tcp_nodelay);
    sqe->addr3 = (uint64_t)(uintptr_t)&g_rt_http_tcp_nodelay;
    sqe->flags = IOSQE_FIXED_FILE;
    sqe->user_data = rt_http_ud(RT_HTTP_OP_SOCKOPT, (unsigned)r->index, (unsigned)c->slot);
    rt_http_mark_submit(r, need_submit);
}

static RT_UNUSED void rt_http_arm_accept(rt_http_ring* r, int multi, bool* need_submit) {
    if (!rt_http_ring_has_room(r)) return;
    struct io_uring_sqe* sqe = rt_http_get_sqe(r);
    if (sqe == NULL) return;
    if (r->fixed) {
        if (multi) io_uring_prep_multishot_accept_direct(sqe, r->listen_fd, NULL, NULL, 0);
        else io_uring_prep_accept_direct(sqe, r->listen_fd, NULL, NULL, 0, IORING_FILE_INDEX_ALLOC);
    } else if (multi) {
        io_uring_prep_multishot_accept(sqe, r->listen_fd, NULL, NULL, 0);
    } else {
        io_uring_prep_accept(sqe, r->listen_fd, NULL, NULL, 0);
    }
    sqe->user_data = rt_http_ud(RT_HTTP_OP_ACCEPT, (unsigned)r->index, multi ? 0u : 1u);
    if (multi) r->multi_on = 1;
    else r->singles++;
    rt_http_mark_submit(r, need_submit);
}

static RT_UNUSED void rt_http_arm_accepts(rt_http_ring* r, bool* need_submit) {
    if (r == NULL || !r->active) return;
    if (!r->multi_on && rt_http_ring_has_room(r)) rt_http_arm_accept(r, 1, need_submit);
    unsigned want = rt_http_single_target(r);
    while ((unsigned)r->singles < want && rt_http_ring_has_room(r)) {
        int before = r->singles;
        rt_http_arm_accept(r, 0, need_submit);
        if (r->singles == before) break;
    }
}

static RT_UNUSED void rt_http_queue_fixed_close(rt_http_ring* r, rt_http_conn* c, bool* need_submit) {
    struct io_uring_sqe* shut = rt_http_get_sqe(r);
    if (shut != NULL) {
        io_uring_prep_shutdown(shut, c->fd, SHUT_RDWR);
        shut->flags |= IOSQE_FIXED_FILE;
        shut->user_data = rt_http_ud(RT_HTTP_OP_SHUTDOWN, (unsigned)r->index, (unsigned)c->slot);
        rt_http_mark_submit(r, need_submit);
    }
    struct io_uring_sqe* sqe = rt_http_get_sqe(r);
    if (sqe == NULL) return;
    io_uring_prep_close_direct(sqe, (unsigned)c->slot);
    sqe->user_data = rt_http_ud(RT_HTTP_OP_CLOSE, (unsigned)r->index, (unsigned)c->slot);
    rt_http_mark_submit(r, need_submit);
}

static RT_UNUSED void rt_http_close_conn(rt_http_ring* r, rt_http_conn* c, bool* need_submit) {
    if (r == NULL || c == NULL || !c->in_use || c->closing) return;
    c->closing = true;
    if (!r->fixed) {
        shutdown(c->fd, SHUT_RDWR);
        close(c->fd);
        rt_http_conn_free_bufs(c);
        c->in_use = false;
        c->closing = false;
        if (r->live > 0) r->live--;
        rt_http_arm_accepts(r, need_submit);
        return;
    }
    /* A multishot recv never finishes, and a later close waits behind it.
       Cancel every request on the direct descriptor first. */
    struct io_uring_sqe* sqe = rt_http_get_sqe(r);
    if (sqe == NULL) {
        c->closing = false;
        return;
    }
    io_uring_prep_cancel_fd(sqe, c->fd, IORING_ASYNC_CANCEL_ALL | IORING_ASYNC_CANCEL_FD_FIXED);
    sqe->user_data = rt_http_ud(RT_HTTP_OP_CANCEL, (unsigned)r->index, (unsigned)c->slot);
    rt_http_mark_submit(r, need_submit);
}

static RT_UNUSED void rt_http_send_simple(rt_http_ring* r, rt_http_conn* c, int status, const char* body, bool close_conn, bool* need_submit) {
    if (r == NULL || c == NULL || !c->in_use || c->closing) return;
    char header[256];
    size_t body_len = body == NULL ? 0 : strlen(body);
    int n = snprintf(header, sizeof(header),
        "HTTP/1.1 %d %s\r\nContent-Type: text/plain\r\nContent-Length: %zu\r\nConnection: %s\r\n\r\n",
        status, rt_http_reason(status), body_len, close_conn ? "close" : "keep-alive");
    if (n < 0 || (size_t)n >= sizeof(header)) {
        rt_http_close_conn(r, c, need_submit);
        return;
    }
    size_t total = (size_t)n + body_len;
    if (c->tx_in_flight || !rt_http_tx_reserve(c, total)) {
        c->close_after_tx = true;
        if (!c->tx_in_flight) rt_http_close_conn(r, c, need_submit);
        return;
    }
    memcpy(c->tx_buf, header, (size_t)n);
    if (body_len > 0) memcpy(c->tx_buf + (size_t)n, body, body_len);
    c->tx_sent = 0;
    c->tx_total = total;
    c->close_after_tx = close_conn;
    if (!rt_http_queue_send(r, c, need_submit)) {
        if (!r->fixed) {
            struct iovec iov;
            memset(&iov, 0, sizeof(iov));
            iov.iov_base = c->tx_buf;
            iov.iov_len = total;
            if (rt_http_send_all_iov(c->fd, &iov, 1)) {
                c->tx_in_flight = false;
                if (close_conn) rt_http_close_conn(r, c, need_submit);
                else rt_http_release_tx(c);
                return;
            }
        }
        rt_http_close_conn(r, c, need_submit);
    }
}

static RT_UNUSED void rt_http_respond(int64_t handle, int64_t status, rt_string content_type, rt_string body) {
    rt_http_request* req = rt_http_request_from_handle(handle);
    if (req->responded) return;
    if (status < 100 || status > 999) status = 500;

    char header[4096];
    int pos = snprintf(header, sizeof(header),
        "HTTP/1.1 %lld %s\r\nContent-Type: %.*s\r\nContent-Length: %zu\r\nConnection: %s\r\n",
        (long long)status,
        rt_http_reason(status),
        (int)content_type.len,
        content_type.data == NULL ? (const uint8_t*)"" : content_type.data,
        body.len,
        req->close_after_response ? "close" : "keep-alive");

    if (pos > 0 && (size_t)pos < sizeof(header)) {
        for (size_t i = 0; i < req->resp_header_count; i++) {
            rt_http_kv kv = req->resp_headers[i];
            int added = snprintf(header + pos, sizeof(header) - (size_t)pos, "%.*s: %.*s\r\n",
                (int)kv.key.len, kv.key.data == NULL ? (const uint8_t*)"" : kv.key.data,
                (int)kv.val.len, kv.val.data == NULL ? (const uint8_t*)"" : kv.val.data);
            if (added > 0 && (size_t)(pos + added) < sizeof(header)) {
                pos += added;
            } else {
                break;
            }
        }
        if ((size_t)(pos + 2) < sizeof(header)) {
            header[pos++] = '\r';
            header[pos++] = '\n';
        }
    }

    if (pos < 0 || (size_t)pos >= sizeof(header)) {
        req->close_after_response = true;
        req->responded = true;
        return;
    }

    size_t body_len = (body.len > 0 && body.data != NULL) ? body.len : 0;
    size_t total_len = (size_t)pos + body_len;

    if (req->http_ring != NULL && req->conn != NULL && !req->conn->closing) {
        if (req->conn->tx_in_flight || !rt_http_tx_reserve(req->conn, total_len)) {
            req->close_after_response = true;
            req->responded = true;
            if (!req->conn->tx_in_flight) rt_http_close_conn(req->http_ring, req->conn, req->need_submit);
            else req->conn->close_after_tx = true;
            return;
        }
        memcpy(req->conn->tx_buf, header, (size_t)pos);
        if (body_len > 0) memcpy(req->conn->tx_buf + (size_t)pos, body.data, body_len);
        req->conn->tx_sent = 0;
        req->conn->tx_total = total_len;
        req->conn->close_after_tx = req->close_after_response;
        if (rt_http_queue_send(req->http_ring, req->conn, req->need_submit)) {
            req->responded = true;
            return;
        }
        if (!req->http_ring->fixed) {
            struct iovec iov;
            memset(&iov, 0, sizeof(iov));
            iov.iov_base = req->conn->tx_buf;
            iov.iov_len = total_len;
            if (rt_http_send_all_iov(req->fd, &iov, 1)) {
                req->conn->tx_in_flight = false;
                if (req->close_after_response) rt_http_close_conn(req->http_ring, req->conn, req->need_submit);
                else rt_http_release_tx(req->conn);
                req->responded = true;
                return;
            }
        }
        rt_http_close_conn(req->http_ring, req->conn, req->need_submit);
        req->responded = true;
        return;
    }

    req->close_after_response = true;
    req->responded = true;
}

static RT_UNUSED void rt_http_text(int64_t handle, int64_t status, rt_string body) {
    rt_http_respond(handle, status, RT_LIT(rt_string, (const uint8_t*)"text/plain", 10), body);
}

static RT_UNUSED void rt_http_json(int64_t handle, int64_t status, rt_string body) {
    rt_http_respond(handle, status, RT_LIT(rt_string, (const uint8_t*)"application/json", 16), body);
}

static RT_UNUSED void rt_http_status(int64_t handle, int64_t status) {
    rt_http_respond(handle, status, RT_LIT(rt_string, (const uint8_t*)"text/plain", 10), RT_LIT(rt_string, NULL, 0));
}

static RT_UNUSED bool rt_http_parse_request_headers(uint8_t* buf, size_t len, size_t header_end, rt_http_request* req, size_t* body_len_out) {
    (void)len;
    size_t header_len = header_end - 2;
    uint8_t* start = buf;
    uint8_t* end = buf + header_len;
    uint8_t* line_end = (uint8_t*)memchr(start, '\r', (size_t)(end - start));
    if (line_end == NULL || line_end + 1 >= end || line_end[1] != '\n') return false;

    uint8_t* sp1 = (uint8_t*)memchr(start, ' ', (size_t)(line_end - start));
    if (sp1 == NULL) return false;
    uint8_t* sp2 = (uint8_t*)memchr(sp1 + 1, ' ', (size_t)(line_end - (sp1 + 1)));
    if (sp2 == NULL) return false;

    req->method = start;
    req->method_len = (size_t)(sp1 - start);
    uint8_t* target = sp1 + 1;
    size_t target_len = (size_t)(sp2 - target);
    uint8_t* question = (uint8_t*)memchr(target, '?', target_len);
    if (question == NULL) {
        req->path = target;
        req->path_len = target_len;
        req->query = target + target_len;
        req->query_len = 0;
    } else {
        req->path = target;
        req->path_len = (size_t)(question - target);
        req->query = question + 1;
        req->query_len = target_len - req->path_len - 1;
    }

    uint8_t* version = sp2 + 1;
    size_t version_len = (size_t)(line_end - version);
    if (!(version_len == 8 && memcmp(version, "HTTP/1.", 7) == 0 && (version[7] == '0' || version[7] == '1'))) {
        return false;
    }
    req->close_after_response = (version[7] == '0');

    size_t content_length = 0;
    bool has_content_length = false;
    bool has_transfer_encoding = false;
    req->header_count = 0;

    uint8_t* line = line_end + 2;
    while (line < end) {
        uint8_t* next = (uint8_t*)memchr(line, '\r', (size_t)(end - line));
        if (next == NULL || next + 1 > end || next[1] != '\n') return false;
        uint8_t* colon = (uint8_t*)memchr(line, ':', (size_t)(next - line));
        if (colon == NULL) return false;

        uint8_t* name = line;
        size_t name_len = (size_t)(colon - line);
        uint8_t* value = colon + 1;
        size_t value_len = (size_t)(next - value);
        rt_http_trim_ows(&value, &value_len);

        if (req->header_count < RT_HTTP_MAX_HEADERS) {
            req->headers[req->header_count++] = RT_LIT(rt_http_header_pair, name, name_len, value, value_len);
        }

        if (rt_http_token_equal_ci(name, name_len, (const uint8_t*)"Content-Length", 14)) {
            if (has_content_length || has_transfer_encoding) return false;
            if (!rt_http_parse_content_length(value, value_len, &content_length)) return false;
            has_content_length = true;
        } else if (rt_http_token_equal_ci(name, name_len, (const uint8_t*)"Transfer-Encoding", 17)) {
            if (has_transfer_encoding || has_content_length) return false;
            has_transfer_encoding = true;
            return false;
        } else if (rt_http_token_equal_ci(name, name_len, (const uint8_t*)"Connection", 10)) {
            if (rt_http_contains_token_ci(value, value_len, (const uint8_t*)"close", 5)) {
                req->close_after_response = true;
            }
        }
        line = next + 2;
    }

    if (!has_content_length) content_length = 0;
    *body_len_out = content_length;
    return true;
}

static RT_UNUSED const uint8_t* rt_http_dup_bytes(rt_arena* arena, const uint8_t* data, size_t len) {
    if (data == NULL || len == 0) return NULL;
    uint8_t* out = (uint8_t*)rt_arena_alloc(arena, len);
    memcpy(out, data, len);
    return out;
}

static RT_UNUSED void rt_http_arm_gpu_poll(struct io_uring* ring, bool* need_submit) {
    int fd = rt_gpu_event_fd();
    if (fd < 0 || ring == NULL) return;
    struct io_uring_sqe* sqe = io_uring_get_sqe(ring);
    if (sqe == NULL) {
        io_uring_submit(ring);
        sqe = io_uring_get_sqe(ring);
    }
    if (sqe == NULL) return;
    io_uring_prep_poll_add(sqe, fd, POLLIN);
    sqe->user_data = rt_http_ud(RT_HTTP_OP_GPU, 0, 0);
    if (need_submit != NULL) *need_submit = true;
}

static RT_UNUSED void rt_http_arm_gpu_timeout(struct io_uring* ring, bool* need_submit) {
    if (rt_gpu_event_fd() < 0 || ring == NULL || g_rt_http_gpu_timer_armed) return;
    g_rt_http_gpu_ts.tv_sec = 1;
    g_rt_http_gpu_ts.tv_nsec = 0;
    struct io_uring_sqe* sqe = io_uring_get_sqe(ring);
    if (sqe == NULL) {
        io_uring_submit(ring);
        sqe = io_uring_get_sqe(ring);
    }
    if (sqe == NULL) return;
    g_rt_http_gpu_timer_gen = g_rt_http_gpu_gen;
    g_rt_http_gpu_timer_armed = true;
    io_uring_prep_timeout(sqe, &g_rt_http_gpu_ts, 0, 0);
    sqe->user_data = rt_http_ud(RT_HTTP_OP_GPU_TIMEOUT, 0, g_rt_http_gpu_timer_gen);
    if (need_submit != NULL) *need_submit = true;
}

static RT_UNUSED void rt_http_trim_worker_pool(rt_context* ctx) {
    size_t kept = 0;
    rt_arena_chunk** link = &ctx->chunk_pool;
    while (*link != NULL) {
        rt_arena_chunk* c = *link;
        if (kept < 16 && c->cap <= 65536) {
            kept++;
            link = &c->next;
        } else {
            *link = c->next;
            free(c);
        }
    }
}

static RT_UNUSED void rt_http_enqueue_ready(rt_http_conn* c) {
    if (c == NULL || g_rt_http_worker == NULL || !c->in_use || c->len == 0 || c->in_ready_queue || c->closing) return;
    c->next_ready_ring = -1;
    c->next_ready_slot = -1;
    c->in_ready_queue = true;
    if (g_rt_http_ready_tail_ring >= 0) {
        rt_http_conn* tail = &g_rt_http_worker->rings[g_rt_http_ready_tail_ring].conns[g_rt_http_ready_tail_slot];
        tail->next_ready_ring = c->ring_index;
        tail->next_ready_slot = c->slot;
    } else {
        g_rt_http_ready_head_ring = c->ring_index;
        g_rt_http_ready_head_slot = c->slot;
    }
    g_rt_http_ready_tail_ring = c->ring_index;
    g_rt_http_ready_tail_slot = c->slot;
}

static RT_UNUSED void rt_http_gpu_finish(int64_t handle, rt_http_handler_fn continuation) {
    if (g_rt_http_in_continuation) rt_runtime_fail("gpu.finish cannot be called from a GPU continuation");
    rt_http_request* req = rt_http_request_from_handle(handle);
    if (req->responded) rt_runtime_fail("gpu.finish cannot run after the handler responded");
    if (req->parked) rt_runtime_fail("gpu.finish can only be called once");
    if (continuation == NULL) rt_runtime_fail("gpu.finish continuation is missing");
    req->parked = true;
    req->park_cont = continuation;
    rt_gpu_submit_signal();
}

static RT_UNUSED void rt_http_retain_parked(rt_http_request* stack_req, rt_arena* stack_arena, rt_context* stack_ctx) {
    rt_http_gpu_park* park = &g_rt_http_park_storage;
    memset(park, 0, sizeof(*park));
    if (!rt_http_park_begin(&park->slot, stack_arena, stack_ctx)) {
        rt_runtime_fail("a GPU request is already in flight on this worker");
    }
    rt_arena* arena = park->slot.arena;
    rt_http_request* req = &g_rt_http_park_req_storage;
    *req = *stack_req;
    req->method = rt_http_dup_bytes(arena, stack_req->method, stack_req->method_len);
    req->path = rt_http_dup_bytes(arena, stack_req->path, stack_req->path_len);
    req->query = rt_http_dup_bytes(arena, stack_req->query, stack_req->query_len);
    req->body = rt_http_dup_bytes(arena, stack_req->body, stack_req->body_len);
    for (size_t i = 0; i < stack_req->header_count && i < RT_HTTP_MAX_HEADERS; i++) {
        rt_http_header_pair header = stack_req->headers[i];
        req->headers[i].name = rt_http_dup_bytes(arena, header.name, header.name_len);
        req->headers[i].name_len = header.name_len;
        req->headers[i].value = rt_http_dup_bytes(arena, header.value, header.value_len);
        req->headers[i].value_len = header.value_len;
    }
    for (size_t i = 0; i < stack_req->param_count && i < RT_HTTP_MAX_PARAMS; i++) {
        rt_http_kv kv = stack_req->params[i];
        req->params[i].key.data = rt_http_dup_bytes(arena, kv.key.data, kv.key.len);
        req->params[i].key.len = kv.key.len;
        req->params[i].val.data = rt_http_dup_bytes(arena, kv.val.data, kv.val.len);
        req->params[i].val.len = kv.val.len;
    }
    for (size_t i = 0; i < stack_req->context_count && i < RT_HTTP_MAX_CONTEXT; i++) {
        rt_http_kv kv = stack_req->context[i];
        req->context[i].key.data = rt_http_dup_bytes(arena, kv.key.data, kv.key.len);
        req->context[i].key.len = kv.key.len;
        req->context[i].val.data = rt_http_dup_bytes(arena, kv.val.data, kv.val.len);
        req->context[i].val.len = kv.val.len;
    }
    for (size_t i = 0; i < stack_req->resp_header_count && i < RT_HTTP_MAX_RESP_HEADERS; i++) {
        rt_http_kv kv = stack_req->resp_headers[i];
        req->resp_headers[i].key.data = rt_http_dup_bytes(arena, kv.key.data, kv.key.len);
        req->resp_headers[i].key.len = kv.key.len;
        req->resp_headers[i].val.data = rt_http_dup_bytes(arena, kv.val.data, kv.val.len);
        req->resp_headers[i].val.len = kv.val.len;
    }
    park->req = req;
    park->cont = stack_req->park_cont;
    g_rt_http_parked = park;
    rt_http_arm_gpu_timeout(stack_req->ring, stack_req->need_submit);
}

static RT_UNUSED void rt_http_dispatch(rt_http_request* req) {
    rt_arena arena;
    rt_arena_init_frame(&arena, &g_rt_http_worker_ctx);
    g_rt_http_worker_ctx.arena = &arena;

    int match_idx = -1;
    for (size_t i = 0; i < g_trux_http_router.route_count; i++) {
        rt_http_route_entry* r = &g_trux_http_router.routes[i];
        if (r->method.len > 0 && r->method.data != NULL) {
            if (!rt_http_string_equal_ci(r->method, req->method, req->method_len)) continue;
        }
        if (rt_http_match_route(r->path, req->path, req->path_len, req->params, &req->param_count, RT_HTTP_MAX_PARAMS)) {
            match_idx = (int)i;
            break;
        }
    }

    bool aborted = false;
    for (size_t i = 0; i < g_trux_http_router.middleware_count; i++) {
        rt_http_middleware_entry* m = &g_trux_http_router.middlewares[i];
        if (rt_http_match_prefix(m->prefix, req->path, req->path_len)) {
            int64_t ret = m->handler(&g_rt_http_worker_ctx, &arena, (int64_t)(intptr_t)req);
            if (ret != 0 || req->responded) {
                aborted = true;
                break;
            }
        }
    }

    if (!aborted && !req->responded) {
        if (match_idx >= 0) {
            g_trux_http_router.routes[match_idx].handler(&g_rt_http_worker_ctx, &arena, (int64_t)(intptr_t)req);
        } else if (g_trux_http_router.not_found_handler != NULL) {
            g_trux_http_router.not_found_handler(&g_rt_http_worker_ctx, &arena, (int64_t)(intptr_t)req);
        } else {
            rt_http_send_simple(req->http_ring, req->conn, 404, "not found\n", req->close_after_response, req->need_submit);
            req->responded = true;
        }
    }

    if (req->parked) {
        if (req->responded) rt_runtime_fail("gpu.finish and a response cannot both happen in the same handler");
        rt_http_retain_parked(req, &arena, &g_rt_http_worker_ctx);
        return;
    }

    if (!req->responded) {
        rt_http_send_simple(req->http_ring, req->conn, 500, "handler did not respond\n", req->close_after_response, req->need_submit);
        req->responded = true;
    }

    rt_arena_deinit(&arena);
    rt_http_trim_worker_pool(&g_rt_http_worker_ctx);
}

typedef struct {
    char host[128];
    int port;
    int worker_index;
} rt_http_thread_args;

static RT_UNUSED void rt_http_drain_conn(rt_http_ring* r, rt_http_conn* c, bool* need_submit) {
    if (r == NULL || c == NULL || !c->in_use || c->closing) return;
    if (c->tx_in_flight) {
        c->drain_after_tx = true;
        return;
    }
    for (;;) {
        if (c->buffer == NULL || c->len == 0) {
            rt_http_release_idle_in(c);
            return;
        }
        ssize_t hdr_end = rt_http_find_header_end(c->buffer, c->len, c->header_scan_start);
        if (hdr_end < 0) {
            if (c->len > RT_HTTP_MAX_HEADER_BYTES) {
                rt_http_send_simple(r, c, 431, "request header fields too large\n", true, need_submit);
                return;
            }
            if (c->len > 3) c->header_scan_start = c->len - 3;
            return;
        }

        rt_http_request req;
        memset(&req, 0, sizeof(req));
        req.fd = c->slot;
        req.ring_index = r->index;
        req.ring = &r->ring;
        req.http_ring = r;
        req.need_submit = need_submit;
        req.conn = c;

        size_t content_length = 0;
        if (!rt_http_parse_request_headers(c->buffer, c->len, (size_t)hdr_end, &req, &content_length)) {
            rt_http_send_simple(r, c, 400, "bad request\n", true, need_submit);
            return;
        }

        if (content_length > RT_HTTP_MAX_BODY_BYTES) {
            rt_http_send_simple(r, c, 413, "payload too large\n", true, need_submit);
            return;
        }

        if (c->len < (size_t)hdr_end + content_length) return;

        if (g_rt_http_parked != NULL) {
            bool matches_gpu = false;
            for (size_t i = 0; i < g_trux_http_router.route_count; i++) {
                rt_http_route_entry* route = &g_trux_http_router.routes[i];
                if (route->uses_gpu && rt_http_token_equal_ci(route->method.data, route->method.len, req.method, req.method_len) &&
                    rt_http_match_route(route->path, req.path, req.path_len, req.params, &req.param_count, RT_HTTP_MAX_PARAMS)) {
                    matches_gpu = true;
                    break;
                }
            }
            if (matches_gpu) {
                rt_http_enqueue_ready(c);
                return;
            }
        }

        req.body = c->buffer + hdr_end;
        req.body_len = content_length;
        rt_http_dispatch(&req);

        size_t consumed = (size_t)hdr_end + content_length;
        if (consumed < c->len) {
            memmove(c->buffer, c->buffer + consumed, c->len - consumed);
            c->len -= consumed;
            c->header_scan_start = 0;
        } else {
            c->len = 0;
            c->header_scan_start = 0;
        }

        if (req.parked) {
            if (c->len == 0) rt_http_release_idle_in(c);
            else rt_http_enqueue_ready(c);
            return;
        }

        if (req.close_after_response || c->close_after_tx) {
            if (c->tx_in_flight) {
                c->close_after_tx = true;
                return;
            }
            if (c->in_use && !c->closing) rt_http_close_conn(r, c, need_submit);
            return;
        }

        if (c->tx_in_flight) {
            if (c->len > 0) c->drain_after_tx = true;
            else rt_http_release_idle_in(c);
            return;
        }

        if (c->len == 0) {
            rt_http_release_idle_in(c);
            return;
        }
    }
}

static RT_UNUSED void rt_http_drain_ready(bool* need_submit) {
    rt_http_worker* w = g_rt_http_worker;
    while (g_rt_http_ready_head_ring >= 0 && g_rt_http_parked == NULL && w != NULL) {
        int ring_i = g_rt_http_ready_head_ring;
        int slot = g_rt_http_ready_head_slot;
        if (ring_i < 0 || ring_i >= w->nrings || (unsigned)slot >= w->rings[ring_i].nslots) {
            g_rt_http_ready_head_ring = -1;
            g_rt_http_ready_head_slot = -1;
            g_rt_http_ready_tail_ring = -1;
            g_rt_http_ready_tail_slot = -1;
            return;
        }
        rt_http_ring* hr = &w->rings[ring_i];
        rt_http_conn* c = &hr->conns[slot];
        g_rt_http_ready_head_ring = c->next_ready_ring;
        g_rt_http_ready_head_slot = c->next_ready_slot;
        if (g_rt_http_ready_head_ring < 0) {
            g_rt_http_ready_tail_ring = -1;
            g_rt_http_ready_tail_slot = -1;
        }
        c->next_ready_ring = -1;
        c->next_ready_slot = -1;
        c->in_ready_queue = false;
        if (!c->in_use || c->len == 0 || c->closing) continue;
        rt_http_drain_conn(hr, c, need_submit);
    }
}

static RT_UNUSED void rt_http_gpu_release(struct io_uring* ring, bool* need_submit, int failed) {
    rt_http_gpu_park* park = g_rt_http_parked;
    if (park == NULL) {
        rt_http_arm_gpu_poll(ring, need_submit);
        return;
    }
    g_rt_http_parked = NULL;
    g_rt_http_gpu_gen++;
    g_rt_http_gpu_timer_armed = false;
    rt_gpu_clear_busy();
    rt_http_request* req = park->req;
    rt_http_worker* w = g_rt_http_worker;
    rt_http_ring* hr = NULL;
    rt_http_conn* conn = NULL;
    int alive = 0;
    if (w != NULL && req->ring_index >= 0 && req->ring_index < w->nrings) {
        hr = &w->rings[req->ring_index];
        if (req->fd >= 0 && (unsigned)req->fd < hr->nslots) {
            conn = &hr->conns[req->fd];
            alive = conn->in_use && !conn->closing;
        }
    }
    if (alive) {
        req->ring = &hr->ring;
        req->http_ring = hr;
        req->need_submit = need_submit;
        req->conn = conn;
        if (!failed && park->cont != NULL) {
            g_rt_http_in_continuation = 1;
            park->cont(park->slot.ctx, park->slot.arena, (int64_t)(intptr_t)req);
            g_rt_http_in_continuation = 0;
        }
        if (!req->responded) {
            const char* body = failed ? "gpu request failed\n" : "handler did not respond\n";
            rt_http_send_simple(hr, conn, 500, body, req->close_after_response, need_submit);
            req->responded = true;
        }
        if (req->close_after_response) {
            if (conn->tx_in_flight) conn->close_after_tx = true;
            else if (conn->in_use) rt_http_close_conn(hr, conn, need_submit);
        }
    }
    if (park->slot.arena != NULL && park->slot.ctx != NULL) {
        rt_arena_deinit(park->slot.arena);
        rt_arena_chunk* chunk = park->slot.ctx->chunk_pool;
        while (chunk != NULL) {
            rt_arena_chunk* next = chunk->next;
            chunk->next = g_rt_http_worker_ctx.chunk_pool;
            g_rt_http_worker_ctx.chunk_pool = chunk;
            chunk = next;
        }
        park->slot.ctx->chunk_pool = NULL;
        rt_http_trim_worker_pool(&g_rt_http_worker_ctx);
    }
    rt_http_park_complete(&park->slot, 1);
    rt_http_arm_gpu_poll(ring, need_submit);
    rt_http_drain_ready(need_submit);
}

static RT_UNUSED unsigned rt_http_direct_slots(void) {
    const char* env = getenv("TRUX_HTTP_DIRECT_SLOTS");
    if (env == NULL || env[0] == '\0') return RT_HTTP_DIRECT_SLOTS_DEFAULT;
    char* end = NULL;
    long value = strtol(env, &end, 10);
    if (end == env || value < 8 || value > 262144) return RT_HTTP_DIRECT_SLOTS_DEFAULT;
    return (unsigned)value;
}

static RT_UNUSED int rt_http_env_on(const char* name) {
    const char* env = getenv(name);
    return env != NULL && env[0] == '1';
}

static RT_UNUSED void rt_http_ring_fail_cleanup(rt_http_ring* r, int fixed, int ring_ready) {
    if (r->conns != NULL) {
        free(r->conns);
        r->conns = NULL;
    }
    if (r->br != NULL && ring_ready) {
        io_uring_free_buf_ring(&r->ring, r->br, RT_BUF_RING_SIZE, RT_BUF_GROUP_ID);
        r->br = NULL;
    }
    if (r->buf_storage != NULL) {
        free(r->buf_storage);
        r->buf_storage = NULL;
    }
    if (ring_ready) {
        if (fixed) io_uring_unregister_files(&r->ring);
        io_uring_queue_exit(&r->ring);
    }
}

static RT_UNUSED int rt_http_open_ring(rt_http_ring* r, rt_http_worker* w, int index, int listen_fd, int fixed) {
    memset(r, 0, sizeof(*r));
    r->index = index;
    r->listen_fd = listen_fd;
    r->worker = w;
    r->fixed = fixed ? true : false;
    r->nslots = fixed ? w->slots : (unsigned)RT_MAX_CONNS;
    struct io_uring_params params;
    memset(&params, 0, sizeof(params));
#ifdef IORING_SETUP_COOP_TASKRUN
    params.flags |= IORING_SETUP_COOP_TASKRUN;
#endif
#ifdef IORING_SETUP_SINGLE_ISSUER
    params.flags |= IORING_SETUP_SINGLE_ISSUER;
#endif
#ifdef IORING_SETUP_DEFER_TASKRUN
    params.flags |= IORING_SETUP_DEFER_TASKRUN;
#endif
    int ret = io_uring_queue_init_params(RT_URING_ENTRIES, &r->ring, &params);
    if (ret < 0) {
        ret = io_uring_queue_init(RT_URING_ENTRIES, &r->ring, 0);
        if (ret < 0) return -1;
    }
    if (fixed) {
        ret = io_uring_register_files_sparse(&r->ring, r->nslots);
        if (ret < 0) {
            io_uring_queue_exit(&r->ring);
            return -2;
        }
    }
    if (posix_memalign((void**)&r->buf_storage, 4096, (size_t)RT_BUF_RING_SIZE * (size_t)RT_BUF_SIZE) != 0) {
        rt_http_ring_fail_cleanup(r, fixed, 1);
        return -1;
    }
    int err = 0;
    r->br = io_uring_setup_buf_ring(&r->ring, RT_BUF_RING_SIZE, RT_BUF_GROUP_ID, 0, &err);
    if (r->br == NULL) {
        rt_http_ring_fail_cleanup(r, fixed, 1);
        return -1;
    }
    for (int i = 0; i < RT_BUF_RING_SIZE; i++) {
        io_uring_buf_ring_add(r->br, r->buf_storage + (size_t)i * RT_BUF_SIZE, RT_BUF_SIZE, i,
            io_uring_buf_ring_mask(RT_BUF_RING_SIZE), i);
    }
    io_uring_buf_ring_advance(r->br, RT_BUF_RING_SIZE);
    r->conns = (rt_http_conn*)calloc(r->nslots, sizeof(rt_http_conn));
    if (r->conns == NULL) {
        rt_http_ring_fail_cleanup(r, fixed, 1);
        return -1;
    }
    for (unsigned i = 0; i < r->nslots; i++) {
        r->conns[i].slot = (int)i;
        r->conns[i].fd = (int)i;
        r->conns[i].ring_index = index;
        r->conns[i].next_ready_ring = -1;
        r->conns[i].next_ready_slot = -1;
    }
    r->active = true;
    return 0;
}

static RT_UNUSED void rt_http_maybe_grow(rt_http_worker* w) {
    if (w == NULL || !w->fixed || w->nrings <= 0 || w->nrings >= RT_HTTP_MAX_RINGS) return;
    rt_http_ring* last = &w->rings[w->nrings - 1];
    if (last->live + 1u < last->nslots) return;
    int idx = w->nrings;
    if (rt_http_open_ring(&w->rings[idx], w, idx, w->listen_fd, 1) != 0) return;
    w->nrings++;
    rt_http_arm_accepts(&w->rings[idx], NULL);
}

static RT_UNUSED void rt_http_on_accept(rt_http_ring* r, unsigned kind, int res, unsigned flags, bool* need_submit) {
    int multi = kind == 0u;
    if (!multi && r->singles > 0) r->singles--;
    if (multi && (flags & IORING_CQE_F_MORE) == 0) r->multi_on = 0;
    if (res < 0) {
        if (res == -ENFILE || res == -EMFILE) rt_http_maybe_grow(r->worker);
        rt_http_arm_accepts(r, need_submit);
        return;
    }
    unsigned slot;
    int fd;
    if (r->fixed) {
        slot = (unsigned)res;
        if (slot >= r->nslots) {
            rt_http_arm_accepts(r, need_submit);
            return;
        }
        fd = (int)slot;
    } else {
        if (res >= (int)r->nslots) {
            close(res);
            rt_http_arm_accepts(r, need_submit);
            return;
        }
        slot = (unsigned)res;
        fd = res;
    }
    rt_http_conn* c = &r->conns[slot];
    if (c->in_use) {
        if (!r->fixed) close(fd);
        rt_http_arm_accepts(r, need_submit);
        return;
    }
    c->fd = fd;
    c->slot = (int)slot;
    c->ring_index = r->index;
    c->len = 0;
    c->header_scan_start = 0;
    c->tx_in_flight = false;
    c->tx_sent = 0;
    c->tx_total = 0;
    c->close_after_tx = false;
    c->closing = false;
    c->drain_after_tx = false;
    c->in_ready_queue = false;
    c->in_use = true;
    r->live++;
    if (r->fixed) rt_http_queue_nodelay(r, c, need_submit);
    else {
        int one = 1;
        (void)setsockopt(c->fd, IPPROTO_TCP, TCP_NODELAY, &one, sizeof(one));
    }
    rt_http_arm_recv(r, c, need_submit);
    rt_http_maybe_grow(r->worker);
    rt_http_arm_accepts(r, need_submit);
}

static RT_UNUSED void rt_http_on_send(rt_http_ring* r, rt_http_conn* c, int res, bool* need_submit) {
    if (c == NULL || !c->in_use) return;
    if (res > 0) {
        c->tx_sent += (size_t)res;
        if (c->tx_sent < c->tx_total) {
            c->tx_in_flight = false;
            if (!rt_http_queue_send(r, c, need_submit)) rt_http_close_conn(r, c, need_submit);
            return;
        }
    }
    c->tx_in_flight = false;
    c->tx_sent = 0;
    c->tx_total = 0;
    if (res <= 0 || c->close_after_tx) {
        rt_http_close_conn(r, c, need_submit);
        return;
    }
    rt_http_release_tx(c);
    if (c->len > 0 || c->drain_after_tx) {
        c->drain_after_tx = false;
        rt_http_drain_conn(r, c, need_submit);
    } else {
        rt_http_release_idle_in(c);
    }
}

static RT_UNUSED void rt_http_recycle_buf(rt_http_ring* r, unsigned flags) {
    int bid = (int)(flags >> IORING_CQE_BUFFER_SHIFT);
    if (r->br == NULL || r->buf_storage == NULL || bid < 0 || bid >= RT_BUF_RING_SIZE) return;
    uint8_t* in_data = r->buf_storage + (size_t)bid * RT_BUF_SIZE;
    io_uring_buf_ring_add(r->br, in_data, RT_BUF_SIZE, bid, io_uring_buf_ring_mask(RT_BUF_RING_SIZE), 0);
    io_uring_buf_ring_advance(r->br, 1);
}

static RT_UNUSED void rt_http_on_recv(rt_http_ring* r, rt_http_conn* c, int res, unsigned flags, bool* need_submit) {
    if (res > 0 && c->in_use && !c->closing) {
        int bid = (int)(flags >> IORING_CQE_BUFFER_SHIFT);
        if (bid < 0 || bid >= RT_BUF_RING_SIZE || r->buf_storage == NULL) {
            rt_http_recycle_buf(r, flags);
            rt_http_close_conn(r, c, need_submit);
            return;
        }
        size_t bytes = (size_t)res;
        if (c->buffer == NULL) {
            size_t cap = 0;
            c->buffer = rt_http_buf_acquire(&cap);
            c->cap = c->buffer == NULL ? 0 : cap;
        }
        if (!rt_http_buf_reserve(c, c->len + bytes)) {
            rt_http_recycle_buf(r, flags);
            rt_http_close_conn(r, c, need_submit);
            return;
        }
        memcpy(c->buffer + c->len, r->buf_storage + (size_t)bid * RT_BUF_SIZE, bytes);
        c->len += bytes;
        rt_http_recycle_buf(r, flags);
        rt_http_drain_conn(r, c, need_submit);
        if (c->in_use && !c->closing && (flags & IORING_CQE_F_MORE) == 0) {
            rt_http_arm_recv(r, c, need_submit);
        }
        return;
    }
    if ((flags & IORING_CQE_F_BUFFER) != 0) rt_http_recycle_buf(r, flags);
    if (!c->in_use || c->closing) return;
    if (res == -ENOBUFS) {
        rt_http_arm_recv(r, c, need_submit);
        return;
    }
    rt_http_close_conn(r, c, need_submit);
}

static RT_UNUSED void rt_http_handle_gpu(rt_http_ring* r, unsigned op, unsigned slot, bool* need_submit) {
    struct io_uring* ring = &r->ring;
    if (op == RT_HTTP_OP_GPU) {
        rt_gpu_drain_eventfd();
        int query = rt_gpu_query_job();
        if (g_rt_http_parked == NULL) {
            if (query == 1) rt_gpu_reset_stream();
            rt_http_arm_gpu_poll(ring, need_submit);
        } else if (query == 0) {
            rt_http_arm_gpu_poll(ring, need_submit);
        } else if (query == 1) {
            rt_gpu_reset_stream();
            rt_http_gpu_release(ring, need_submit, 1);
        } else {
            rt_http_gpu_release(ring, need_submit, 0);
        }
        return;
    }
    g_rt_http_gpu_timer_armed = false;
    if (g_rt_http_parked != NULL && slot == g_rt_http_gpu_gen) {
        int query = rt_gpu_query_job();
        if (query == 0) rt_http_arm_gpu_timeout(ring, need_submit);
        else if (query == 1) {
            rt_gpu_reset_stream();
            rt_http_gpu_release(ring, need_submit, 1);
        } else {
            rt_http_gpu_release(ring, need_submit, 0);
        }
    }
}

static RT_UNUSED void rt_http_drain_cq(rt_http_ring* r) {
    struct io_uring_cqe* cqe;
    unsigned head;
    bool need_submit = false;
    io_uring_for_each_cqe(&r->ring, head, cqe) {
        uint64_t udata = cqe->user_data;
        unsigned op = rt_http_ud_op(udata);
        unsigned slot = rt_http_ud_slot(udata);
        int res = cqe->res;
        unsigned flags = cqe->flags;
        io_uring_cq_advance(&r->ring, 1);
        if (op == RT_HTTP_OP_GPU || op == RT_HTTP_OP_GPU_TIMEOUT) {
            rt_http_handle_gpu(r, op, slot, &need_submit);
        } else if (op == RT_HTTP_OP_ACCEPT) {
            rt_http_on_accept(r, slot, res, flags, &need_submit);
        } else if (op == RT_HTTP_OP_CANCEL) {
            if (slot < r->nslots && r->conns[slot].in_use) {
                rt_http_queue_fixed_close(r, &r->conns[slot], &need_submit);
            }
        } else if (op == RT_HTTP_OP_SHUTDOWN) {
            (void)res;
        } else if (op == RT_HTTP_OP_CLOSE) {
            if (slot < r->nslots) {
                rt_http_conn* c = &r->conns[slot];
                rt_http_conn_free_bufs(c);
                if (c->in_use && r->live > 0) r->live--;
                c->in_use = false;
                c->closing = false;
            }
            rt_http_arm_accepts(r, &need_submit);
        } else if (op == RT_HTTP_OP_SEND) {
            if (slot < r->nslots) rt_http_on_send(r, &r->conns[slot], res, &need_submit);
        } else if (op == RT_HTTP_OP_RECV) {
            if (slot < r->nslots) rt_http_on_recv(r, &r->conns[slot], res, flags, &need_submit);
        }
    }
    if (need_submit) r->pending_submit = 1;
    (void)head;
}

static RT_UNUSED void rt_http_flush(rt_http_worker* w) {
    for (int i = 0; i < w->nrings; i++) {
        rt_http_ring* r = &w->rings[i];
        if (!r->active) continue;
        if (r->pending_submit) {
            io_uring_submit_and_get_events(&r->ring);
            r->pending_submit = 0;
        } else {
            io_uring_get_events(&r->ring);
        }
    }
}

static RT_UNUSED int rt_http_has_cqe(rt_http_worker* w) {
    for (int i = 0; i < w->nrings; i++) {
        struct io_uring_cqe* cqe;
        if (io_uring_peek_cqe(&w->rings[i].ring, &cqe) == 0) return 1;
    }
    return 0;
}

static RT_UNUSED void* rt_http_worker_thread(void* arg) {
    rt_http_thread_args* targs = (rt_http_thread_args*)arg;
    int worker_idx = targs->worker_index;
#if defined(__linux__)
    cpu_set_t cpuset;
    CPU_ZERO(&cpuset);
    long nproc = sysconf(_SC_NPROCESSORS_ONLN);
    if (nproc > 0) {
        CPU_SET((size_t)(worker_idx % nproc), &cpuset);
        (void)pthread_setaffinity_np(pthread_self(), sizeof(cpu_set_t), &cpuset);
    }
#endif

    int listen_fd = socket(AF_INET, SOCK_STREAM, 0);
    if (listen_fd < 0) rt_runtime_fail("worker socket creation failed");

    int one = 1;
    (void)setsockopt(listen_fd, SOL_SOCKET, SO_REUSEPORT, &one, sizeof(one));
    (void)setsockopt(listen_fd, SOL_SOCKET, SO_REUSEADDR, &one, sizeof(one));
    (void)setsockopt(listen_fd, IPPROTO_TCP, TCP_NODELAY, &one, sizeof(one));

    struct sockaddr_in addr;
    memset(&addr, 0, sizeof(addr));
    addr.sin_family = AF_INET;
    addr.sin_port = htons((uint16_t)targs->port);
    if (inet_pton(AF_INET, targs->host, &addr.sin_addr) != 1) {
        rt_runtime_fail("invalid http host IP address");
    }
    if (bind(listen_fd, (struct sockaddr*)&addr, sizeof(addr)) != 0) {
        rt_runtime_fail("worker bind failed");
    }
    if (listen(listen_fd, 4096) != 0) {
        rt_runtime_fail("worker listen failed");
    }

    rt_http_worker worker;
    memset(&worker, 0, sizeof(worker));
    worker.listen_fd = listen_fd;
    worker.slots = rt_http_direct_slots();
    g_rt_http_worker = &worker;
    g_rt_http_ready_head_ring = -1;
    g_rt_http_ready_head_slot = -1;
    g_rt_http_ready_tail_ring = -1;
    g_rt_http_ready_tail_slot = -1;

    int opened = 0;
    if (!rt_http_env_on("TRUX_HTTP_NO_DIRECT")) {
        int rc = rt_http_open_ring(&worker.rings[0], &worker, 0, listen_fd, 1);
        if (rc == 0) {
            worker.nrings = 1;
            worker.fixed = true;
            opened = 1;
        } else if (rc != -2 || rt_http_env_on("TRUX_HTTP_REQUIRE_DIRECT")) {
            rt_runtime_fail(rc == -2 ? "io_uring direct descriptors are unavailable" : "io_uring setup failed");
        }
    } else if (rt_http_env_on("TRUX_HTTP_REQUIRE_DIRECT")) {
        rt_runtime_fail("io_uring direct descriptors are unavailable");
    }
    if (!opened) {
        if (rt_http_open_ring(&worker.rings[0], &worker, 0, listen_fd, 0) != 0) {
            rt_runtime_fail("io_uring_queue_init failed");
        }
        worker.nrings = 1;
        worker.fixed = false;
    }

    rt_gpu_worker_init();
    {
        bool arm_gpu = false;
        rt_http_arm_gpu_poll(&worker.rings[0].ring, &arm_gpu);
        if (arm_gpu) worker.rings[0].pending_submit = 1;
    }
    rt_http_arm_accepts(&worker.rings[0], NULL);
    rt_http_flush(&worker);

    for (;;) {
        if (g_rt_http_parked != NULL) {
            int query = rt_gpu_query_job();
            if (query == 2 || query == 1) {
                bool need_sub = false;
                if (query == 1) rt_gpu_reset_stream();
                rt_http_gpu_release(&worker.rings[0].ring, &need_sub, query == 1);
                if (need_sub) worker.rings[0].pending_submit = 1;
            }
        }

        rt_http_flush(&worker);
        if (!rt_http_has_cqe(&worker)) {
            /* DEFER_TASKRUN posts completions only from io_uring_enter.
               Waiting on one ring at a time would hide the others, so a
               single ring blocks and several rings take turns. */
            struct io_uring_cqe* waited = NULL;
            int wait_rc;
            if (worker.nrings <= 1) {
                wait_rc = io_uring_wait_cqe(&worker.rings[0].ring, &waited);
            } else {
                struct __kernel_timespec ts;
                memset(&ts, 0, sizeof(ts));
                ts.tv_nsec = 1000000;
                int idx = worker.wait_cursor % worker.nrings;
                worker.wait_cursor++;
                wait_rc = io_uring_wait_cqe_timeout(&worker.rings[idx].ring, &waited, &ts);
                for (int i = 0; i < worker.nrings; i++) {
                    if (worker.rings[i].active) io_uring_get_events(&worker.rings[i].ring);
                }
            }
            if (wait_rc == -EINTR) continue;
            (void)waited;
        }

        int rings = worker.nrings;
        for (int i = 0; i < rings; i++) {
            if (worker.rings[i].active) rt_http_drain_cq(&worker.rings[i]);
        }
        for (int i = 0; i < worker.nrings; i++) rt_http_arm_accepts(&worker.rings[i], NULL);
        rt_http_flush(&worker);
    }
    return NULL;
}

static RT_UNUSED void rt_http_serve(rt_string host, int64_t port, int64_t workers) {
    if (port < 1 || port > 65535) rt_runtime_fail("http port must be between 1 and 65535");
    if (workers <= 0) {
        cpu_set_t cpuset;
        CPU_ZERO(&cpuset);
        if (sched_getaffinity(0, sizeof(cpuset), &cpuset) == 0) {
            workers = CPU_COUNT(&cpuset);
        }
        if (workers <= 0) {
            workers = sysconf(_SC_NPROCESSORS_ONLN);
        }
        if (workers < 1) workers = 1;
    }
    if (workers > 1024) workers = 1024;

#ifdef SIGPIPE
    signal(SIGPIPE, SIG_IGN);
#endif

    rt_http_thread_args args;
    memset(&args, 0, sizeof(args));
    args.port = (int)port;
    if (host.data != NULL && host.len > 0) {
        size_t len = host.len < sizeof(args.host) - 1 ? host.len : sizeof(args.host) - 1;
        memcpy(args.host, host.data, len);
        args.host[len] = '\0';
    } else {
        strcpy(args.host, "0.0.0.0");
    }

    pthread_t* threads = (pthread_t*)malloc((size_t)workers * sizeof(pthread_t));
    if (!threads) rt_runtime_fail("allocation failed for worker threads");
    rt_http_thread_args* thread_args = (rt_http_thread_args*)malloc((size_t)workers * sizeof(rt_http_thread_args));
    if (!thread_args) rt_runtime_fail("allocation failed for worker thread args");

    rt_gpu_publish_workers();
    for (int64_t i = 0; i < workers; i++) {
        thread_args[i] = args;
        thread_args[i].worker_index = (int)i;
        if (pthread_create(&threads[i], NULL, rt_http_worker_thread, &thread_args[i]) != 0) {
            rt_runtime_fail("failed to spawn worker thread");
        }
    }

    for (int64_t i = 0; i < workers; i++) {
        (void)pthread_join(threads[i], NULL);
    }
    free(thread_args);
    free(threads);
}

static RT_UNUSED void rt_http_listen(int64_t port) {
    rt_http_serve(RT_LIT(rt_string, (const uint8_t*)"0.0.0.0", 7), port, 0);
}

#else

typedef int64_t (*rt_http_handler_fn)(rt_context* trux_ctx, rt_arena* trux_result_arena, int64_t request);

static RT_UNUSED void rt_http_register_route(rt_string method, rt_string path, rt_http_handler_fn handler) { (void)method; (void)path; (void)handler; rt_runtime_fail("http is supported on Linux only"); }
static RT_UNUSED void rt_http_register_route_gpu(rt_string method, rt_string path, rt_http_handler_fn handler) { (void)method; (void)path; (void)handler; rt_runtime_fail("http is supported on Linux only"); }
static RT_UNUSED void rt_http_register_middleware(rt_string prefix, rt_http_handler_fn handler) { (void)prefix; (void)handler; rt_runtime_fail("http is supported on Linux only"); }
static RT_UNUSED void rt_http_register_not_found(rt_http_handler_fn handler) { (void)handler; rt_runtime_fail("http is supported on Linux only"); }
static RT_UNUSED void rt_http_listen(int64_t port) { (void)port; rt_runtime_fail("http is supported on Linux only"); }
static RT_UNUSED void rt_http_gpu_finish(int64_t handle, rt_http_handler_fn handler) { (void)handle; (void)handler; rt_runtime_fail("http is supported on Linux only"); }
static RT_UNUSED void rt_http_serve(rt_string host, int64_t port, int64_t workers) { (void)host; (void)port; (void)workers; rt_runtime_fail("http is supported on Linux only"); }
static RT_UNUSED rt_string rt_http_param(rt_arena* arena, int64_t handle, rt_string name) { (void)arena; (void)handle; (void)name; rt_runtime_fail("http is supported on Linux only"); return RT_LIT(rt_string, NULL, 0); }
static RT_UNUSED void rt_http_set_context(int64_t handle, rt_string key, rt_string val) { (void)handle; (void)key; (void)val; rt_runtime_fail("http is supported on Linux only"); }
static RT_UNUSED rt_string rt_http_context(rt_arena* arena, int64_t handle, rt_string key) { (void)arena; (void)handle; (void)key; rt_runtime_fail("http is supported on Linux only"); return RT_LIT(rt_string, NULL, 0); }
static RT_UNUSED void rt_http_set_header(int64_t handle, rt_string name, rt_string val) { (void)handle; (void)name; (void)val; rt_runtime_fail("http is supported on Linux only"); }
static RT_UNUSED void rt_http_text(int64_t handle, int64_t status, rt_string body) { (void)handle; (void)status; (void)body; rt_runtime_fail("http is supported on Linux only"); }
static RT_UNUSED void rt_http_json(int64_t handle, int64_t status, rt_string body) { (void)handle; (void)status; (void)body; rt_runtime_fail("http is supported on Linux only"); }
static RT_UNUSED void rt_http_status(int64_t handle, int64_t status) { (void)handle; (void)status; rt_runtime_fail("http is supported on Linux only"); }
static RT_UNUSED rt_string rt_http_method(rt_arena* arena, int64_t handle) { (void)arena; (void)handle; rt_runtime_fail("http is supported on Linux only"); return RT_LIT(rt_string, NULL, 0); }
static RT_UNUSED rt_string rt_http_path(rt_arena* arena, int64_t handle) { (void)arena; (void)handle; rt_runtime_fail("http is supported on Linux only"); return RT_LIT(rt_string, NULL, 0); }
static RT_UNUSED rt_string rt_http_query(rt_arena* arena, int64_t handle) { (void)arena; (void)handle; rt_runtime_fail("http is supported on Linux only"); return RT_LIT(rt_string, NULL, 0); }
static RT_UNUSED rt_string rt_http_header(rt_arena* arena, int64_t handle, rt_string name) { (void)arena; (void)handle; (void)name; rt_runtime_fail("http is supported on Linux only"); return RT_LIT(rt_string, NULL, 0); }
static RT_UNUSED rt_string rt_http_body(rt_arena* arena, int64_t handle) { (void)arena; (void)handle; rt_runtime_fail("http is supported on Linux only"); return RT_LIT(rt_string, NULL, 0); }
static RT_UNUSED void rt_http_respond(int64_t handle, int64_t status, rt_string content_type, rt_string body) { (void)handle; (void)status; (void)content_type; (void)body; rt_runtime_fail("http is supported on Linux only"); }

#endif

#endif
`

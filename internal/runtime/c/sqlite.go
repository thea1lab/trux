package c

import _ "embed"

const SQLiteHeaderName = "trux_sqlite_runtime.h"

//go:embed sqlite/sqlite3.h
var SQLite3HeaderSource string

//go:embed sqlite/sqlite3.c
var SQLite3CSource string

const SQLiteHeaderSource = `
#ifndef TRUX_SQLITE_RUNTIME_H
#define TRUX_SQLITE_RUNTIME_H

#include "sqlite3.h"
#include <sched.h>
#include <unistd.h>

#define RT_SQLITE_STMT_CACHE_SIZE 32

typedef struct trux_sqlite_db trux_sqlite_db;
typedef struct trux_sqlite_stmt trux_sqlite_stmt;

struct trux_sqlite_stmt {
    sqlite3_stmt* handle;
    trux_sqlite_db* db;
    char* sql_key;
    size_t sql_len;
    int is_finalized;
    int is_cached;
    int in_use;
};

struct trux_sqlite_db {
    sqlite3* handle;
    int is_open;
    int is_thread_local;
    int commits_since_ckpt;
    int checkpoint_frames;
    trux_sqlite_stmt* stmt_cache[RT_SQLITE_STMT_CACHE_SIZE];
    size_t stmt_cache_count;
};

static inline void rt_sqlite_cleanup_all(void) {}

#endif /* TRUX_SQLITE_RUNTIME_H */
`

const SQLiteImplSource = `
/* --- SQLite Runtime Implementation --- */
#define RT_SQLITE_TLS_CACHE_SIZE 16

typedef struct {
    char path[512];
    size_t path_len;
    trux_sqlite_db* db;
} rt_sqlite_tls_entry;

static __thread rt_sqlite_tls_entry g_trux_sqlite_tls_cache[RT_SQLITE_TLS_CACHE_SIZE];
static __thread size_t g_trux_sqlite_tls_count = 0;

/* usleep is not declared under _POSIX_C_SOURCE 200809L, which the runtime sets. */
static void rt_sqlite_sleep_us(long usec) {
    struct timespec ts;
    ts.tv_sec = (time_t)(usec / 1000000L);
    ts.tv_nsec = (long)(usec % 1000000L) * 1000L;
    nanosleep(&ts, NULL);
}

/* Give a lock a few milliseconds, then let the caller see SQLITE_BUSY.
 * Sleeping for seconds here stops every request on a single-threaded server. */
static int rt_sqlite_busy_handler(void* ptr, int count) {
    (void)ptr;
    if (count < 4) {
        sched_yield();
        return 1;
    }
    if (count < 20) {
        rt_sqlite_sleep_us(200);
        return 1;
    }
    return 0;
}

/* WAL checkpoint policy. A RESTART of a large log runs on the caller thread and
 * stops every request until it finishes. Passive checkpoints of about 4 MiB
 * (1000 frames) stay short enough that one core can drain the queue afterward.
 * journal_size_limit still caps the file. If a reader blocks the checkpoint,
 * the next try waits a few commits, unless the log has already hit the
 * memory-based ceiling. */
#define RT_SQLITE_CHECKPOINT_FRAMES_MIN 2000
#define RT_SQLITE_CHECKPOINT_FRAMES_MAX 20000
#define RT_SQLITE_CHECKPOINT_BATCH 1000
#define RT_SQLITE_CHECKPOINT_RETRY 32

static long long rt_sqlite_read_limit_file(const char* path) {
    FILE* f = fopen(path, "r");
    if (f == NULL) return 0;
    char buf[64] = {0};
    long long limit = 0;
    if (fgets(buf, (int)sizeof(buf), f) != NULL && strncmp(buf, "max", 3) != 0) {
        limit = atoll(buf);
    }
    fclose(f);
    return limit;
}

static int rt_sqlite_default_checkpoint_frames(void) {
    long long limit = rt_sqlite_read_limit_file("/sys/fs/cgroup/memory.max");
    if (limit <= 0) {
        limit = rt_sqlite_read_limit_file("/sys/fs/cgroup/memory/memory.limit_in_bytes");
    }
    if (limit <= 0 || limit > (1LL << 60)) {
        long pages = sysconf(_SC_PHYS_PAGES);
        long page_size = sysconf(_SC_PAGESIZE);
        if (pages > 0 && page_size > 0) {
            limit = (long long)pages * (long long)page_size;
        }
    }
    if (limit <= 0) return RT_SQLITE_CHECKPOINT_FRAMES_MAX;
    long long frames = limit / 32768; /* 4 KiB frames, WAL <= memory_limit / 8 */
    if (frames < RT_SQLITE_CHECKPOINT_FRAMES_MIN) frames = RT_SQLITE_CHECKPOINT_FRAMES_MIN;
    if (frames > RT_SQLITE_CHECKPOINT_FRAMES_MAX) frames = RT_SQLITE_CHECKPOINT_FRAMES_MAX;
    return (int)frames;
}

static int rt_sqlite_wal_hook(void* arg, sqlite3* db, const char* zDb, int nEntry) {
    trux_sqlite_db* tdb = (trux_sqlite_db*)arg;
    int ceiling = (tdb != NULL) ? tdb->checkpoint_frames : RT_SQLITE_CHECKPOINT_FRAMES_MAX;
    if (ceiling < RT_SQLITE_CHECKPOINT_BATCH) ceiling = RT_SQLITE_CHECKPOINT_BATCH;
    if (nEntry < RT_SQLITE_CHECKPOINT_BATCH) {
        if (tdb != NULL) tdb->commits_since_ckpt = 0;
        return SQLITE_OK;
    }
    if (tdb != NULL && nEntry < ceiling && tdb->commits_since_ckpt > 0 &&
        tdb->commits_since_ckpt < RT_SQLITE_CHECKPOINT_RETRY) {
        tdb->commits_since_ckpt++;
        return SQLITE_OK;
    }
    int in_log = 0;
    int checkpointed = 0;
    sqlite3_wal_checkpoint_v2(db, zDb, SQLITE_CHECKPOINT_PASSIVE, &in_log, &checkpointed);
    if (tdb != NULL) {
        if (in_log > 0 && checkpointed < in_log) tdb->commits_since_ckpt = 1;
        else tdb->commits_since_ckpt = 0;
    }
    return SQLITE_OK;
}

static RT_UNUSED trux_tuple_ptr_struct_14_trux_sqlite_db_error rt_sqlite_open(rt_arena* arena, rt_string path) {
    char* c_path = rt_string_to_c_string(path, "sqlite open path");
    trux_sqlite_db* db = (trux_sqlite_db*)calloc(1, sizeof(trux_sqlite_db));
    if (db == NULL) {
        free(c_path);
        return RT_LIT(trux_tuple_ptr_struct_14_trux_sqlite_db_error, NULL, rt_error_new(arena, RT_LIT(rt_string, (const uint8_t*)"failed to allocate sqlite db handle", 35)));
    }

    int rc = sqlite3_open(c_path, &db->handle);
    free(c_path);
    if (rc != SQLITE_OK) {
        const char* errmsg = db->handle ? sqlite3_errmsg(db->handle) : sqlite3_errstr(rc);
        rt_error err = rt_error_new(arena, rt_string_from_c(arena, errmsg));
        if (db->handle != NULL) {
            sqlite3_close(db->handle);
            db->handle = NULL;
        }
        free(db);
        return RT_LIT(trux_tuple_ptr_struct_14_trux_sqlite_db_error, NULL, err);
    }
    db->checkpoint_frames = rt_sqlite_default_checkpoint_frames();
    sqlite3_busy_handler(db->handle, rt_sqlite_busy_handler, NULL);
    (void)sqlite3_exec(db->handle, "PRAGMA synchronous=NORMAL; PRAGMA wal_autocheckpoint=0; PRAGMA temp_store=MEMORY; PRAGMA journal_size_limit=4194304; PRAGMA mmap_size=67108864; PRAGMA cache_size=-16384;", NULL, NULL, NULL);
    sqlite3_wal_hook(db->handle, rt_sqlite_wal_hook, db);
    db->is_open = 1;
    return RT_LIT(trux_tuple_ptr_struct_14_trux_sqlite_db_error, db, NULL);
}

static RT_UNUSED trux_tuple_ptr_struct_14_trux_sqlite_db_error rt_sqlite_open_thread_local(rt_arena* arena, rt_string path) {
    rt_check_string(path);
    /* An empty path asks for the connection this thread already opened.
       The hot path then skips reading the environment and reapplying pragmas. */
    if (path.len == 0) {
        for (size_t i = 0; i < g_trux_sqlite_tls_count; i++) {
            if (g_trux_sqlite_tls_cache[i].db != NULL && g_trux_sqlite_tls_cache[i].db->is_open) {
                return RT_LIT(trux_tuple_ptr_struct_14_trux_sqlite_db_error, g_trux_sqlite_tls_cache[i].db, NULL);
            }
        }
        return RT_LIT(trux_tuple_ptr_struct_14_trux_sqlite_db_error, NULL, rt_error_new(arena, RT_LIT(rt_string, (const uint8_t*)"sqlite thread-local connection is not open", 42)));
    }
    for (size_t i = 0; i < g_trux_sqlite_tls_count; i++) {
        if (g_trux_sqlite_tls_cache[i].db != NULL &&
            g_trux_sqlite_tls_cache[i].db->is_open &&
            g_trux_sqlite_tls_cache[i].path_len == path.len &&
            (path.len == 0 || memcmp(g_trux_sqlite_tls_cache[i].path, path.data, path.len) == 0)) {
            return RT_LIT(trux_tuple_ptr_struct_14_trux_sqlite_db_error, g_trux_sqlite_tls_cache[i].db, NULL);
        }
    }

    trux_tuple_ptr_struct_14_trux_sqlite_db_error res = rt_sqlite_open(arena, path);
    if (res.f1 != NULL) {
        return res;
    }

    trux_sqlite_db* db = res.f0;
    db->is_thread_local = 1;

    if (g_trux_sqlite_tls_count < RT_SQLITE_TLS_CACHE_SIZE && path.len < sizeof(g_trux_sqlite_tls_cache[0].path)) {
        if (path.len > 0) {
            memcpy(g_trux_sqlite_tls_cache[g_trux_sqlite_tls_count].path, path.data, path.len);
        }
        g_trux_sqlite_tls_cache[g_trux_sqlite_tls_count].path[path.len] = '\0';
        g_trux_sqlite_tls_cache[g_trux_sqlite_tls_count].path_len = path.len;
        g_trux_sqlite_tls_cache[g_trux_sqlite_tls_count].db = db;
        g_trux_sqlite_tls_count++;
    }
    return RT_LIT(trux_tuple_ptr_struct_14_trux_sqlite_db_error, db, NULL);
}

static RT_UNUSED rt_error rt_sqlite_close(rt_arena* arena, trux_sqlite_db* db) {
    if (db == NULL || !db->is_open || db->handle == NULL) {
        return rt_error_new(arena, RT_LIT(rt_string, (const uint8_t*)"database connection is not open", 31));
    }
    if (db->is_thread_local) {
        return NULL;
    }
    for (size_t i = 0; i < db->stmt_cache_count; i++) {
        trux_sqlite_stmt* s = db->stmt_cache[i];
        if (s != NULL) {
            if (s->handle != NULL) {
                sqlite3_finalize(s->handle);
                s->handle = NULL;
            }
            free(s->sql_key);
            free(s);
            db->stmt_cache[i] = NULL;
        }
    }
    db->stmt_cache_count = 0;
    int rc = sqlite3_close_v2(db->handle);
    db->handle = NULL;
    db->is_open = 0;
    free(db);

    if (rc != SQLITE_OK) {
        return rt_error_new(arena, rt_string_from_c(arena, sqlite3_errstr(rc)));
    }
    return NULL;
}

static RT_UNUSED rt_error rt_sqlite_exec(rt_arena* arena, trux_sqlite_db* db, rt_string sql) {
    if (db == NULL || !db->is_open || db->handle == NULL) {
        return rt_error_new(arena, RT_LIT(rt_string, (const uint8_t*)"database connection is not open", 31));
    }
    char* c_sql = rt_string_to_c_string(sql, "sqlite exec sql");
    char* errmsg = NULL;
    int rc = sqlite3_exec(db->handle, c_sql, NULL, NULL, &errmsg);
    free(c_sql);
    if (rc != SQLITE_OK) {
        rt_error err = rt_error_new(arena, rt_string_from_c(arena, errmsg ? errmsg : sqlite3_errstr(rc)));
        if (errmsg != NULL) {
            sqlite3_free(errmsg);
        }
        return err;
    }
    return NULL;
}

static RT_UNUSED trux_tuple_list_map_string_string_error rt_sqlite_query(rt_arena* arena, trux_sqlite_db* db, rt_string sql) {
    if (db == NULL || !db->is_open || db->handle == NULL) {
        return RT_LIT(trux_tuple_list_map_string_string_error, NULL, rt_error_new(arena, RT_LIT(rt_string, (const uint8_t*)"database connection is not open", 31)));
    }
    char* c_sql = rt_string_to_c_string(sql, "sqlite query sql");
    sqlite3_stmt* stmt = NULL;
    int rc = sqlite3_prepare_v2(db->handle, c_sql, -1, &stmt, NULL);
    free(c_sql);
    if (rc != SQLITE_OK) {
        return RT_LIT(trux_tuple_list_map_string_string_error, NULL, rt_error_new(arena, rt_string_from_c(arena, sqlite3_errmsg(db->handle))));
    }
    rt_list_map_string_string* rows = rt_list_map_string_string_new(arena, 0);
    int col_count = sqlite3_column_count(stmt);
    while ((rc = sqlite3_step(stmt)) == SQLITE_ROW) {
        rt_map_string_string* row = rt_map_string_string_new(arena, col_count);
        for (int i = 0; i < col_count; i++) {
            const char* col_name = sqlite3_column_name(stmt, i);
            const char* text_val = (const char*)sqlite3_column_text(stmt, i);
            rt_string k = rt_string_from_c(arena, col_name ? col_name : "");
            rt_string v = rt_string_from_c(arena, text_val ? text_val : "");
            rt_map_string_string_set(row, k, v);
        }
        rt_list_map_string_string_append(rows, row);
    }
    if (rc != SQLITE_DONE) {
        rt_error err = rt_error_new(arena, rt_string_from_c(arena, sqlite3_errmsg(db->handle)));
        sqlite3_finalize(stmt);
        return RT_LIT(trux_tuple_list_map_string_string_error, rows, err);
    }
    sqlite3_finalize(stmt);
    return RT_LIT(trux_tuple_list_map_string_string_error, rows, NULL);
}

static RT_UNUSED int64_t rt_sqlite_last_insert_rowid(rt_arena* arena, trux_sqlite_db* db) {
    (void)arena;
    if (db == NULL || !db->is_open || db->handle == NULL) {
        return 0;
    }
    return (int64_t)sqlite3_last_insert_rowid(db->handle);
}

static RT_UNUSED int64_t rt_sqlite_changes(rt_arena* arena, trux_sqlite_db* db) {
    (void)arena;
    if (db == NULL || !db->is_open || db->handle == NULL) {
        return 0;
    }
    return (int64_t)sqlite3_changes(db->handle);
}

static RT_UNUSED int64_t rt_sqlite_total_changes(rt_arena* arena, trux_sqlite_db* db) {
    (void)arena;
    if (db == NULL || !db->is_open || db->handle == NULL) {
        return 0;
    }
    return (int64_t)sqlite3_total_changes(db->handle);
}

static RT_UNUSED rt_error rt_sqlite_begin(rt_arena* arena, trux_sqlite_db* db) {
    if (db == NULL || !db->is_open || db->handle == NULL) {
        return rt_error_new(arena, RT_LIT(rt_string, (const uint8_t*)"database connection is not open", 31));
    }
    int rc = sqlite3_exec(db->handle, "BEGIN", NULL, NULL, NULL);
    if (rc != SQLITE_OK) {
        return rt_error_new(arena, rt_string_from_c(arena, sqlite3_errmsg(db->handle)));
    }
    return NULL;
}

static RT_UNUSED rt_error rt_sqlite_commit(rt_arena* arena, trux_sqlite_db* db) {
    if (db == NULL || !db->is_open || db->handle == NULL) {
        return rt_error_new(arena, RT_LIT(rt_string, (const uint8_t*)"database connection is not open", 31));
    }
    int rc = sqlite3_exec(db->handle, "COMMIT", NULL, NULL, NULL);
    if (rc != SQLITE_OK) {
        return rt_error_new(arena, rt_string_from_c(arena, sqlite3_errmsg(db->handle)));
    }
    return NULL;
}

static RT_UNUSED rt_error rt_sqlite_rollback(rt_arena* arena, trux_sqlite_db* db) {
    if (db == NULL || !db->is_open || db->handle == NULL) {
        return rt_error_new(arena, RT_LIT(rt_string, (const uint8_t*)"database connection is not open", 31));
    }
    int rc = sqlite3_exec(db->handle, "ROLLBACK", NULL, NULL, NULL);
    if (rc != SQLITE_OK) {
        return rt_error_new(arena, rt_string_from_c(arena, sqlite3_errmsg(db->handle)));
    }
    return NULL;
}

static RT_UNUSED trux_tuple_ptr_struct_16_trux_sqlite_stmt_error rt_sqlite_prepare(rt_arena* arena, trux_sqlite_db* db, rt_string sql) {
    if (db == NULL || !db->is_open || db->handle == NULL) {
        return RT_LIT(trux_tuple_ptr_struct_16_trux_sqlite_stmt_error, NULL, rt_error_new(arena, RT_LIT(rt_string, (const uint8_t*)"database connection is not open", 31)));
    }
    for (size_t i = 0; i < db->stmt_cache_count; i++) {
        trux_sqlite_stmt* s = db->stmt_cache[i];
        if (s != NULL && !s->in_use && s->handle != NULL && s->sql_len == sql.len &&
            (sql.len == 0 || memcmp(s->sql_key, sql.data, sql.len) == 0)) {
            s->is_finalized = 0;
            s->in_use = 1;
            return RT_LIT(trux_tuple_ptr_struct_16_trux_sqlite_stmt_error, s, NULL);
        }
    }

    char* c_sql = rt_string_to_c_string(sql, "sqlite prepare sql");
    sqlite3_stmt* handle = NULL;
    unsigned int prep_flags = (db->stmt_cache_count < RT_SQLITE_STMT_CACHE_SIZE) ? SQLITE_PREPARE_PERSISTENT : 0;
    int rc = sqlite3_prepare_v3(db->handle, c_sql, (int)sql.len, prep_flags, &handle, NULL);
    if (rc != SQLITE_OK) {
        free(c_sql);
        return RT_LIT(trux_tuple_ptr_struct_16_trux_sqlite_stmt_error, NULL, rt_error_new(arena, rt_string_from_c(arena, sqlite3_errmsg(db->handle))));
    }
    trux_sqlite_stmt* stmt = (trux_sqlite_stmt*)calloc(1, sizeof(trux_sqlite_stmt));
    if (stmt == NULL) {
        free(c_sql);
        sqlite3_finalize(handle);
        return RT_LIT(trux_tuple_ptr_struct_16_trux_sqlite_stmt_error, NULL, rt_error_new(arena, RT_LIT(rt_string, (const uint8_t*)"failed to allocate sqlite stmt handle", 37)));
    }
    stmt->handle = handle;
    stmt->db = db;
    stmt->is_finalized = 0;
    stmt->in_use = 1;

    if (db->stmt_cache_count < RT_SQLITE_STMT_CACHE_SIZE) {
        stmt->sql_key = c_sql;
        stmt->sql_len = sql.len;
        stmt->is_cached = 1;
        db->stmt_cache[db->stmt_cache_count++] = stmt;
    } else {
        free(c_sql);
        stmt->is_cached = 0;
    }
    return RT_LIT(trux_tuple_ptr_struct_16_trux_sqlite_stmt_error, stmt, NULL);
}

static RT_UNUSED trux_tuple_bool_error rt_sqlite_step(rt_arena* arena, trux_sqlite_stmt* stmt) {
    if (stmt == NULL || stmt->is_finalized || stmt->handle == NULL) {
        return RT_LIT(trux_tuple_bool_error, false, rt_error_new(arena, RT_LIT(rt_string, (const uint8_t*)"statement is not valid or finalized", 35)));
    }
    int rc = sqlite3_step(stmt->handle);
    if (rc == SQLITE_ROW) {
        return RT_LIT(trux_tuple_bool_error, true, NULL);
    }
    if (rc == SQLITE_DONE) {
        return RT_LIT(trux_tuple_bool_error, false, NULL);
    }
    const char* errmsg = (stmt->db && stmt->db->handle) ? sqlite3_errmsg(stmt->db->handle) : sqlite3_errstr(rc);
    return RT_LIT(trux_tuple_bool_error, false, rt_error_new(arena, rt_string_from_c(arena, errmsg)));
}

static RT_UNUSED rt_error rt_sqlite_reset(rt_arena* arena, trux_sqlite_stmt* stmt) {
    if (stmt == NULL || stmt->is_finalized || stmt->handle == NULL) {
        return rt_error_new(arena, RT_LIT(rt_string, (const uint8_t*)"statement is not valid or finalized", 35));
    }
    int rc = sqlite3_reset(stmt->handle);
    if (rc != SQLITE_OK) {
        const char* errmsg = (stmt->db && stmt->db->handle) ? sqlite3_errmsg(stmt->db->handle) : sqlite3_errstr(rc);
        return rt_error_new(arena, rt_string_from_c(arena, errmsg));
    }
    return NULL;
}

static RT_UNUSED rt_error rt_sqlite_clear_bindings(rt_arena* arena, trux_sqlite_stmt* stmt) {
    if (stmt == NULL || stmt->is_finalized || stmt->handle == NULL) {
        return rt_error_new(arena, RT_LIT(rt_string, (const uint8_t*)"statement is not valid or finalized", 35));
    }
    int rc = sqlite3_clear_bindings(stmt->handle);
    if (rc != SQLITE_OK) {
        const char* errmsg = (stmt->db && stmt->db->handle) ? sqlite3_errmsg(stmt->db->handle) : sqlite3_errstr(rc);
        return rt_error_new(arena, rt_string_from_c(arena, errmsg));
    }
    return NULL;
}

static RT_UNUSED rt_error rt_sqlite_finalize(rt_arena* arena, trux_sqlite_stmt* stmt) {
    if (stmt == NULL || stmt->is_finalized || stmt->handle == NULL) {
        return rt_error_new(arena, RT_LIT(rt_string, (const uint8_t*)"statement is already finalized", 30));
    }
    if (stmt->is_cached && stmt->db != NULL && stmt->db->is_open) {
        int rc = sqlite3_reset(stmt->handle);
        (void)sqlite3_clear_bindings(stmt->handle);
        stmt->is_finalized = 1;
        stmt->in_use = 0;
        if (rc != SQLITE_OK) {
            return rt_error_new(arena, rt_string_from_c(arena, sqlite3_errstr(rc)));
        }
        return NULL;
    }
    int rc = sqlite3_finalize(stmt->handle);
    stmt->handle = NULL;
    stmt->is_finalized = 1;
    stmt->in_use = 0;
    free(stmt->sql_key);
    free(stmt);

    if (rc != SQLITE_OK) {
        return rt_error_new(arena, rt_string_from_c(arena, sqlite3_errstr(rc)));
    }
    return NULL;
}

static RT_UNUSED rt_error rt_sqlite_bind_int(rt_arena* arena, trux_sqlite_stmt* stmt, int64_t index, int64_t val) {
    if (stmt == NULL || stmt->is_finalized || stmt->handle == NULL) {
        return rt_error_new(arena, RT_LIT(rt_string, (const uint8_t*)"statement is not valid or finalized", 35));
    }
    int rc = sqlite3_bind_int64(stmt->handle, (int)index, (sqlite3_int64)val);
    if (rc != SQLITE_OK) {
        const char* errmsg = (stmt->db && stmt->db->handle) ? sqlite3_errmsg(stmt->db->handle) : sqlite3_errstr(rc);
        return rt_error_new(arena, rt_string_from_c(arena, errmsg));
    }
    return NULL;
}

static RT_UNUSED rt_error rt_sqlite_bind_float(rt_arena* arena, trux_sqlite_stmt* stmt, int64_t index, double val) {
    if (stmt == NULL || stmt->is_finalized || stmt->handle == NULL) {
        return rt_error_new(arena, RT_LIT(rt_string, (const uint8_t*)"statement is not valid or finalized", 35));
    }
    int rc = sqlite3_bind_double(stmt->handle, (int)index, val);
    if (rc != SQLITE_OK) {
        const char* errmsg = (stmt->db && stmt->db->handle) ? sqlite3_errmsg(stmt->db->handle) : sqlite3_errstr(rc);
        return rt_error_new(arena, rt_string_from_c(arena, errmsg));
    }
    return NULL;
}

static RT_UNUSED rt_error rt_sqlite_bind_text(rt_arena* arena, trux_sqlite_stmt* stmt, int64_t index, rt_string val) {
    if (stmt == NULL || stmt->is_finalized || stmt->handle == NULL) {
        return rt_error_new(arena, RT_LIT(rt_string, (const uint8_t*)"statement is not valid or finalized", 35));
    }
    int rc = sqlite3_bind_text64(stmt->handle, (int)index, (const char*)val.data, val.len, SQLITE_TRANSIENT, SQLITE_UTF8);
    if (rc != SQLITE_OK) {
        const char* errmsg = (stmt->db && stmt->db->handle) ? sqlite3_errmsg(stmt->db->handle) : sqlite3_errstr(rc);
        return rt_error_new(arena, rt_string_from_c(arena, errmsg));
    }
    return NULL;
}

static RT_UNUSED rt_error rt_sqlite_bind_null(rt_arena* arena, trux_sqlite_stmt* stmt, int64_t index) {
    if (stmt == NULL || stmt->is_finalized || stmt->handle == NULL) {
        return rt_error_new(arena, RT_LIT(rt_string, (const uint8_t*)"statement is not valid or finalized", 35));
    }
    int rc = sqlite3_bind_null(stmt->handle, (int)index);
    if (rc != SQLITE_OK) {
        const char* errmsg = (stmt->db && stmt->db->handle) ? sqlite3_errmsg(stmt->db->handle) : sqlite3_errstr(rc);
        return rt_error_new(arena, rt_string_from_c(arena, errmsg));
    }
    return NULL;
}

static RT_UNUSED int64_t rt_sqlite_bind_index(rt_arena* arena, trux_sqlite_stmt* stmt, rt_string name) {
    (void)arena;
    if (stmt == NULL || stmt->is_finalized || stmt->handle == NULL) {
        return 0;
    }
    char* c_name = rt_string_to_c_string(name, "sqlite param name");
    int idx = sqlite3_bind_parameter_index(stmt->handle, c_name);
    free(c_name);
    return (int64_t)idx;
}

static RT_UNUSED int64_t rt_sqlite_column_count(rt_arena* arena, trux_sqlite_stmt* stmt) {
    (void)arena;
    if (stmt == NULL || stmt->is_finalized || stmt->handle == NULL) {
        return 0;
    }
    return (int64_t)sqlite3_column_count(stmt->handle);
}

static RT_UNUSED rt_string rt_sqlite_column_name(rt_arena* arena, trux_sqlite_stmt* stmt, int64_t col) {
    if (stmt == NULL || stmt->is_finalized || stmt->handle == NULL) {
        return RT_LIT(rt_string, NULL, 0);
    }
    const char* name = sqlite3_column_name(stmt->handle, (int)col);
    return rt_string_from_c(arena, name ? name : "");
}

static RT_UNUSED int64_t rt_sqlite_column_index(rt_arena* arena, trux_sqlite_stmt* stmt, rt_string name) {
    (void)arena;
    if (stmt == NULL || stmt->is_finalized || stmt->handle == NULL) {
        return -1;
    }
    char* c_name = rt_string_to_c_string(name, "sqlite column name");
    int count = sqlite3_column_count(stmt->handle);
    int64_t found = -1;
    for (int i = 0; i < count; i++) {
        const char* cn = sqlite3_column_name(stmt->handle, i);
        if (cn && strcmp(cn, c_name) == 0) {
            found = (int64_t)i;
            break;
        }
    }
    free(c_name);
    return found;
}

static RT_UNUSED rt_string rt_sqlite_column_type(rt_arena* arena, trux_sqlite_stmt* stmt, int64_t col) {
    (void)arena;
    if (stmt == NULL || stmt->is_finalized || stmt->handle == NULL) {
        return RT_LIT(rt_string, (const uint8_t*)"UNKNOWN", 7);
    }
    int t = sqlite3_column_type(stmt->handle, (int)col);
    switch (t) {
    case SQLITE_INTEGER:
        return RT_LIT(rt_string, (const uint8_t*)"INTEGER", 7);
    case SQLITE_FLOAT:
        return RT_LIT(rt_string, (const uint8_t*)"FLOAT", 5);
    case SQLITE_TEXT:
        return RT_LIT(rt_string, (const uint8_t*)"TEXT", 4);
    case SQLITE_BLOB:
        return RT_LIT(rt_string, (const uint8_t*)"BLOB", 4);
    case SQLITE_NULL:
        return RT_LIT(rt_string, (const uint8_t*)"NULL", 4);
    default:
        return RT_LIT(rt_string, (const uint8_t*)"UNKNOWN", 7);
    }
}

static RT_UNUSED int64_t rt_sqlite_column_int(rt_arena* arena, trux_sqlite_stmt* stmt, int64_t col) {
    (void)arena;
    if (stmt == NULL || stmt->is_finalized || stmt->handle == NULL) {
        return 0;
    }
    return (int64_t)sqlite3_column_int64(stmt->handle, (int)col);
}

static RT_UNUSED double rt_sqlite_column_float(rt_arena* arena, trux_sqlite_stmt* stmt, int64_t col) {
    (void)arena;
    if (stmt == NULL || stmt->is_finalized || stmt->handle == NULL) {
        return 0.0;
    }
    return (double)sqlite3_column_double(stmt->handle, (int)col);
}

static RT_UNUSED rt_string rt_sqlite_column_text(rt_arena* arena, trux_sqlite_stmt* stmt, int64_t col) {
    if (stmt == NULL || stmt->is_finalized || stmt->handle == NULL) {
        return RT_LIT(rt_string, NULL, 0);
    }
    const uint8_t* text = (const uint8_t*)sqlite3_column_text(stmt->handle, (int)col);
    if (text == NULL) {
        return RT_LIT(rt_string, NULL, 0);
    }
    int bytes = sqlite3_column_bytes(stmt->handle, (int)col);
    if (bytes <= 0) {
        return RT_LIT(rt_string, NULL, 0);
    }
    size_t len = (size_t)bytes;
    uint8_t* data = (uint8_t*)rt_arena_alloc(arena, len);
    memcpy(data, text, len);
    return RT_LIT(rt_string, data, len);
}

static RT_UNUSED bool rt_sqlite_column_is_null(rt_arena* arena, trux_sqlite_stmt* stmt, int64_t col) {
    (void)arena;
    if (stmt == NULL || stmt->is_finalized || stmt->handle == NULL) {
        return true;
    }
    return sqlite3_column_type(stmt->handle, (int)col) == SQLITE_NULL;
}
`

#include <assert.h>
#include <curl/curl.h>
#include <ncurses.h>
#include <openssl/evp.h>
#include <sqlite3.h>
#include <stdio.h>
#include <string.h>
#include <uv.h>
#include <zlib.h>

static void check_zlib(void)
{
    const unsigned char input[] = "native-smoke";
    unsigned char compressed[128], restored[sizeof(input)];
    uLongf compressed_size = sizeof(compressed), restored_size = sizeof(restored);
    assert(compress(compressed, &compressed_size, input, sizeof(input)) == Z_OK);
    assert(uncompress(restored, &restored_size, compressed, compressed_size) == Z_OK);
    assert(restored_size == sizeof(input) && memcmp(input, restored, sizeof(input)) == 0);
}

static void check_crypto(void)
{
    unsigned char digest[EVP_MAX_MD_SIZE];
    unsigned int length;
    assert(EVP_Digest("abc", 3, digest, &length, EVP_sha256(), NULL) == 1);
    char hex[65];
    assert(length == 32);
    for (unsigned int index = 0; index < length; ++index)
        snprintf(hex + index * 2, 3, "%02x", (unsigned)digest[index]);
    assert(strcmp(hex, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad") == 0);
}

static void check_sqlite(void)
{
    sqlite3 *database;
    sqlite3_stmt *query;
    assert(sqlite3_open(":memory:", &database) == SQLITE_OK);
    assert(sqlite3_exec(database, "CREATE TABLE sample(value INT); INSERT INTO sample VALUES (42);",
                        NULL, NULL, NULL) == SQLITE_OK);
    assert(sqlite3_prepare_v2(database, "SELECT value FROM sample", -1, &query, NULL) == SQLITE_OK);
    assert(sqlite3_step(query) == SQLITE_ROW && sqlite3_column_int(query, 0) == 42);
    assert(sqlite3_finalize(query) == SQLITE_OK);
    assert(sqlite3_close(database) == SQLITE_OK);
}

static void on_timer(uv_timer_t *timer)
{
    int *fired = timer->data;
    ++*fired;
    uv_close((uv_handle_t *)timer, NULL);
}

static void check_uv(void)
{
    uv_loop_t loop;
    uv_timer_t timer;
    int fired = 0;
    assert(uv_loop_init(&loop) == 0);
    assert(uv_timer_init(&loop, &timer) == 0);
    timer.data = &fired;
    assert(uv_timer_start(&timer, on_timer, 1, 0) == 0);
    assert(uv_run(&loop, UV_RUN_DEFAULT) == 0 && fired == 1);
    assert(uv_loop_close(&loop) == 0);
}

static void check_curses(void)
{
    FILE *terminal = tmpfile();
    assert(terminal);
    SCREEN *screen = newterm("xterm", terminal, terminal);
    assert(screen);
    assert(mvaddch(0, 0, 'X') == OK);
    assert((mvinch(0, 0) & A_CHARTEXT) == 'X');
    endwin();
    delscreen(screen);
    fclose(terminal);
}

static size_t count_body(char *data, size_t size, size_t count, void *context)
{
    (void)data;
    size_t *bytes = context;
    *bytes += size * count;
    return size * count;
}

static void check_https(void)
{
    assert(curl_global_init(CURL_GLOBAL_DEFAULT) == CURLE_OK);
    CURL *curl = curl_easy_init();
    assert(curl);
    size_t bytes = 0;
    long status = 0;
    assert(curl_easy_setopt(curl, CURLOPT_URL, "https://example.com/") == CURLE_OK);
    assert(curl_easy_setopt(curl, CURLOPT_TIMEOUT, 20L) == CURLE_OK);
    assert(curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, count_body) == CURLE_OK);
    assert(curl_easy_setopt(curl, CURLOPT_WRITEDATA, &bytes) == CURLE_OK);
    CURLcode result = curl_easy_perform(curl);
    if (result != CURLE_OK)
        fprintf(stderr, "%s\n", curl_easy_strerror(result));
    assert(result == CURLE_OK);
    assert(curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &status) == CURLE_OK);
    assert(status == 200 && bytes > 0);
    curl_easy_cleanup(curl);
    curl_global_cleanup();
}

int main(int argc, char **argv)
{
    assert(argc == 2);
    if (strcmp(argv[1], "zlib") == 0)
        check_zlib();
    else if (strcmp(argv[1], "crypto") == 0)
        check_crypto();
    else if (strcmp(argv[1], "sqlite") == 0)
        check_sqlite();
    else if (strcmp(argv[1], "uv") == 0)
        check_uv();
    else if (strcmp(argv[1], "curses") == 0)
        check_curses();
    else if (strcmp(argv[1], "https") == 0)
        check_https();
    else
        return 1;
}

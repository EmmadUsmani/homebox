#define _POSIX_C_SOURCE 200809L
#include <assert.h>
#include <dlfcn.h>
#include <iconv.h>
#include <locale.h>
#include <netdb.h>
#include <pthread.h>
#include <string.h>

static void *worker(void *value)
{
    return value;
}

int main(void)
{
    assert(setlocale(LC_ALL, "en_US.UTF-8"));

    char source[] = "é";
    char target[4] = {0};
    char *input = source, *output = target;
    size_t input_left = strlen(source), output_left = sizeof(target);
    iconv_t converter = iconv_open("ISO-8859-1", "UTF-8");
    assert(converter != (iconv_t)-1);
    assert(iconv(converter, &input, &input_left, &output, &output_left) == 0);
    assert(input_left == 0 && (unsigned char)target[0] == 0xe9);
    assert(iconv_close(converter) == 0);

    void *plugin = dlopen("./plugin.so", RTLD_NOW);
    assert(plugin);
    int (*answer)(void) = (int (*)(void))dlsym(plugin, "answer");
    assert(answer && answer() == 42);
    assert(dlclose(plugin) == 0);

    int sentinel = 42;
    void *result = 0;
    pthread_t thread;
    assert(pthread_create(&thread, 0, worker, &sentinel) == 0);
    assert(pthread_join(thread, &result) == 0 && result == &sentinel);

    const struct addrinfo hints = {.ai_family = AF_INET, .ai_socktype = SOCK_STREAM};
    struct addrinfo *address;
    assert(getaddrinfo("localhost", "80", &hints, &address) == 0);
    assert(address->ai_family == AF_INET);
    freeaddrinfo(address);
}

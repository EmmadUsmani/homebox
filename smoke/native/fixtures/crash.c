#include <stdlib.h>

static void crash_fixture(void)
{
    abort();
}

int main(void)
{
    crash_fixture();
}

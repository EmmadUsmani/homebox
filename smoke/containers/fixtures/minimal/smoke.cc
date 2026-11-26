#include <iostream>
#include <unistd.h>

int main() {
    if (geteuid() == 0) {
        return 1;
    }
    std::cout << "cpp:" << 42 << '\n';
}

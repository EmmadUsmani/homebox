#include <array>
#include <filesystem>
#include <format>
#include <fstream>
#include <ranges>
#include <thread>

int main()
{
    const auto file = std::filesystem::path("result.txt");
    const std::array values{1, 2, 3, 4};
    std::jthread worker([&] {
        int total = 0;
        for (const int value : values | std::views::filter([](int n) { return n % 2 == 0; }))
            total += value;
        std::ofstream(file) << std::format("sum={}", total);
    });
    worker.join();

    std::string result;
    std::ifstream(file) >> result;
    return result == "sum=6" ? 0 : 1;
}

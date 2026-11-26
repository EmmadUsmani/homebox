#include <SDL.h>

int main(void) {
    if (SDL_Init(SDL_INIT_VIDEO) != 0)
        return 1;
    SDL_Window *window = SDL_CreateWindow("SmokeSDL", SDL_WINDOWPOS_CENTERED,
        SDL_WINDOWPOS_CENTERED, 320, 160, SDL_WINDOW_SHOWN);
    if (!window)
        return 1;
    SDL_Surface *surface = SDL_GetWindowSurface(window);
    if (!surface || SDL_FillRect(surface, NULL, SDL_MapRGB(surface->format, 0, 204, 68)) ||
        SDL_UpdateWindowSurface(window))
        return 1;
    SDL_Event event;
    while (SDL_WaitEvent(&event) && event.type != SDL_QUIT)
        ;
    SDL_DestroyWindow(window);
    SDL_Quit();
    return 0;
}

#include <cairo.h>

int main(int argc, char **argv) {
    if (argc != 2)
        return 1;
    cairo_surface_t *surface = cairo_image_surface_create(CAIRO_FORMAT_ARGB32, 64, 64);
    cairo_t *context = cairo_create(surface);
    cairo_set_source_rgb(context, 0, 0, 1);
    cairo_paint(context);
    cairo_set_source_rgb(context, 1, 0, 0);
    cairo_rectangle(context, 16, 16, 32, 32);
    cairo_fill(context);
    cairo_status_t status = cairo_surface_write_to_png(surface, argv[1]);
    cairo_destroy(context);
    cairo_surface_destroy(surface);
    return status == CAIRO_STATUS_SUCCESS ? 0 : 1;
}

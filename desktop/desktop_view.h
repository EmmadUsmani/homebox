#pragma once

void *desktopViewOpen(void *window, const char *url);
void desktopViewLayout(void *view, double x, double y, double width, double height);
void desktopViewClose(void *view);

#include <stddef.h>
char *desktopClipboardRead(size_t limit, size_t *length);
int desktopClipboardWrite(const char *text, size_t length);

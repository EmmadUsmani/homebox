#pragma once

void *desktopViewOpen(void *window, const char *url);
void desktopViewLayout(void *view, double x, double y, double width, double height);
void desktopViewClose(void *view);

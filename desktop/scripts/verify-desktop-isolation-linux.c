// Include the native viewer so the fixture can inspect its private session.
#include "../desktop_view_linux.c"
#include <dlfcn.h>
#include <gdk/x11/gdkx.h>

static GtkWindow *window;
static DesktopView *viewer;
static GMainLoop *loop;
static const char *address;
static gboolean pass;
static int stage;
static gboolean reading_clipboard;

static void clipboard_read(GObject *source, GAsyncResult *result, gpointer data) {
    char *text = gdk_clipboard_read_text_finish(GDK_CLIPBOARD(source), result, NULL);
    pass = g_strcmp0(text, "host fixture clipboard") == 0;
    if (!pass) g_printerr("Viewer changed the host clipboard\n");
    g_free(text);
    g_main_loop_quit(loop);
}

static gboolean click_probe(gpointer data) {
    void *xtst = dlopen("libXtst.so.6", RTLD_NOW);
    if (!xtst) return G_SOURCE_REMOVE;
    Bool (*motion)(Display *, int, int, int, unsigned long) = dlsym(xtst, "XTestFakeMotionEvent");
    Bool (*button)(Display *, unsigned int, Bool, unsigned long) = dlsym(xtst, "XTestFakeButtonEvent");
    if (motion && button) {
        GdkSurface *surface = gtk_native_get_surface(GTK_NATIVE(window));
        Display *display = gdk_x11_display_get_xdisplay(gdk_surface_get_display(surface));
        Window xid = gdk_x11_surface_get_xid(surface);
        Window child;
        int x, y;
        int scale = gdk_surface_get_scale_factor(surface);
        XTranslateCoordinates(display, xid, DefaultRootWindow(display), 30 * scale, 30 * scale, &x, &y, &child);
        motion(display, -1, x, y, CurrentTime);
        button(display, 1, True, CurrentTime);
        button(display, 1, False, CurrentTime);
        XFlush(display);
    }
    dlclose(xtst);
    return G_SOURCE_REMOVE;
}

static gboolean reopen(gpointer data);

static void title_changed(WebKitWebView *view, GParamSpec *property, gpointer data) {
    const char *title = webkit_web_view_get_title(view);
    if (!title) return;
    if (g_str_has_prefix(title, "FAIL:")) {
        g_printerr("%s\n", title);
        g_main_loop_quit(loop);
    } else if (g_str_equal(title, "READY") && stage == 0) {
        stage = 1;
        g_timeout_add(500, click_probe, NULL);
    } else if (g_str_equal(title, "PASS") && stage == 1) {
        // Use a new viewer for the same origin. Cookies and local storage
        // from the first view must be absent.
        stage = 2;
        g_idle_add(reopen, NULL);
    } else if (g_str_equal(title, "READY") && stage == 2 && !reading_clipboard) {
        reading_clipboard = TRUE;
        GdkClipboard *clipboard = gdk_display_get_clipboard(gdk_display_get_default());
        gdk_clipboard_read_text_async(clipboard, NULL, clipboard_read, NULL);
    }
}

static gboolean reopen(gpointer data) {
    if (viewer) desktopViewClose(viewer);
    viewer = desktopViewOpen(window, address);
    if (!viewer) {
        g_main_loop_quit(loop);
        return G_SOURCE_REMOVE;
    }
    WebKitNetworkSession *session = webkit_web_view_get_network_session(viewer->view);
    WebKitWebsiteDataManager *manager = webkit_network_session_get_website_data_manager(session);
    if (!webkit_website_data_manager_is_ephemeral(manager)) {
        g_printerr("Viewer data manager is persistent\n");
        g_main_loop_quit(loop);
        return G_SOURCE_REMOVE;
    }
    g_signal_connect(viewer->view, "notify::title", G_CALLBACK(title_changed), NULL);
    desktopViewLayout(viewer, 0, 0, 480, 300);
    return G_SOURCE_REMOVE;
}

static gboolean timeout(gpointer data) {
    g_printerr("Timed out in viewer fixture (stage %d)\n", stage);
    g_main_loop_quit(loop);
    return G_SOURCE_REMOVE;
}

int main(int argc, char **argv) {
    if (argc != 2) return 2;
    address = argv[1];
    gtk_init();
    if (!GDK_IS_X11_DISPLAY(gdk_display_get_default())) return 2;
    GdkClipboard *clipboard = gdk_display_get_clipboard(gdk_display_get_default());
    window = GTK_WINDOW(gtk_window_new());
    gtk_window_set_title(window, "Homebox viewer isolation");
    gtk_window_set_decorated(window, FALSE);
    gtk_window_set_default_size(window, 480, 300);
    GtkWidget *box = gtk_box_new(GTK_ORIENTATION_VERTICAL, 0);
    GtkWidget *host_view = webkit_web_view_new();
    gtk_widget_set_vexpand(host_view, TRUE);
    gtk_box_append(GTK_BOX(box), host_view);
    gtk_window_set_child(window, box);
    gtk_window_present(window);
    // The caller runs the fixture under an isolated X server or explicitly
    // opts into replacing the current clipboard for this check.
    gdk_clipboard_set_text(clipboard, "host fixture clipboard");
    loop = g_main_loop_new(NULL, FALSE);
    g_idle_add(reopen, NULL);
    g_timeout_add_seconds(18, timeout, NULL);
    g_main_loop_run(loop);
    if (viewer) desktopViewClose(viewer);
    gtk_window_destroy(window);
    g_main_loop_unref(loop);
    if (pass) g_print("Linux desktop viewer isolation passed\n");
    return pass ? 0 : 1;
}

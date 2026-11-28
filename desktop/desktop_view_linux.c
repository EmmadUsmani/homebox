#include <gtk/gtk.h>
#include <math.h>
#include <webkit/webkit.h>
#include "desktop_view.h"

typedef struct {
    GtkOverlay *overlay;
    WebKitWebView *view;
    int port;
    GdkRectangle bounds;
    gulong position_handler;
} DesktopView;

static gboolean permits_uri(const char *address, int port) {
    if (!address) return FALSE;
    GUri *uri = g_uri_parse(address, G_URI_FLAGS_NONE, NULL);
    if (!uri) return FALSE;
    gboolean allowed = g_strcmp0(g_uri_get_scheme(uri), "http") == 0 &&
        g_strcmp0(g_uri_get_host(uri), "127.0.0.1") == 0 &&
        g_uri_get_port(uri) == port && port > 0 && !g_uri_get_userinfo(uri);
    g_uri_unref(uri);
    return allowed;
}

static gboolean decide_policy(WebKitWebView *view, WebKitPolicyDecision *decision,
                              WebKitPolicyDecisionType type, DesktopView *desktop) {
    gboolean allowed = FALSE;
    if (type == WEBKIT_POLICY_DECISION_TYPE_NAVIGATION_ACTION) {
        WebKitNavigationAction *action = webkit_navigation_policy_decision_get_navigation_action(
            WEBKIT_NAVIGATION_POLICY_DECISION(decision));
        allowed = permits_uri(webkit_uri_request_get_uri(webkit_navigation_action_get_request(action)),
                              desktop->port);
    } else if (type == WEBKIT_POLICY_DECISION_TYPE_RESPONSE) {
        WebKitResponsePolicyDecision *response = WEBKIT_RESPONSE_POLICY_DECISION(decision);
        allowed = webkit_response_policy_decision_is_main_frame_main_resource(response) &&
            webkit_response_policy_decision_is_mime_type_supported(response) &&
            permits_uri(webkit_uri_response_get_uri(webkit_response_policy_decision_get_response(response)),
                        desktop->port);
        // Also reject supported MIME types served with Content-Disposition: attachment.
        SoupMessageHeaders *headers = webkit_uri_response_get_http_headers(
            webkit_response_policy_decision_get_response(response));
        char *disposition = NULL;
        if (headers && soup_message_headers_get_content_disposition(headers, &disposition, NULL) &&
            g_ascii_strcasecmp(disposition, "attachment") == 0) allowed = FALSE;
        g_free(disposition);
    }
    if (allowed) webkit_policy_decision_use(decision);
    else webkit_policy_decision_ignore(decision);
    return TRUE;
}

static GtkWidget *deny_popup(WebKitWebView *view, WebKitNavigationAction *action) {
    return NULL;
}

static void cancel_download(WebKitNetworkSession *session, WebKitDownload *download) {
    webkit_download_cancel(download);
}

static gboolean deny_permission(WebKitWebView *view, WebKitPermissionRequest *request) {
    webkit_permission_request_deny(request);
    return TRUE;
}

static gboolean deny_file_chooser(WebKitWebView *view, WebKitFileChooserRequest *request) {
    webkit_file_chooser_request_cancel(request);
    return TRUE;
}

static gboolean deny_context_menu(WebKitWebView *view, WebKitContextMenu *menu, WebKitHitTestResult *hit) {
    return TRUE;
}

static gboolean deny_dialog(WebKitWebView *view, WebKitScriptDialog *dialog) {
    return TRUE;
}

// Use the view's own overlay allocation. Wails keeps its menu and the web view
// in a vertical box, so browser coordinates start below the native menu.
static GtkOverlay *viewer_overlay(GtkWindow *window) {
    GtkWidget *box = gtk_window_get_child(window);
    if (!GTK_IS_BOX(box)) return NULL;
    GtkWidget *content = gtk_widget_get_last_child(box);
    if (GTK_IS_OVERLAY(content)) return GTK_OVERLAY(content);
    if (!WEBKIT_IS_WEB_VIEW(content)) return NULL;

    GtkWidget *overlay = gtk_overlay_new();
    g_object_ref(content);
    gtk_box_remove(GTK_BOX(box), content);
    gtk_overlay_set_child(GTK_OVERLAY(overlay), content);
    g_object_unref(content);
    gtk_widget_set_hexpand(overlay, TRUE);
    gtk_widget_set_vexpand(overlay, TRUE);
    gtk_box_append(GTK_BOX(box), overlay);
    return GTK_OVERLAY(overlay);
}

static gboolean position_view(GtkOverlay *overlay, GtkWidget *child, GdkRectangle *allocation,
                              DesktopView *desktop) {
    if (child != GTK_WIDGET(desktop->view)) return FALSE;
    GdkRectangle viewport = {0, 0, gtk_widget_get_width(GTK_WIDGET(overlay)),
                                 gtk_widget_get_height(GTK_WIDGET(overlay))};
    if (!gdk_rectangle_intersect(&viewport, &desktop->bounds, allocation))
        *allocation = (GdkRectangle){0};
    return TRUE;
}

void *desktopViewOpen(void *window, const char *address) {
    if (!GTK_IS_WINDOW(window)) return NULL;
    GUri *uri = g_uri_parse(address, G_URI_FLAGS_NONE, NULL);
    if (!uri) return NULL;
    int port = g_uri_get_port(uri);
    g_uri_unref(uri);
    if (!permits_uri(address, port)) return NULL;
    GtkOverlay *overlay = viewer_overlay(GTK_WINDOW(window));
    if (!overlay) return NULL;

    WebKitNetworkSession *session = webkit_network_session_new_ephemeral();
    // The viewer's one-time URL should always go directly to loopback.
    webkit_network_session_set_proxy_settings(session, WEBKIT_NETWORK_PROXY_MODE_NO_PROXY, NULL);
    g_signal_connect(session, "download-started", G_CALLBACK(cancel_download), NULL);
    WebKitWebContext *context = webkit_web_context_new();
    WebKitUserContentManager *content = webkit_user_content_manager_new();
    const char *guard =
        "(() => {"
        "  const define = Object.defineProperty;"
        "  define(Document.prototype, 'execCommand', {value: () => false, writable: false, configurable: false});"
        "  if (globalThis.Clipboard) {"
        "    for (const method of ['write', 'writeText', 'read', 'readText']) {"
        "      define(Clipboard.prototype, method, {value: () => Promise.reject("
        "        new DOMException('Clipboard access is disabled', 'NotAllowedError')), writable: false, configurable: false});"
        "    }"
        "  }"
        "  define(Navigator.prototype, 'share', {value: () => Promise.reject("
        "    new DOMException('Sharing is disabled', 'NotAllowedError')), writable: false, configurable: false});"
        "  for (const event of ['copy', 'cut', 'paste', 'dragstart', 'dragenter', 'dragover', 'dragleave', 'drop']) {"
        "    addEventListener(event, e => { e.preventDefault(); e.stopImmediatePropagation(); }, true);"
        "  }"
        "  for (const event of ['mousedown', 'mouseup', 'auxclick']) {"
        "    addEventListener(event, e => { if (e.button === 1) e.preventDefault(); }, true);"
        "  }"
        "  addEventListener('keydown', e => {"
        "    if ((e.ctrlKey && ['c', 'x', 'v'].includes(e.key.toLowerCase())) ||"
        "        (e.key === 'Insert' && (e.ctrlKey || e.shiftKey))) e.preventDefault();"
        "  }, true);"
        "})();";
    WebKitUserScript *script = webkit_user_script_new(guard, WEBKIT_USER_CONTENT_INJECT_ALL_FRAMES,
                                                      WEBKIT_USER_SCRIPT_INJECT_AT_DOCUMENT_START,
                                                      NULL, NULL);
    webkit_user_content_manager_add_script(content, script);
    webkit_user_script_unref(script);
    // The guest renders to a canvas. Suppress ordinary text selection, which
    // WebKitGTK automatically publishes to the desktop PRIMARY selection.
    WebKitUserStyleSheet *style = webkit_user_style_sheet_new(
        "* { -webkit-user-select: none !important; }",
        WEBKIT_USER_CONTENT_INJECT_ALL_FRAMES, WEBKIT_USER_STYLE_LEVEL_USER, NULL, NULL);
    webkit_user_content_manager_add_style_sheet(content, style);
    webkit_user_style_sheet_unref(style);

    GtkWidget *widget = g_object_new(WEBKIT_TYPE_WEB_VIEW, "web-context", context,
        "network-session", session, "user-content-manager", content, NULL);
    g_object_unref(content);
    g_object_unref(context);
    g_object_unref(session);

    DesktopView *desktop = g_new0(DesktopView, 1);
    desktop->overlay = g_object_ref(overlay);
    desktop->view = g_object_ref_sink(WEBKIT_WEB_VIEW(widget));
    desktop->port = port;
    WebKitSettings *settings = webkit_web_view_get_settings(desktop->view);
    webkit_settings_set_javascript_can_access_clipboard(settings, FALSE);
    webkit_settings_set_javascript_can_open_windows_automatically(settings, FALSE);
    webkit_settings_set_enable_media_stream(settings, FALSE);
    webkit_settings_set_enable_developer_extras(settings, FALSE);
    g_signal_connect(widget, "decide-policy", G_CALLBACK(decide_policy), desktop);
    g_signal_connect(widget, "create", G_CALLBACK(deny_popup), NULL);
    g_signal_connect(widget, "permission-request", G_CALLBACK(deny_permission), NULL);
    g_signal_connect(widget, "run-file-chooser", G_CALLBACK(deny_file_chooser), NULL);
    g_signal_connect(widget, "context-menu", G_CALLBACK(deny_context_menu), NULL);
    g_signal_connect(widget, "script-dialog", G_CALLBACK(deny_dialog), NULL);

    gtk_widget_set_visible(widget, FALSE);
    gtk_overlay_add_overlay(overlay, widget);
    gtk_overlay_set_clip_overlay(overlay, widget, TRUE);
    desktop->position_handler = g_signal_connect(overlay, "get-child-position",
                                                  G_CALLBACK(position_view), desktop);
    webkit_web_view_load_uri(desktop->view, address);
    return desktop;
}

void desktopViewLayout(void *handle, double x, double y, double width, double height) {
    DesktopView *desktop = handle;
    // DOM bounds are CSS pixels. GTK allocations are also in logical pixels.
    gboolean visible = isfinite(x) && isfinite(y) && isfinite(width) && isfinite(height) &&
        fabs(x) < G_MAXINT / 4 && fabs(y) < G_MAXINT / 4 &&
        width >= 1 && width < G_MAXINT / 4 && height >= 1 && height < G_MAXINT / 4;
    if (visible) desktop->bounds = (GdkRectangle){x, y, width, height};
    gtk_widget_set_visible(GTK_WIDGET(desktop->view), visible);
    gtk_widget_queue_allocate(GTK_WIDGET(desktop->overlay));
}

void desktopViewClose(void *handle) {
    DesktopView *desktop = handle;
    webkit_web_view_stop_loading(desktop->view);
    g_signal_handler_disconnect(desktop->overlay, desktop->position_handler);
    GtkWidget *widget = GTK_WIDGET(desktop->view);
    g_signal_handlers_disconnect_by_data(widget, desktop);
    if (gtk_widget_get_parent(widget) == GTK_WIDGET(desktop->overlay))
        gtk_overlay_remove_overlay(desktop->overlay, widget);
    g_object_unref(widget);
    g_object_unref(desktop->overlay);
    g_free(desktop);
}

int desktopClipboardWrite(const char *text, size_t length) {
    GdkDisplay *display = gdk_display_get_default();
    if (!display || !g_utf8_validate(text, length, NULL)) return 0;
    gdk_clipboard_set_text(gdk_display_get_clipboard(display), text);
    return 1;
}

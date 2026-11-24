#import <AppKit/AppKit.h>
#import <WebKit/WebKit.h>
#import "desktop_view.h"

@interface DesktopView : WKWebView <WKNavigationDelegate, WKUIDelegate>
@property (nonatomic, strong) NSURL *origin;
@end

@implementation DesktopView
- (BOOL)permitsURL:(NSURL *)url {
    return [url.scheme isEqualToString:self.origin.scheme] &&
        [url.host isEqualToString:self.origin.host] &&
        [url.port isEqualToNumber:self.origin.port] &&
        url.user == nil && url.password == nil;
}
- (void)webView:(WKWebView *)view decidePolicyForNavigationAction:(WKNavigationAction *)action
    decisionHandler:(void (^)(WKNavigationActionPolicy))decide {
    BOOL allowed = action.targetFrame.isMainFrame && !action.shouldPerformDownload &&
        [self permitsURL:action.request.URL];
    decide(allowed ? WKNavigationActionPolicyAllow : WKNavigationActionPolicyCancel);
}
- (void)webView:(WKWebView *)view decidePolicyForNavigationResponse:(WKNavigationResponse *)response
    decisionHandler:(void (^)(WKNavigationResponsePolicy))decide {
    BOOL allowed = response.forMainFrame && response.canShowMIMEType &&
        [self permitsURL:response.response.URL];
    decide(allowed ? WKNavigationResponsePolicyAllow : WKNavigationResponsePolicyCancel);
}
- (WKWebView *)webView:(WKWebView *)view createWebViewWithConfiguration:(WKWebViewConfiguration *)configuration
    forNavigationAction:(WKNavigationAction *)action windowFeatures:(WKWindowFeatures *)features {
    return nil;
}
- (void)webView:(WKWebView *)view runOpenPanelWithParameters:(WKOpenPanelParameters *)parameters
    initiatedByFrame:(WKFrameInfo *)frame completionHandler:(void (^)(NSArray<NSURL *> *))complete {
    complete(nil);
}
- (void)webView:(WKWebView *)view requestMediaCapturePermissionForOrigin:(WKSecurityOrigin *)origin
    initiatedByFrame:(WKFrameInfo *)frame type:(WKMediaCaptureType)type
    decisionHandler:(void (^)(WKPermissionDecision))decide {
    decide(WKPermissionDecisionDeny);
}
- (NSMenu *)menuForEvent:(NSEvent *)event { return nil; }
- (void)copy:(id)sender {}
- (void)cut:(id)sender {}
- (void)paste:(id)sender {}
- (BOOL)performKeyEquivalent:(NSEvent *)event {
    if (event.modifierFlags & NSEventModifierFlagCommand) {
        NSString *key = event.charactersIgnoringModifiers.lowercaseString;
        if ([@[@"c", @"x", @"v"] containsObject:key]) {
            // Deliver keyboard input to the remote viewer without invoking AppKit's edit menu.
            [self keyDown:event];
            return YES;
        }
    }
    return [super performKeyEquivalent:event];
}
@end

void *desktopViewOpen(void *windowHandle, const char *address) {
    NSWindow *window = (__bridge NSWindow *)windowHandle;
    NSURL *url = [NSURL URLWithString:[NSString stringWithUTF8String:address]];
    if (window.contentView == nil || ![url.scheme isEqualToString:@"http"] ||
        ![url.host isEqualToString:@"127.0.0.1"] || url.port == nil) {
        return NULL;
    }
    WKWebViewConfiguration *configuration = [WKWebViewConfiguration new];
    configuration.websiteDataStore = [WKWebsiteDataStore nonPersistentDataStore];
    configuration.preferences.javaScriptCanOpenWindowsAutomatically = NO;
    // WebKit permits clipboard writes with transient user activation. Install these
    // guards before page scripts and suppress clipboard commands from the embedder.
    NSString *clipboard =
        @"(() => {"
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
         "  for (const event of ['copy', 'cut', 'paste', 'dragstart', 'dragover', 'drop']) {"
         "    addEventListener(event, e => { e.preventDefault(); e.stopImmediatePropagation(); }, true);"
         "  }"
         "})();";
    WKUserScript *guard = [[WKUserScript alloc] initWithSource:clipboard
        injectionTime:WKUserScriptInjectionTimeAtDocumentStart forMainFrameOnly:NO];
    [configuration.userContentController addUserScript:guard];
    DesktopView *view = [[DesktopView alloc] initWithFrame:NSZeroRect configuration:configuration];
    view.origin = url;
    view.navigationDelegate = view;
    view.UIDelegate = view;
    view.allowsLinkPreview = NO;
    [window.contentView addSubview:view positioned:NSWindowAbove relativeTo:nil];
    [view loadRequest:[NSURLRequest requestWithURL:url]];
    return (__bridge_retained void *)view;
}

void desktopViewLayout(void *handle, double x, double y, double width, double height) {
    DesktopView *view = (__bridge DesktopView *)handle;
    NSView *container = view.superview;
    if (container == nil) return;
    NSRect bounds = container.bounds;
    double bottom = container.isFlipped ? y : NSHeight(bounds) - y - height;
    view.frame = NSIntersectionRect(bounds, NSMakeRect(x, bottom, width, height));
}

void desktopViewClose(void *handle) {
    DesktopView *view = (__bridge_transfer DesktopView *)handle;
    [view stopLoading];
    [view removeFromSuperview];
}

char *desktopClipboardRead(size_t limit, size_t *length) {
    NSString *value = [NSPasteboard.generalPasteboard stringForType:NSPasteboardTypeString];
    *length = [value lengthOfBytesUsingEncoding:NSUTF8StringEncoding];
    if (value == nil || *length > limit) return NULL;

    NSData *data = [value dataUsingEncoding:NSUTF8StringEncoding];
    if (data == nil) return NULL;
    char *text = malloc(MAX(data.length, 1));
    if (text != NULL && data.length > 0) memcpy(text, data.bytes, data.length);
    return text;
}

int desktopClipboardWrite(const char *text, size_t length) {
    NSString *value = length == 0 ? @"" :
        [[NSString alloc] initWithBytes:text length:length encoding:NSUTF8StringEncoding];
    if (value == nil) return 0;
    NSPasteboard *clipboard = NSPasteboard.generalPasteboard;
    [clipboard clearContents];
    return [clipboard setString:value forType:NSPasteboardTypeString];
}

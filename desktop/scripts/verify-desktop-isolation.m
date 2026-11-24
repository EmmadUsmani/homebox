#import <AppKit/AppKit.h>
#import <WebKit/WebKit.h>
#import "../desktop_view.h"

static void verify(WKWebView *view, NSInteger clipboard, CFAbsoluteTime deadline) {
    if (CFAbsoluteTimeGetCurrent() >= deadline) {
        fputs("Timed out waiting for the guest test page\n", stderr);
        exit(1);
    }
    [view evaluateJavaScript:@"document.querySelector('#probe')?.getBoundingClientRect().toJSON()" completionHandler:^(id value, NSError *error) {
        if (![value isKindOfClass:NSDictionary.class]) {
            dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 100 * NSEC_PER_MSEC), dispatch_get_main_queue(), ^{
                verify(view, clipboard, deadline);
            });
            return;
        }
        CGRect screen = CGDisplayBounds(CGMainDisplayID());
        NSRect window = view.window.contentLayoutRect;
        NSPoint origin = [view.window convertPointToScreen:NSMakePoint(NSMinX(window), NSMaxY(window))];
        CGPoint point = CGPointMake(origin.x + [value[@"x"] doubleValue] + 12,
            CGRectGetMaxY(screen) - origin.y + [value[@"y"] doubleValue] + 10);
        for (int index = 0; index < 2; index++) {
            CGEventRef click = CGEventCreateMouseEvent(NULL, index == 0 ? kCGEventLeftMouseDown : kCGEventLeftMouseUp, point, kCGMouseButtonLeft);
            CGEventPost(kCGHIDEventTap, click);
            CFRelease(click);
        }
        dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 2 * NSEC_PER_SEC), dispatch_get_main_queue(), ^{
            [view evaluateJavaScript:@"window.result" completionHandler:^(id result, NSError *readError) {
                BOOL pass = [result isEqual:@"PASS"] && [NSPasteboard generalPasteboard].changeCount == clipboard;
                fprintf(pass ? stdout : stderr, "Guest isolation %s (%s), pasteboard %s\n",
                    pass ? "passed" : "failed", [[result description] UTF8String] ?: "no result",
                    [NSPasteboard generalPasteboard].changeCount == clipboard ? "unchanged" : "changed");
                exit(pass ? 0 : 1);
            }];
        });
    }];
}

int main(int argc, const char **argv) {
    if (argc != 2 || !AXIsProcessTrusted()) {
        fputs("Usage: verify-desktop-isolation URL (Accessibility permission required)\n", stderr);
        return 1;
    }
    @autoreleasepool {
        NSApplication *app = NSApplication.sharedApplication;
        [app setActivationPolicy:NSApplicationActivationPolicyRegular];
        NSWindow *window = [[NSWindow alloc] initWithContentRect:NSMakeRect(0, 0, 600, 420)
            styleMask:NSWindowStyleMaskTitled backing:NSBackingStoreBuffered defer:NO];
        [window center];
        [window makeKeyAndOrderFront:nil];
        [app activate];
        void *handle = desktopViewOpen((__bridge void *)window, argv[1]);
        if (!handle) return 1;
        desktopViewLayout(handle, 0, 0, 600, 420);
        verify((__bridge WKWebView *)handle, [NSPasteboard generalPasteboard].changeCount, CFAbsoluteTimeGetCurrent() + 10);
        [app run];
    }
}

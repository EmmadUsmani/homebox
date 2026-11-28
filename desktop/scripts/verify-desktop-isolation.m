#import <AppKit/AppKit.h>
#import <WebKit/WebKit.h>
#import "../desktop_view.h"

static void clickProbe(WKWebView *view, NSDictionary *rect, NSInteger clipboard, BOOL retry);

static void readResult(WKWebView *view, NSDictionary *rect, NSInteger clipboard, BOOL retry, CFAbsoluteTime deadline) {
    [view evaluateJavaScript:@"[Boolean(window.probeStarted), window.result || '']" completionHandler:^(NSArray *state, NSError *error) {
        NSString *result = error ? @"" : state.lastObject;
        if (result.length == 0 && (!NSApp.isActive || !view.window.isKeyWindow)) {
            fputs("Viewer focus lost before the guest reported a result\n", stderr);
            exit(1);
        }
        if (result.length == 0 && CFAbsoluteTimeGetCurrent() < deadline) {
            dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 100 * NSEC_PER_MSEC), dispatch_get_main_queue(), ^{
                readResult(view, rect, clipboard, retry, deadline);
            });
            return;
        }
        if (retry && !error && ![state.firstObject boolValue]) {
            clickProbe(view, rect, clipboard, NO);
            return;
        }
        BOOL clipboardUnchanged = [NSPasteboard generalPasteboard].changeCount == clipboard;
        BOOL pass = [result isEqual:@"PASS"] && clipboardUnchanged;
        fprintf(pass ? stdout : stderr, "Guest isolation %s (%s), pasteboard %s\n",
            pass ? "passed" : "failed", result.length ? result.UTF8String : "no result",
            clipboardUnchanged ? "unchanged" : "changed");
        exit(pass ? 0 : 1);
    }];
}

static void sendProbeWhenFocused(WKWebView *view, NSDictionary *rect, NSInteger clipboard, BOOL retry, CFAbsoluteTime deadline) {
    if (!NSApp.isActive || !view.window.isKeyWindow) {
        if (CFAbsoluteTimeGetCurrent() >= deadline) {
            fprintf(stderr, "Viewer focus lost before clicking the guest probe (active=%d, key=%d)\n",
                NSApp.isActive, view.window.isKeyWindow);
            exit(1);
        }
        dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 100 * NSEC_PER_MSEC), dispatch_get_main_queue(), ^{
            sendProbeWhenFocused(view, rect, clipboard, retry, deadline);
        });
        return;
    }
    CGFloat x = [rect[@"x"] doubleValue] + [rect[@"width"] doubleValue] / 2;
    CGFloat y = [rect[@"y"] doubleValue] + [rect[@"height"] doubleValue] / 2;
    NSPoint point = [view convertPoint:NSMakePoint(x, view.isFlipped ? y : NSHeight(view.bounds) - y) toView:nil];
    for (int index = 0; index < 2; index++) {
        NSEvent *click = [NSEvent mouseEventWithType:index == 0 ? NSEventTypeLeftMouseDown : NSEventTypeLeftMouseUp
            location:point modifierFlags:0 timestamp:NSProcessInfo.processInfo.systemUptime
            windowNumber:view.window.windowNumber context:nil eventNumber:index clickCount:1 pressure:index == 0 ? 1 : 0];
        [NSApp sendEvent:click];
    }
    readResult(view, rect, clipboard, retry, CFAbsoluteTimeGetCurrent() + 3);
}

static void clickProbe(WKWebView *view, NSDictionary *rect, NSInteger clipboard, BOOL retry) {
    [NSApp activateIgnoringOtherApps:YES];
    [view.window orderFrontRegardless];
    [view.window makeKeyAndOrderFront:nil];
    [view.window makeFirstResponder:view];
    sendProbeWhenFocused(view, rect, clipboard, retry, CFAbsoluteTimeGetCurrent() + 3);
}

static void verify(WKWebView *view, NSInteger clipboard, CFAbsoluteTime deadline) {
    if (CFAbsoluteTimeGetCurrent() >= deadline) {
        fputs("Timed out waiting for the guest test page\n", stderr);
        exit(1);
    }
    [view evaluateJavaScript:@"document.querySelector('#probe')?.getBoundingClientRect().toJSON()" completionHandler:^(id value, NSError *error) {
        if ([value isKindOfClass:NSDictionary.class]) {
            clickProbe(view, value, clipboard, YES);
        } else {
            dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 100 * NSEC_PER_MSEC), dispatch_get_main_queue(), ^{
                verify(view, clipboard, deadline);
            });
        }
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
        [window orderFrontRegardless];
        [app activateIgnoringOtherApps:YES];
        void *handle = desktopViewOpen((__bridge void *)window, argv[1]);
        if (!handle) return 1;
        desktopViewLayout(handle, 0, 0, 600, 420);
        verify((__bridge WKWebView *)handle, [NSPasteboard generalPasteboard].changeCount, CFAbsoluteTimeGetCurrent() + 10);
        [app run];
    }
}

//go:build cef

#import <Cocoa/Cocoa.h>

#include "app_darwin.h"

// The protocols from include/cef_application_mac.h. That header
// declares them only when OS_MAC is set, which comes from CEF's C++
// headers. The runtime matches protocols by name.
@protocol CrAppProtocol
- (BOOL)isHandlingSendEvent;
@end

@protocol CrAppControlProtocol <CrAppProtocol>
- (void)setHandlingSendEvent:(BOOL)handlingSendEvent;
@end

@protocol CefAppProtocol <CrAppControlProtocol>
@end

// TubelessApplication tells Chromium when an event is being sent, so
// its message pump does not run nested work in the middle of one.
@interface TubelessApplication : NSApplication <CefAppProtocol> {
	BOOL handlingSendEvent_;
}
@end

@implementation TubelessApplication

- (BOOL)isHandlingSendEvent {
	return handlingSendEvent_;
}

- (void)setHandlingSendEvent:(BOOL)handlingSendEvent {
	handlingSendEvent_ = handlingSendEvent;
}

- (void)sendEvent:(NSEvent*)event {
	BOOL wasHandling = handlingSendEvent_;
	handlingSendEvent_ = YES;
	[super sendEvent:event];
	handlingSendEvent_ = wasHandling;
}

@end

void tb_app_prepare(void) {
	@autoreleasepool {
		[TubelessApplication sharedApplication];
	}
}

int tb_app_is_ready(void) {
	return [NSApp conformsToProtocol:@protocol(CefAppProtocol)] ? 1 : 0;
}

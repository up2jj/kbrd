#import <AppKit/AppKit.h>

@interface KBRDShareDelegate : NSObject <NSApplicationDelegate, NSSharingServicePickerDelegate, NSSharingServiceDelegate>
@property(nonatomic, strong) NSURL *cardURL;
@property(nonatomic, strong) NSWindow *anchorWindow;
@property(nonatomic, strong) NSSharingServicePicker *picker;
@end

@implementation KBRDShareDelegate

- (void)applicationDidFinishLaunching:(NSNotification *)notification {
    [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];

    self.anchorWindow = [[NSWindow alloc]
        initWithContentRect:NSMakeRect(0, 0, 2, 2)
                  styleMask:NSWindowStyleMaskBorderless
                    backing:NSBackingStoreBuffered
                      defer:NO];
    self.anchorWindow.opaque = NO;
    self.anchorWindow.backgroundColor = NSColor.clearColor;
    self.anchorWindow.level = NSFloatingWindowLevel;
    [self.anchorWindow center];
    [self.anchorWindow makeKeyAndOrderFront:nil];

    [NSApp activateIgnoringOtherApps:YES];

    self.picker = [[NSSharingServicePicker alloc] initWithItems:@[self.cardURL]];
    self.picker.delegate = self;
    dispatch_async(dispatch_get_main_queue(), ^{
        [self.picker showRelativeToRect:self.anchorWindow.contentView.bounds
                                 ofView:self.anchorWindow.contentView
                          preferredEdge:NSMaxYEdge];
    });
}

- (void)sharingServicePicker:(NSSharingServicePicker *)picker
     didChooseSharingService:(NSSharingService *)service {
    if (service == nil) {
        [NSApp terminate:nil];
        return;
    }
    service.delegate = self;
}

- (void)sharingService:(NSSharingService *)sharingService didShareItems:(NSArray *)items {
    [NSApp terminate:nil];
}

- (void)sharingService:(NSSharingService *)sharingService
    didFailToShareItems:(NSArray *)items
                  error:(NSError *)error {
    [NSApp terminate:nil];
}

@end

int main(int argc, const char *argv[]) {
    @autoreleasepool {
        if (argc != 2) {
            fprintf(stderr, "usage: kbrd-share <card-path>\n");
            return 2;
        }

        KBRDShareDelegate *delegate = [KBRDShareDelegate new];
        delegate.cardURL = [NSURL fileURLWithPath:[NSString stringWithUTF8String:argv[1]]];
        NSApplication *application = NSApplication.sharedApplication;
        application.delegate = delegate;
        [application run];
    }
    return 0;
}

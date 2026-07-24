#import <AppKit/AppKit.h>
#import <Carbon/Carbon.h>
#import <dispatch/dispatch.h>
#import <UserNotifications/UserNotifications.h>

static NSString * const CardCategory = @"KBRD_CARD";
static NSString * const SyncCategory = @"KBRD_SYNC";
static NSString * const OpenAction = @"open-card";
static NSString * const DoneAction = @"mark-done";
static NSString * const SnoozeAction = @"snooze-due";
static NSString * const RetryAction = @"retry-sync";

@interface AppDelegate : NSObject <NSApplicationDelegate, NSWindowDelegate, NSSearchFieldDelegate, UNUserNotificationCenterDelegate>
@property NSStatusItem *statusItem;
@property NSPanel *panel;
@property NSPopUpButton *board;
@property NSPopUpButton *column;
@property NSTextField *titleField;
@property NSTextView *body;
@property NSSearchField *clipboardSearch;
@property NSPopUpButton *clipboard;
@property NSTextField *gitStatus;
@property NSTextField *remindersStatus;
@property NSTextField *message;
@property NSArray *boards;
@property NSArray *clipboardEntries;
@property NSString *kbrdPath;
@property EventHotKeyRef hotKey;
- (void)toggleCapture;
@end

static OSStatus HotKeyHandler(EventHandlerCallRef next, EventRef event, void *data) {
    [(__bridge AppDelegate *)data toggleCapture];
    return noErr;
}

static NSTextField *Label(NSString *text, NSRect frame) {
    NSTextField *label = [[NSTextField alloc] initWithFrame:frame];
    label.stringValue = text;
    label.editable = NO;
    label.bezeled = NO;
    label.drawsBackground = NO;
    label.font = [NSFont systemFontOfSize:11 weight:NSFontWeightMedium];
    label.textColor = NSColor.secondaryLabelColor;
    return label;
}

@implementation AppDelegate

- (void)applicationWillFinishLaunching:(NSNotification *)note {
    UNUserNotificationCenter *center = UNUserNotificationCenter.currentNotificationCenter;
    center.delegate = self;
    UNNotificationAction *open = [UNNotificationAction actionWithIdentifier:OpenAction title:@"Open card" options:UNNotificationActionOptionForeground];
    UNNotificationAction *done = [UNNotificationAction actionWithIdentifier:DoneAction title:@"Mark done" options:UNNotificationActionOptionNone];
    UNNotificationAction *snooze = [UNNotificationAction actionWithIdentifier:SnoozeAction title:@"Snooze due date" options:UNNotificationActionOptionNone];
    UNNotificationAction *retry = [UNNotificationAction actionWithIdentifier:RetryAction title:@"Retry sync" options:UNNotificationActionOptionNone];
    UNNotificationCategory *card = [UNNotificationCategory categoryWithIdentifier:CardCategory actions:@[open, done, snooze] intentIdentifiers:@[] options:UNNotificationCategoryOptionCustomDismissAction];
    UNNotificationCategory *sync = [UNNotificationCategory categoryWithIdentifier:SyncCategory actions:@[retry] intentIdentifiers:@[] options:UNNotificationCategoryOptionCustomDismissAction];
    [center setNotificationCategories:[NSSet setWithObjects:card, sync, nil]];
    [center requestAuthorizationWithOptions:UNAuthorizationOptionAlert completionHandler:^(BOOL granted, NSError *error) {}];
}

- (void)applicationDidFinishLaunching:(NSNotification *)note {
    [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
    [self installMenus];
    self.kbrdPath = [NSBundle.mainBundle pathForResource:@"kbrd" ofType:nil];
    self.statusItem = [NSStatusBar.systemStatusBar statusItemWithLength:NSSquareStatusItemLength];
    self.statusItem.button.image = [NSImage imageWithSystemSymbolName:@"square.and.pencil" accessibilityDescription:@"kbrd quick capture"];
    self.statusItem.button.target = self;
    self.statusItem.button.action = @selector(toggleCapture);
    [self buildPanel];
    [self installHotKey];
}

- (void)installHotKey {
    UInt32 keyCode = kVK_ANSI_K, modifiers = cmdKey | shiftKey;
    NSString *label = @"Command-Shift-K", *error = nil;
    NSDictionary *settings = [self run:@[@"companion", @"hotkey"] input:nil error:&error];
    if (settings) {
        keyCode = [settings[@"key_code"] unsignedIntValue];
        modifiers = [settings[@"modifiers"] unsignedIntValue];
        if ([settings[@"label"] length]) label = settings[@"label"];
    }

    EventHotKeyID keyID = {'KBRD', 1};
    EventTypeSpec spec = {kEventClassKeyboard, kEventHotKeyPressed};
    InstallApplicationEventHandler(&HotKeyHandler, 1, &spec, (__bridge void *)self, NULL);
    OSStatus status = RegisterEventHotKey(keyCode, modifiers, keyID, GetApplicationEventTarget(), 0, &_hotKey);
    if (status != noErr) self.message.stringValue = [NSString stringWithFormat:@"%@ is unavailable", label];
    else if (!settings) self.message.stringValue = [NSString stringWithFormat:@"Invalid shortcut; %@ is active", label];
    else self.message.stringValue = [NSString stringWithFormat:@"%@ opens this window", label];
}

- (void)application:(NSApplication *)application openURLs:(NSArray<NSURL *> *)urls {
    for (NSURL *url in urls) [self deliverNotificationURL:url];
}

- (void)deliverNotificationURL:(NSURL *)url {
    if (![url.scheme isEqualToString:@"kbrd-notify"] || ![url.host isEqualToString:@"deliver"]) return;
    NSURLComponents *parts = [NSURLComponents componentsWithURL:url resolvingAgainstBaseURL:NO];
    NSMutableDictionary *values = [NSMutableDictionary dictionary];
    for (NSURLQueryItem *item in parts.queryItems) if (item.value) values[item.name] = item.value;
    NSString *message = values[@"message"];
    if (!message.length) return;
    UNMutableNotificationContent *content = [[UNMutableNotificationContent alloc] init];
    content.title = values[@"title"] ?: @"kbrd";
    content.body = message;
    content.userInfo = values;
    if ([values[@"card"] length] && [values[@"route"] length]) content.categoryIdentifier = CardCategory;
    else if ([values[@"sync"] length] && [values[@"route"] length]) content.categoryIdentifier = SyncCategory;
    NSString *identifier = [NSString stringWithFormat:@"kbrd-%@", NSUUID.UUID.UUIDString];
    UNNotificationRequest *request = [UNNotificationRequest requestWithIdentifier:identifier content:content trigger:nil];
    [UNUserNotificationCenter.currentNotificationCenter addNotificationRequest:request withCompletionHandler:nil];
}

- (void)userNotificationCenter:(UNUserNotificationCenter *)center
 didReceiveNotificationResponse:(UNNotificationResponse *)response
          withCompletionHandler:(void (^)(void))completionHandler {
    NSString *action = response.actionIdentifier;
    NSDictionary *info = response.notification.request.content.userInfo;
    if ([action isEqualToString:UNNotificationDefaultActionIdentifier] && [info[@"card"] length]) action = OpenAction;
    if ([action isEqualToString:UNNotificationDismissActionIdentifier]) { completionHandler(); return; }
    if (![action isEqualToString:OpenAction] && ![action isEqualToString:DoneAction] &&
        ![action isEqualToString:SnoozeAction] && ![action isEqualToString:RetryAction]) {
        completionHandler(); return;
    }
    NSMutableArray *arguments = [NSMutableArray arrayWithArray:@[@"companion", @"notification-action",
        @"--route", info[@"route"] ?: @"", @"--action", action,
        @"--board", info[@"board"] ?: @""]];
    if ([info[@"card"] length]) [arguments addObjectsFromArray:@[@"--card", info[@"card"]]];
    if ([info[@"sync"] length]) [arguments addObjectsFromArray:@[@"--sync", info[@"sync"]]];
    dispatch_async(dispatch_get_global_queue(QOS_CLASS_USER_INITIATED, 0), ^{
        NSString *error = nil;
        [self run:arguments input:nil error:&error];
        if ([action isEqualToString:OpenAction]) [self activateTerminal:info[@"terminal"]];
        completionHandler();
    });
}

- (void)userNotificationCenter:(UNUserNotificationCenter *)center
       willPresentNotification:(UNNotification *)notification
         withCompletionHandler:(void (^)(UNNotificationPresentationOptions options))completionHandler {
    completionHandler(UNNotificationPresentationOptionBanner | UNNotificationPresentationOptionList);
}

- (void)activateTerminal:(NSString *)program {
    NSDictionary *bundles = @{
        @"Apple_Terminal": @"com.apple.Terminal", @"iTerm.app": @"com.googlecode.iterm2",
        @"WezTerm": @"com.github.wez.wezterm", @"ghostty": @"com.mitchellh.ghostty",
        @"Ghostty": @"com.mitchellh.ghostty", @"kitty": @"net.kovidgoyal.kitty"
    };
    NSString *bundle = bundles[program];
    if (!bundle.length) return;
    NSRunningApplication *app = [NSRunningApplication runningApplicationsWithBundleIdentifier:bundle].firstObject;
    [app activateWithOptions:NSApplicationActivateIgnoringOtherApps];
}

- (void)installMenus {
    NSMenu *main = [[NSMenu alloc] initWithTitle:@""];

    NSMenuItem *appItem = [[NSMenuItem alloc] initWithTitle:@"kbrd Companion" action:nil keyEquivalent:@""];
    NSMenu *appMenu = [[NSMenu alloc] initWithTitle:@"kbrd Companion"];
    NSMenuItem *quit = [[NSMenuItem alloc] initWithTitle:@"Quit kbrd Companion" action:@selector(terminate:) keyEquivalent:@"q"];
    quit.target = NSApp;
    [appMenu addItem:quit];
    appItem.submenu = appMenu;
    [main addItem:appItem];

    NSMenuItem *editItem = [[NSMenuItem alloc] initWithTitle:@"Edit" action:nil keyEquivalent:@""];
    NSMenu *editMenu = [[NSMenu alloc] initWithTitle:@"Edit"];
    [editMenu addItemWithTitle:@"Copy" action:@selector(copy:) keyEquivalent:@"c"];
    [editMenu addItemWithTitle:@"Paste" action:@selector(paste:) keyEquivalent:@"v"];
    [editMenu addItem:NSMenuItem.separatorItem];
    [editMenu addItemWithTitle:@"Select All" action:@selector(selectAll:) keyEquivalent:@"a"];
    editItem.submenu = editMenu;
    [main addItem:editItem];

    NSApp.mainMenu = main;
}

- (void)applicationWillTerminate:(NSNotification *)note {
    if (self.hotKey) UnregisterEventHotKey(self.hotKey);
}

- (void)buildPanel {
    self.panel = [[NSPanel alloc] initWithContentRect:NSMakeRect(0, 0, 460, 520)
                                           styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskUtilityWindow
                                             backing:NSBackingStoreBuffered defer:NO];
    self.panel.title = @"kbrd quick capture";
    self.panel.delegate = self;
    self.panel.floatingPanel = YES;
    self.panel.hidesOnDeactivate = YES;
    NSView *v = self.panel.contentView;

    [v addSubview:Label(@"BOARD", NSMakeRect(20, 478, 90, 16))];
    self.board = [[NSPopUpButton alloc] initWithFrame:NSMakeRect(20, 445, 205, 28) pullsDown:NO];
    self.board.target = self; self.board.action = @selector(boardChanged:); [v addSubview:self.board];
    [v addSubview:Label(@"COLUMN", NSMakeRect(235, 478, 90, 16))];
    self.column = [[NSPopUpButton alloc] initWithFrame:NSMakeRect(235, 445, 205, 28) pullsDown:NO];
    self.column.target = self; self.column.action = @selector(columnChanged:); [v addSubview:self.column];

    [v addSubview:Label(@"TITLE", NSMakeRect(20, 418, 90, 16))];
    self.titleField = [[NSTextField alloc] initWithFrame:NSMakeRect(20, 387, 420, 26)];
    self.titleField.placeholderString = @"Card title"; [v addSubview:self.titleField];
    [v addSubview:Label(@"NOTES", NSMakeRect(20, 360, 90, 16))];
    NSScrollView *scroll = [[NSScrollView alloc] initWithFrame:NSMakeRect(20, 195, 420, 160)];
    scroll.hasVerticalScroller = YES; scroll.borderType = NSBezelBorder;
    self.body = [[NSTextView alloc] initWithFrame:scroll.bounds];
    self.body.font = [NSFont monospacedSystemFontOfSize:12 weight:NSFontWeightRegular];
    self.body.autoresizingMask = NSViewWidthSizable; scroll.documentView = self.body; [v addSubview:scroll];

    [v addSubview:Label(@"CLIPBOARD HISTORY", NSMakeRect(20, 169, 150, 16))];
    self.clipboardSearch = [[NSSearchField alloc] initWithFrame:NSMakeRect(20, 137, 205, 26)];
    self.clipboardSearch.placeholderString = @"Search"; self.clipboardSearch.delegate = self;
    self.clipboardSearch.target = self; self.clipboardSearch.action = @selector(filterClipboard:); [v addSubview:self.clipboardSearch];
    self.clipboard = [[NSPopUpButton alloc] initWithFrame:NSMakeRect(235, 137, 205, 26) pullsDown:NO];
    self.clipboard.target = self; self.clipboard.action = @selector(useClipboard:); [v addSubview:self.clipboard];

    self.gitStatus = Label(@"Git: —", NSMakeRect(20, 105, 205, 18)); [v addSubview:self.gitStatus];
    self.remindersStatus = Label(@"Reminders: —", NSMakeRect(235, 105, 205, 18)); [v addSubview:self.remindersStatus];
    self.message = Label(@"Command-Shift-K opens this window", NSMakeRect(20, 70, 300, 18)); [v addSubview:self.message];

    NSButton *scratch = [[NSButton alloc] initWithFrame:NSMakeRect(236, 24, 98, 34)];
    scratch.title = @"Scratchpad"; scratch.bezelStyle = NSBezelStyleRounded;
    scratch.target = self; scratch.action = @selector(saveScratchpad:); [v addSubview:scratch];
    NSButton *capture = [[NSButton alloc] initWithFrame:NSMakeRect(340, 24, 100, 34)];
    capture.title = @"Capture"; capture.bezelStyle = NSBezelStyleRounded;
    capture.keyEquivalent = @"\r"; capture.target = self; capture.action = @selector(capture:); [v addSubview:capture];
    NSButton *quit = [[NSButton alloc] initWithFrame:NSMakeRect(20, 24, 60, 34)];
    quit.title = @"Quit"; quit.bezelStyle = NSBezelStyleRounded;
    quit.target = NSApp; quit.action = @selector(terminate:); [v addSubview:quit];
    NSButton *loadPad = [[NSButton alloc] initWithFrame:NSMakeRect(86, 24, 86, 34)];
    loadPad.title = @"View Pad"; loadPad.bezelStyle = NSBezelStyleRounded;
    loadPad.target = self; loadPad.action = @selector(loadScratchpad:); [v addSubview:loadPad];
}

- (NSDictionary *)run:(NSArray<NSString *> *)arguments input:(NSString *)input error:(NSString **)errorText {
    if (!self.kbrdPath.length) { if (errorText) *errorText = @"kbrd executable is unavailable"; return nil; }
    NSTask *task = [[NSTask alloc] init];
    task.executableURL = [NSURL fileURLWithPath:self.kbrdPath]; task.arguments = arguments;
    NSPipe *out = NSPipe.pipe, *err = NSPipe.pipe; task.standardOutput = out; task.standardError = err;
    NSPipe *in = input ? NSPipe.pipe : nil;
    if (in) task.standardInput = in;
    NSError *launchError = nil;
    if (![task launchAndReturnError:&launchError]) { if (errorText) *errorText = launchError.localizedDescription; return nil; }

    dispatch_group_t group = dispatch_group_create();
    dispatch_queue_t queue = dispatch_get_global_queue(QOS_CLASS_USER_INITIATED, 0);
    __block NSData *output = nil, *errorData = nil;
    dispatch_group_async(group, queue, ^{ output = [out.fileHandleForReading readDataToEndOfFile]; });
    dispatch_group_async(group, queue, ^{ errorData = [err.fileHandleForReading readDataToEndOfFile]; });
    if (in) {
        NSData *inputData = [input dataUsingEncoding:NSUTF8StringEncoding];
        dispatch_group_async(group, queue, ^{
            [in.fileHandleForWriting writeData:inputData];
            [in.fileHandleForWriting closeFile];
        });
    }
    [task waitUntilExit];
    dispatch_group_wait(group, DISPATCH_TIME_FOREVER);
    if (task.terminationStatus != 0) {
        if (errorText) *errorText = [[NSString alloc] initWithData:errorData encoding:NSUTF8StringEncoding];
        return nil;
    }
    if (!output.length) return @{};
    id decoded = [NSJSONSerialization JSONObjectWithData:output options:0 error:nil];
    return [decoded isKindOfClass:NSDictionary.class] ? decoded : @{};
}

- (void)reload {
    NSString *error = nil; NSDictionary *snapshot = [self run:@[@"companion", @"snapshot"] input:nil error:&error];
    if (!snapshot) { self.message.stringValue = error ?: @"Could not load boards"; return; }
    self.boards = snapshot[@"boards"] ?: @[]; self.clipboardEntries = snapshot[@"clipboard"] ?: @[];
    NSString *savedBoard = [NSUserDefaults.standardUserDefaults stringForKey:@"boardPath"];
    [self.board removeAllItems]; NSInteger selected = 0;
    NSMutableArray *available = [NSMutableArray array];
    for (NSDictionary *item in self.boards) if ([item[@"available"] boolValue]) [available addObject:item];
    self.boards = available;
    for (NSInteger i = 0; i < self.boards.count; i++) {
        NSDictionary *item = self.boards[i]; [self.board addItemWithTitle:item[@"name"] ?: item[@"path"]];
        if ([item[@"path"] isEqualToString:savedBoard]) selected = i;
    }
    if (self.boards.count) [self.board selectItemAtIndex:selected];
    [self boardChanged:nil]; [self filterClipboard:nil];
}

- (void)toggleCapture {
    if (self.panel.visible) { [self.panel orderOut:nil]; return; }
    [self reload]; [self.panel center]; [self.panel makeKeyAndOrderFront:nil]; [NSApp activateIgnoringOtherApps:YES];
    [self.panel makeFirstResponder:self.titleField];
}

- (NSDictionary *)selectedBoard { NSInteger i = self.board.indexOfSelectedItem; return i >= 0 && i < self.boards.count ? self.boards[i] : nil; }

- (void)boardChanged:(id)sender {
    NSDictionary *item = self.selectedBoard; [self.column removeAllItems];
    for (NSString *column in item[@"columns"] ?: @[]) [self.column addItemWithTitle:column];
    NSString *path = item[@"path"];
    if (path) [NSUserDefaults.standardUserDefaults setObject:path forKey:@"boardPath"];
    NSString *saved = path ? [NSUserDefaults.standardUserDefaults stringForKey:[@"column:" stringByAppendingString:path]] : nil;
    if (saved && [self.column itemWithTitle:saved]) [self.column selectItemWithTitle:saved];
    self.gitStatus.stringValue = [@"Git: " stringByAppendingString:item[@"git"] ?: @"—"];
    self.remindersStatus.stringValue = [@"Reminders: " stringByAppendingString:item[@"reminders"] ?: @"—"];
}

- (void)columnChanged:(id)sender {
    NSString *path = self.selectedBoard[@"path"];
    if (path && self.column.titleOfSelectedItem) [NSUserDefaults.standardUserDefaults setObject:self.column.titleOfSelectedItem forKey:[@"column:" stringByAppendingString:path]];
}

- (void)filterClipboard:(id)sender {
    NSString *query = self.clipboardSearch.stringValue.lowercaseString; [self.clipboard removeAllItems];
    [self.clipboard addItemWithTitle:@"Choose saved text…"]; self.clipboard.lastItem.representedObject = nil;
    for (NSDictionary *entry in self.clipboardEntries) {
        NSString *text = entry[@"text"] ?: @"";
        if (query.length && [text.lowercaseString rangeOfString:query].location == NSNotFound) continue;
        NSString *oneLine = [[text componentsSeparatedByCharactersInSet:NSCharacterSet.newlineCharacterSet] componentsJoinedByString:@" "];
        if (oneLine.length > 55) oneLine = [[oneLine substringToIndex:55] stringByAppendingString:@"…"];
        [self.clipboard addItemWithTitle:oneLine.length ? oneLine : @"(empty)"]; self.clipboard.lastItem.representedObject = entry;
    }
}

- (void)controlTextDidChange:(NSNotification *)notification { if (notification.object == self.clipboardSearch) [self filterClipboard:nil]; }
- (void)useClipboard:(id)sender { NSDictionary *entry = self.clipboard.selectedItem.representedObject; if (entry) [self.body insertText:entry[@"text"] replacementRange:self.body.selectedRange]; }

- (void)capture:(id)sender {
    NSDictionary *item = self.selectedBoard;
    if (!item || !self.titleField.stringValue.length) { self.message.stringValue = @"Choose a board and enter a title"; return; }
    NSDictionary *payload = @{
        @"board": item[@"path"] ?: @"",
        @"column": self.column.titleOfSelectedItem ?: @"1",
        @"name": self.titleField.stringValue,
        @"content": self.body.string ?: @"",
        @"source_app": @"kbrd Companion"
    };
    NSData *encoded = [NSJSONSerialization dataWithJSONObject:payload options:0 error:nil];
    NSString *input = [[NSString alloc] initWithData:encoded encoding:NSUTF8StringEncoding];
    NSString *error = nil;
    NSDictionary *result = [self run:@[@"companion", @"capture"] input:input error:&error];
    if (!result) { self.message.stringValue = error ?: @"Capture failed"; return; }
    NSArray *warnings = result[@"warnings"] ?: @[];
    self.titleField.stringValue = @""; [self.body setString:@""];
    self.message.stringValue = warnings.count ? [NSString stringWithFormat:@"Captured · warning: %@", warnings.firstObject[@"message"] ?: @"hook failed"] : @"Captured";
    if (!warnings.count) [self.panel orderOut:nil];
}

- (void)saveScratchpad:(id)sender {
    NSDictionary *item = self.selectedBoard; NSString *text = self.body.string ?: @"";
    if (!item || !text.length) { self.message.stringValue = @"Choose a board and enter scratchpad text"; return; }
    NSString *error = nil;
    if (![self run:@[@"companion", @"scratchpad", @"--board", item[@"path"]] input:text error:&error]) { self.message.stringValue = error ?: @"Scratchpad failed"; return; }
    [self.body setString:@""]; self.message.stringValue = @"Added to scratchpad";
}

- (void)loadScratchpad:(id)sender {
    NSDictionary *item = self.selectedBoard;
    if (!item) { self.message.stringValue = @"Choose a board"; return; }
    [self.body setString:item[@"scratchpad"] ?: @""];
    self.message.stringValue = @"Scratchpad loaded into notes";
}

- (BOOL)applicationShouldTerminateAfterLastWindowClosed:(NSApplication *)sender { return NO; }
- (BOOL)applicationShouldHandleReopen:(NSApplication *)sender hasVisibleWindows:(BOOL)visible {
    if (!visible) [self toggleCapture];
    return YES;
}
@end

int main(int argc, const char *argv[]) {
    @autoreleasepool {
        NSApplication *app = NSApplication.sharedApplication;
        AppDelegate *delegate = [AppDelegate new]; app.delegate = delegate; [app run];
    }
    return 0;
}

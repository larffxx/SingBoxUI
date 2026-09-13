// tray_darwin.m implements the macOS menu bar item of SingBoxUI.
//
// The status item is a process-wide singleton, like the menu bar itself: there is
// exactly one, it is created on the main thread and its icon and menu are
// replaced wholesale on every update. Nothing here touches application state —
// an NSStatusItem needs neither an application delegate nor a window, so this
// file deliberately defines no delegate: the one the process has belongs to
// Wails, and a second class of the same name would be a collision between two
// frameworks instead of a menu bar icon.

#import "tray_darwin.h"

#import <Cocoa/Cocoa.h>

// Menu selection target. AppKit sends the action to this object; it forwards the
// identifier the Go side put into the item and returns to the event loop.
@interface SBUTrayTarget : NSObject
- (void)selectItem:(NSMenuItem *)sender;
@end

@implementation SBUTrayTarget

- (void)selectItem:(NSMenuItem *)sender {
    NSString *identifier = sender.representedObject;
    if (![identifier isKindOfClass:[NSString class]] || identifier.length == 0) {
        return;
    }
    sbuTraySelect((char *)identifier.UTF8String);
}

@end

static NSStatusItem *sbuStatusItem = nil;
static SBUTrayTarget *sbuTarget = nil;
static bool sbuReady = false;

// sbuOnMainThread runs the block on the main thread, immediately when it is
// already there. Cocoa user-interface calls are only legal on that thread, and
// the callers in Go are goroutines (the Wails startup hook runs on one).
static void sbuOnMainThread(dispatch_block_t block) {
    if ([NSThread isMainThread]) {
        block();
        return;
    }
    dispatch_async(dispatch_get_main_queue(), block);
}

// sbuSymbolImage maps the platform-neutral icon name of the model onto an SF
// Symbol. A symbol that this macOS version does not know would leave an empty
// slot in the menu bar — the one outcome a menu bar icon must never produce — so
// the outline shipped by every supported version is the fallback. Template
// images take their colour from the menu bar, which is what makes the icon legible
// in both appearances.
static NSImage *sbuSymbolImage(NSString *state) {
    NSString *name = @"shield";
    if ([state isEqualToString:@"active"]) {
        name = @"shield.fill";
    } else if ([state isEqualToString:@"busy"]) {
        name = @"shield.lefthalf.filled";
    } else if ([state isEqualToString:@"failed"]) {
        name = @"exclamationmark.shield.fill";
    }
    NSImage *image = [NSImage imageWithSystemSymbolName:name accessibilityDescription:@"SingBoxUI"];
    if (image == nil) {
        sbuTrayProblem((char *)[[NSString stringWithFormat:@"no symbol named %@ on this macOS", name] UTF8String]);
        image = [NSImage imageWithSystemSymbolName:@"shield" accessibilityDescription:@"SingBoxUI"];
    }
    if (image == nil) {
        sbuTrayProblem((char *)[@"no shield symbol on this macOS" UTF8String]);
    }
    image.template = YES;
    return image;
}

// sbuApplyModel builds the status item from a model, creating it on first use.
static void sbuApplyModel(NSString *json) {
    NSData *data = [json dataUsingEncoding:NSUTF8StringEncoding];
    NSError *error = nil;
    id parsed = [NSJSONSerialization JSONObjectWithData:data options:0 error:&error];
    if (![parsed isKindOfClass:[NSDictionary class]]) {
        sbuTrayProblem((char *)[[NSString stringWithFormat:@"the model was rejected: %@",
                                 error.localizedDescription ?: @"not a JSON object"] UTF8String]);
        return;
    }
    NSDictionary *model = (NSDictionary *)parsed;

    if (sbuStatusItem == nil) {
        sbuStatusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSSquareStatusItemLength];
        sbuTarget = [[SBUTrayTarget alloc] init];
        if (sbuStatusItem == nil) {
            sbuTrayProblem((char *)[@"the system refused a status item" UTF8String]);
            return;
        }
    }

    NSString *tooltip = model[@"tooltip"];
    NSStatusBarButton *button = sbuStatusItem.button;
    button.image = sbuSymbolImage(model[@"icon"]);
    button.toolTip = tooltip;
    button.accessibilityLabel = tooltip;

    NSMenu *menu = [[NSMenu alloc] initWithTitle:@"SingBoxUI"];
    // The model decides what is enabled; AppKit's automatic validation would
    // enable every item whose target implements the action, including the
    // status line, which is text rather than a command.
    menu.autoenablesItems = NO;

    for (id entry in model[@"items"]) {
        if (![entry isKindOfClass:[NSDictionary class]]) {
            continue;
        }
        NSDictionary *row = (NSDictionary *)entry;
        if ([row[@"separator"] boolValue]) {
            [menu addItem:[NSMenuItem separatorItem]];
            continue;
        }
        NSString *title = row[@"title"];
        NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:(title ?: @"")
                                                      action:@selector(selectItem:)
                                               keyEquivalent:@""];
        item.target = sbuTarget;
        item.enabled = [row[@"enabled"] boolValue];
        item.representedObject = row[@"id"];
        [menu addItem:item];
    }

    // Assigning the menu is what makes a click open it; no click handler of our
    // own is needed, and the icon keeps its native behaviour.
    sbuStatusItem.menu = menu;

    // The icon and its menu are in the menu bar only now: this is the one line
    // the application logs about a surface that has nothing else to show.
    if (!sbuReady) {
        sbuReady = YES;
        sbuTrayReady();
    }
}

void sbuTrayShow(const char *modelJSON) {
    if (modelJSON == NULL) {
        return;
    }
    NSString *json = [NSString stringWithUTF8String:modelJSON];
    if (json == nil) {
        return;
    }
    sbuOnMainThread(^{
        sbuApplyModel(json);
    });
}

void sbuTrayHide(void) {
    sbuOnMainThread(^{
        if (sbuStatusItem != nil) {
            [[NSStatusBar systemStatusBar] removeStatusItem:sbuStatusItem];
            sbuStatusItem = nil;
        }
        sbuTarget = nil;
        sbuReady = false;
    });
}

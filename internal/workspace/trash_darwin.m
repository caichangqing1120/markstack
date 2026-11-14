//go:build darwin && cgo

#import <Foundation/Foundation.h>
#include <string.h>

static char *markstack_copy_string(NSString *value) {
    const char *text = value.UTF8String;
    return text == NULL ? NULL : strdup(text);
}

char *markstack_trash(const char *path, char **failure) {
    @autoreleasepool {
        *failure = NULL;
        NSString *name = [NSString stringWithUTF8String:path];
        if (name == nil) {
            *failure = strdup("invalid UTF-8 file path");
            return NULL;
        }
        NSURL *result = nil;
        NSError *error = nil;
        if (![[NSFileManager defaultManager] trashItemAtURL:[NSURL fileURLWithPath:name]
                                               resultingItemURL:&result error:&error]) {
            *failure = markstack_copy_string(error.localizedDescription);
            return NULL;
        }
        return markstack_copy_string(result.path);
    }
}

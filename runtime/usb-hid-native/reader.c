// Non-exclusive macOS reader for one configured USB keyboard-wedge device.
// stdout is a stream of completed US-layout ASCII lines, stderr is status only.
#include <CoreFoundation/CoreFoundation.h>
#include <IOKit/hid/IOHIDManager.h>
#include <errno.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static unsigned shifts;
static char line[4096];
static size_t length;
static int invalid_frame;
static int disconnected;

static void removed(void *ctx, IOReturn result, void *sender) {
    disconnected = 1;
    CFRunLoopStop(CFRunLoopGetCurrent());
}

static void input(void *ctx, IOReturn result, void *sender, IOHIDValueRef value) {
    IOHIDElementRef element = IOHIDValueGetElement(value);
    if (result || IOHIDElementGetUsagePage(element) != 7) return;
    unsigned key = IOHIDElementGetUsage(element);
    int down = IOHIDValueGetIntegerValue(value) != 0;
    if (key == 225 || key == 229) {
        unsigned bit = key == 225 ? 1 : 2;
        shifts = down ? shifts | bit : shifts & ~bit;
        return;
    }
    if (!down || !key) return;
    if (key == 40 || key == 88) {
        if (length && !invalid_frame) {
            fwrite(line, 1, length, stdout);
            putchar('\n');
            fflush(stdout);
        }
        length = 0;
        invalid_frame = 0;
        return;
    }
    char ch = 0;
    if (key >= 4 && key <= 29) ch = (shifts ? 'A' : 'a') + key - 4;
    if (key >= 30 && key <= 39)
        ch = (shifts ? "!@#$%^&*()" : "1234567890")[key - 30];
    if (key >= 44 && key <= 56)
        ch = (shifts ? " _+{}|~:\"~<>?" : " -=[]\\#;'`,./")[key - 44];
    if (!ch || length == sizeof(line)) invalid_frame = 1;
    if (!invalid_frame) line[length++] = ch;
}

static int set_number(CFMutableDictionaryRef match, CFStringRef key, const char *s, long max) {
    char *end;
    errno = 0;
    long n = strtol(s, &end, 0);
    if (errno || end == s || *end || n <= 0 || n > max) return 0;
    int64_t v = n;
    CFNumberRef num = CFNumberCreate(NULL, kCFNumberSInt64Type, &v);
    CFDictionarySetValue(match, key, num);
    CFRelease(num);
    return 1;
}

static uint32_t number_property(IOHIDDeviceRef device, CFStringRef key) {
    CFTypeRef value = IOHIDDeviceGetProperty(device, key);
    int64_t n;
    if (!value || CFGetTypeID(value) != CFNumberGetTypeID() ||
        !CFNumberGetValue(value, kCFNumberSInt64Type, &n)) return 0;
    return (uint32_t)n;
}

static void print_device(const void *value, void *context) {
    IOHIDDeviceRef device = (IOHIDDeviceRef)value;
    uint32_t vendor = number_property(device, CFSTR(kIOHIDVendorIDKey));
    uint32_t product = number_property(device, CFSTR(kIOHIDProductIDKey));
    uint32_t location = number_property(device, CFSTR(kIOHIDLocationIDKey));
    if (!vendor || !product || !location) return;
    char name[512] = {0};
    CFTypeRef label = IOHIDDeviceGetProperty(device, CFSTR(kIOHIDProductKey));
    if (label && CFGetTypeID(label) == CFStringGetTypeID())
        CFStringGetCString(label, name, sizeof(name), kCFStringEncodingUTF8);
    for (char *c = name; *c; c++) if ((unsigned char)*c < 32) *c = ' ';
    printf("usb-hid://device/%04x/%04x/%08x\t%s\n", vendor, product, location, name);
}

int main(int argc, char **argv) {
    int list = argc == 2 && strcmp(argv[1], "--list") == 0;
    if (!list && argc != 4) return 2;
    IOHIDManagerRef manager = IOHIDManagerCreate(NULL, 0);
    CFMutableDictionaryRef match = CFDictionaryCreateMutable(NULL, 0,
        &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    CFDictionarySetValue(match, CFSTR(kIOHIDTransportKey), CFSTR("USB"));
    if (!list && (!set_number(match, CFSTR(kIOHIDVendorIDKey), argv[1], 65535) ||
        !set_number(match, CFSTR(kIOHIDProductIDKey), argv[2], 65535) ||
        !set_number(match, CFSTR(kIOHIDLocationIDKey), argv[3], UINT32_MAX))) return 2;
    if (!set_number(match, CFSTR(kIOHIDPrimaryUsagePageKey), "1", 1) ||
        !set_number(match, CFSTR(kIOHIDPrimaryUsageKey), "6", 6)) return 2;
    IOHIDManagerSetDeviceMatching(manager, match);
    CFRelease(match);
    CFSetRef devices = IOHIDManagerCopyDevices(manager);
    if (list) {
        if (devices) {
            CFSetApplyFunction(devices, print_device, NULL);
            CFRelease(devices);
        }
        CFRelease(manager);
        return 0;
    }
    if (!devices || CFSetGetCount(devices) != 1) {
        fprintf(stderr, "deviceMissingOrAmbiguous\n");
        return 3;
    }
    IOHIDDeviceRef device;
    CFSetGetValues(devices, (const void **)&device);
    IOReturn status = IOHIDDeviceOpen(device, kIOHIDOptionsTypeNone);
    if (status != kIOReturnSuccess) {
        fprintf(stderr, "deviceOpenDenied 0x%x\n", status);
        return 4;
    }
    IOHIDDeviceRegisterRemovalCallback(device, removed, NULL);
    IOHIDDeviceRegisterInputValueCallback(device, input, NULL);
    IOHIDDeviceScheduleWithRunLoop(device, CFRunLoopGetCurrent(), kCFRunLoopDefaultMode);
    fprintf(stderr, "ready\n");
    CFRunLoopRun();
    IOHIDDeviceClose(device, 0);
    CFRelease(devices);
    CFRelease(manager);
    return disconnected ? 5 : 0;
}

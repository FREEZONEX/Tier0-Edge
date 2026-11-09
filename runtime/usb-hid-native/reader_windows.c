// Raw Input delivers a device identity with each event. INPUTSINK preserves
// normal foreground keyboard delivery; never register RIDEV_NOLEGACY.
#include <windows.h>
#include <cfgmgr32.h>
#include <hidsdi.h>
#include <stdio.h>
#include <wchar.h>

static HANDLE selected;
static char frame[4096];
static size_t used;
static int bad;
static BYTE keys[256];
static HKL layout;

static LRESULT CALLBACK receive(HWND window, UINT message, WPARAM wp, LPARAM lp) {
    if (message == WM_INPUT_DEVICE_CHANGE && wp == GIDC_REMOVAL && (HANDLE)lp == selected) {
        PostQuitMessage(5);
    }
    if (message == WM_INPUT) {
        RAWINPUTHEADER header;
        UINT size = sizeof(header);
        if (GetRawInputData((HRAWINPUT)lp, RID_HEADER, &header, &size, sizeof(header)) != sizeof(header)
            || header.hDevice != selected || header.dwType != RIM_TYPEKEYBOARD)
            return DefWindowProcW(window, message, wp, lp);
        RAWINPUT raw;
        size = sizeof(raw);
        if (GetRawInputData((HRAWINPUT)lp, RID_INPUT, &raw, &size, sizeof(header)) == (UINT)-1)
            return DefWindowProcW(window, message, wp, lp);
        RAWKEYBOARD *key = &raw.data.keyboard;
        UINT vk = key->VKey;
        if (vk >= 255) return 0;
        BOOL down = !(key->Flags & RI_KEY_BREAK);
        keys[vk] = down ? 0x80 : 0;
        if (!down || vk == VK_SHIFT || vk == VK_CONTROL || vk == VK_MENU) return 0;
        if (vk == VK_RETURN) {
            if (used && !bad) {
                fwrite(frame, 1, used, stdout);
                putchar('\n');
                fflush(stdout);
            }
            used = 0;
            bad = 0;
        } else {
            WCHAR chars[8];
            int n = ToUnicodeEx(vk, key->MakeCode, keys, chars, 8, 4, layout);
            if (n != 1 || chars[0] < 32 || chars[0] > 126 || used == sizeof(frame)) bad = 1;
            if (!bad) frame[used++] = (char)chars[0];
        }
    }
    return DefWindowProcW(window, message, wp, lp);
}

static BOOL has_usb_parent(wchar_t *instance) {
    DEVINST dev;
    if (CM_Locate_DevNodeW(&dev, instance, CM_LOCATE_DEVNODE_NORMAL) != CR_SUCCESS) return FALSE;
    while (CM_Get_Parent(&dev, dev, 0) == CR_SUCCESS) {
        wchar_t id[MAX_DEVICE_ID_LEN];
        if (CM_Get_Device_IDW(dev, id, MAX_DEVICE_ID_LEN, 0) != CR_SUCCESS) return FALSE;
        if (!_wcsnicmp(id, L"USB\\VID_", 8)) return TRUE;
        // Bluetooth receivers can themselves be USB devices.
        if (!_wcsnicmp(id, L"BTH", 3)) return FALSE;
    }
    return FALSE;
}

static void print_device(const wchar_t *path, wchar_t *instance) {
    wchar_t product[128] = {0};
    HANDLE device = CreateFileW(path, 0, FILE_SHARE_READ | FILE_SHARE_WRITE,
                                NULL, OPEN_EXISTING, 0, NULL);
    if (device != INVALID_HANDLE_VALUE) {
        HidD_GetProductString(device, product, sizeof(product));
        CloseHandle(device);
    }
    product[127] = 0;
    for (wchar_t *c = product; *c; c++) if (*c < 32) *c = L' ';
    char name[512] = {0};
    WideCharToMultiByte(CP_UTF8, 0, product, -1, name, sizeof(name), NULL, NULL);
    for (wchar_t *c = instance; *c; c++) if (*c == L'\\') *c = L'/';
    printf("usb-hid://windows/%ls\t%s\n", instance + 4, name);
}

int wmain(int argc, wchar_t **argv) {
    // Accept the complete PnP instance, including its instance-specific suffix.
    if (argc != 2) return 2;
    BOOL list = !wcscmp(argv[1], L"--list");
    if (!list && (_wcsnicmp(argv[1], L"HID\\VID_", 8) || wcslen(argv[1]) >= 512)) return 2;
    UINT count = 0;
    if (GetRawInputDeviceList(NULL, &count, sizeof(RAWINPUTDEVICELIST)) != 0) return 3;
    if (!count) return list ? 0 : 3;
    RAWINPUTDEVICELIST *devices = calloc(count, sizeof(*devices));
    if (!devices) return 3;
    UINT found = GetRawInputDeviceList(devices, &count, sizeof(*devices));
    if (found == (UINT)-1) return 3;
    for (UINT i = 0; i < found; ++i) {
        if (devices[i].dwType != RIM_TYPEKEYBOARD) continue;
        wchar_t path[1024];
        UINT length = 1024;
        if (GetRawInputDeviceInfoW(devices[i].hDevice, RIDI_DEVICENAME, path, &length) == (UINT)-1 ||
            wcsncmp(path, L"\\\\?\\", 4) != 0) continue;
        wchar_t instance[1024];
        wcscpy(instance, path + 4);
        wchar_t *guid = wcsrchr(instance, L'#');
        if (!guid) continue;
        *guid = 0;
        for (wchar_t *p = instance; *p; p++) if (*p == L'#') *p = L'\\';
        if (_wcsnicmp(instance, L"HID\\VID_", 8) || !has_usb_parent(instance)) continue;
        if (list) {
            print_device(path, instance);
        } else if (_wcsicmp(instance, argv[1]) == 0) {
            if (selected) return 3;
            selected = devices[i].hDevice;
        }
    }
    free(devices);
    if (list) return 0;
    if (!selected) return 3;
    layout = LoadKeyboardLayoutW(L"00000409", 0);
    if (!layout) return 4;
    WNDCLASSW cls = {.lpfnWndProc = receive, .hInstance = GetModuleHandleW(NULL), .lpszClassName = L"Tier0HidInput"};
    if (!RegisterClassW(&cls)) return 4;
    HWND window = CreateWindowExW(0, cls.lpszClassName, L"", 0, 0, 0, 0, 0, HWND_MESSAGE, NULL, cls.hInstance, NULL);
    RAWINPUTDEVICE filter = {1, 6, RIDEV_INPUTSINK | RIDEV_DEVNOTIFY, window};
    if (!window || !RegisterRawInputDevices(&filter, 1, sizeof(filter))) return 4;
    fprintf(stderr, "ready\n");
    MSG msg;
    while (GetMessageW(&msg, NULL, 0, 0) > 0) DispatchMessageW(&msg);
    return (int)msg.wParam;
}

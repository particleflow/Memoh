#include <napi/native_api.h>
#include <filemanagement/file_uri/oh_file_uri.h>
#include <cstdlib>
#include <cstring>
#include <memory>
#include <string>
#include <vector>
#include <climits>
#include <cmath>
#include "folder_publish.h"

static napi_value FolderError(napi_env env, const char *phase, int code)
{
    napi_value message = nullptr, error = nullptr, number = nullptr;
    napi_create_string_utf8(env, phase, NAPI_AUTO_LENGTH, &message);
    napi_create_error(env, nullptr, message, &error);
    napi_create_int32(env, code, &number); napi_set_named_property(env, error, "code", number);
    napi_throw(env, error); return nullptr;
}

// Native-only direct-child open. No path from Web, truncation, link traversal
// or create fallback. The caller keeps the selected directory descriptor.
napi_value FolderOpen(napi_env env, napi_callback_info info)
{
    size_t argc = 3, length = 0, copied = 0;
    napi_value args[3] {};
    double directory = -1, mode = -1;
    char name[256] {};
    if (napi_get_cb_info(env, info, &argc, args, nullptr, nullptr) != napi_ok || argc != 3 ||
        napi_get_value_double(env, args[0], &directory) != napi_ok || !std::isfinite(directory) ||
        directory < 0 || directory > INT_MAX || std::floor(directory) != directory ||
        napi_get_value_string_utf8(env, args[1], nullptr, 0, &length) != napi_ok || length < 1 || length > 255 ||
        napi_get_value_string_utf8(env, args[1], name, sizeof(name), &copied) != napi_ok || copied != length ||
        std::strlen(name) != length || !std::strcmp(name, ".") || !std::strcmp(name, "..") ||
        std::strchr(name, '/') || std::strchr(name, '\\') ||
        napi_get_value_double(env, args[2], &mode) != napi_ok || (mode != 0 && mode != 1 && mode != 2)) {
        napi_throw_error(env, nullptr, "MEDIA_FOLDER_OPEN_ARGUMENT"); return nullptr;
    }
    const int fd = OpenFolderFile(static_cast<int>(directory), name, static_cast<int>(mode));
    if (fd < 0) {
        return FolderError(env, "MEDIA_FOLDER_OPEN", errno);
    }
    napi_value result = nullptr;
    if (napi_create_int32(env, fd, &result) != napi_ok) {
        close(fd); napi_throw_error(env, nullptr, "MEDIA_FOLDER_OPEN_RESULT"); return nullptr;
    }
    return result;
}

napi_value FolderPublish(napi_env env, napi_callback_info info)
{
    size_t argc = 4;
    napi_value args[4] {};
    double directory = -1, source = -1;
    char names[2][256] {};
    bool valid = napi_get_cb_info(env, info, &argc, args, nullptr, nullptr) == napi_ok && argc == 4 &&
        napi_get_value_double(env, args[0], &directory) == napi_ok &&
        napi_get_value_double(env, args[1], &source) == napi_ok;
    for (const double fd : {directory, source}) {
        valid = valid && std::isfinite(fd) && fd >= 0 && fd <= INT_MAX && std::floor(fd) == fd;
    }
    for (size_t i = 0; valid && i < 2; ++i) {
        size_t length = 0, copied = 0;
        valid = napi_get_value_string_utf8(env, args[i + 2], nullptr, 0, &length) == napi_ok && length > 0 && length <= 255 &&
            napi_get_value_string_utf8(env, args[i + 2], names[i], sizeof(names[i]), &copied) == napi_ok &&
            copied == length && std::strlen(names[i]) == length;
    }
    if (!valid) { napi_throw_error(env, nullptr, "MEDIA_FOLDER_PUBLISH_ARGUMENT"); return nullptr; }
    const int error = PublishFolderFile(static_cast<int>(directory), static_cast<int>(source), names[0], names[1]);
    if (error) {
        return FolderError(env, "MEDIA_FOLDER_PUBLISH", error);
    }
    napi_value result = nullptr;
    napi_get_undefined(env, &result); return result;
}

// Native-owned private staging -> explicitly donated exchange directory.
// Both directory descriptors stay pinned; never accept a Web path or fallback
// to a plain rename/copy when atomic no-overwrite publication is unavailable.
napi_value FolderPublishFrom(napi_env env, napi_callback_info info)
{
    size_t argc = 5;
    napi_value args[5] {};
    double descriptors[3] {-1, -1, -1};
    char names[2][256] {};
    bool valid = napi_get_cb_info(env, info, &argc, args, nullptr, nullptr) == napi_ok && argc == 5;
    for (size_t i = 0; valid && i < 3; ++i) {
        valid = napi_get_value_double(env, args[i], &descriptors[i]) == napi_ok && std::isfinite(descriptors[i]) &&
            descriptors[i] >= 0 && descriptors[i] <= INT_MAX && std::floor(descriptors[i]) == descriptors[i];
    }
    for (size_t i = 0; valid && i < 2; ++i) {
        size_t length = 0, copied = 0;
        valid = napi_get_value_string_utf8(env, args[i + 3], nullptr, 0, &length) == napi_ok && length > 0 && length <= 255 &&
            napi_get_value_string_utf8(env, args[i + 3], names[i], sizeof(names[i]), &copied) == napi_ok &&
            copied == length && std::strlen(names[i]) == length;
    }
    if (!valid) { napi_throw_error(env, nullptr, "MEDIA_FOLDER_PUBLISH_ARGUMENT"); return nullptr; }
    const int error = PublishFolderFileFrom(static_cast<int>(descriptors[0]), static_cast<int>(descriptors[1]),
        static_cast<int>(descriptors[2]), names[0], names[1]);
    if (error) { return FolderError(env, "MEDIA_FOLDER_PUBLISH", error); }
    napi_value result = nullptr; napi_get_undefined(env, &result); return result;
}

// Official URI conversion only. This function grants nothing and performs no
// file open/read/write/delete. Only native-selected documents URIs are accepted.
napi_value FolderUriPath(napi_env env, napi_callback_info info)
{
    size_t argc = 1, length = 0;
    napi_value arg = nullptr;
    if (napi_get_cb_info(env, info, &argc, &arg, nullptr, nullptr) != napi_ok || argc != 1 ||
        napi_get_value_string_utf8(env, arg, nullptr, 0, &length) != napi_ok || length < 13 || length > 8192) {
        napi_throw_error(env, nullptr, "ASSET_FOLDER_URI_INVALID"); return nullptr;
    }
    std::vector<char> buffer(length + 1);
    size_t copied = 0;
    if (napi_get_value_string_utf8(env, arg, buffer.data(), buffer.size(), &copied) != napi_ok || copied != length) {
        napi_throw_error(env, nullptr, "ASSET_FOLDER_URI_INVALID"); return nullptr;
    }
    const std::string uri(buffer.data(), length);
    if (uri.compare(0, 12, "file://docs/") != 0 || uri.find('\0') != std::string::npos ||
        uri.find_first_of("?#\r\n\t") != std::string::npos || !OH_FileUri_IsValidUri(uri.data(), uri.size())) {
        napi_throw_error(env, nullptr, "ASSET_FOLDER_URI_INVALID"); return nullptr;
    }
    char *converted = nullptr;
    const auto code = OH_FileUri_GetPathFromUri(uri.data(), uri.size(), &converted);
    std::unique_ptr<char, decltype(&std::free)> path(converted, &std::free);
    if (code != 0 || !path || path.get()[0] != '/' || strnlen(path.get(), 8193) > 8192) {
        napi_throw_error(env, nullptr, "ASSET_FOLDER_PATH_UNAVAILABLE"); return nullptr;
    }
    napi_value result = nullptr;
    if (napi_create_string_utf8(env, path.get(), NAPI_AUTO_LENGTH, &result) != napi_ok) {
        napi_throw_error(env, nullptr, "ASSET_FOLDER_PATH_UNAVAILABLE"); return nullptr;
    }
    return result;
}

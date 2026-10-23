#include <napi/native_api.h>
#include "authority_lock.h"
#include <libraw/libraw.h>
#include <sys/stat.h>
#include <unistd.h>
#include <cstring>
#include <cerrno>
#include <climits>
#include <cmath>
#include <chrono>
#include <memory>
#include <string>
#include <vector>

// Only a native-owned original FD crosses this adapter. LibRaw supplies RAW processing;
// image display/packing and asset identity stay in the existing native asset adapter.
struct DecodeWork {
    napi_async_work work = nullptr;
    napi_deferred deferred = nullptr;
    int fd = -1;
    int width = 0, height = 0, error = 0;
    std::chrono::steady_clock::time_point deadline;
    std::vector<unsigned char> rgba;
    bool jpeg = false;
    ~DecodeWork() { if (fd >= 0) close(fd); }
};

static void Execute(napi_env, void *data)
{
    auto &w = *static_cast<DecodeWork *>(data);
    try {
        LibRaw raw;
        w.deadline = std::chrono::steady_clock::now() + std::chrono::seconds(90);
        raw.set_progress_handler([](void *data, LibRaw_progress, int, int) {
            return std::chrono::steady_clock::now() >= static_cast<DecodeWork *>(data)->deadline ? 1 : 0;
        }, &w);
        raw.imgdata.params.use_camera_wb = 1;
        raw.imgdata.params.output_bps = 8;
        // Display projection, never a replacement of the original. Half-size Bayer
        // processing keeps camera RAW display within PixelMap's documented memory bound.
        raw.imgdata.params.half_size = 1;
        raw.imgdata.rawparams.max_raw_memory_mb = 512;
        const std::string path = "/proc/self/fd/" + std::to_string(w.fd);
        w.error = raw.open_file(path.c_str());
        if (w.error) return;
        // Prefer the camera's embedded display image. Some computational DNGs
        // contain a black-level-only RAW plane and the real photo in this preview.
        if (raw.unpack_thumb() == LIBRAW_SUCCESS) {
            int result = 0;
            std::unique_ptr<libraw_processed_image_t, decltype(&LibRaw::dcraw_clear_mem)>
                preview(raw.dcraw_make_mem_thumb(&result), &LibRaw::dcraw_clear_mem);
            if (preview && !result && preview->type == LIBRAW_IMAGE_JPEG &&
                preview->data_size > 0 && preview->data_size <= 512ULL * 1024 * 1024) {
                w.rgba.assign(preview->data, preview->data + preview->data_size);
                w.width = raw.imgdata.thumbnail.twidth;
                w.height = raw.imgdata.thumbnail.theight;
                w.jpeg = true;
                return;
            }
        }
        if (!w.error) w.error = raw.unpack();
        if (!w.error) w.error = raw.dcraw_process();
        if (w.error) return;
        int result = 0;
        std::unique_ptr<libraw_processed_image_t, decltype(&LibRaw::dcraw_clear_mem)>
            pixels(raw.dcraw_make_mem_image(&result), &LibRaw::dcraw_clear_mem);
        if (!pixels || result) { w.error = result ? result : LIBRAW_UNSPECIFIED_ERROR; return; }
        const size_t count = size_t(pixels->width) * pixels->height;
        if (pixels->type != LIBRAW_IMAGE_BITMAP || pixels->colors != 3 || pixels->bits != 8 ||
            count == 0 || count > (128ULL * 1024 * 1024) / 4 || pixels->data_size != count * 3) {
            w.error = LIBRAW_UNSPECIFIED_ERROR; return;
        }
        w.width = pixels->width; w.height = pixels->height;
        w.rgba.resize(count * 4);
        for (size_t i = 0; i < count; ++i) {
            memcpy(w.rgba.data() + i * 4, pixels->data + i * 3, 3);
            w.rgba[i * 4 + 3] = 255;
        }
    } catch (...) { w.error = LIBRAW_UNSPECIFIED_ERROR; }
}

static void Complete(napi_env env, napi_status status, void *data)
{
    std::unique_ptr<DecodeWork> w(static_cast<DecodeWork *>(data));
    napi_value result = nullptr, buffer = nullptr, width = nullptr, height = nullptr;
    void *bytes = nullptr;
    bool ok = status == napi_ok && w->error == 0;
    if (ok) ok = napi_create_object(env, &result) == napi_ok &&
        napi_create_arraybuffer(env, w->rgba.size(), &bytes, &buffer) == napi_ok &&
        napi_create_int32(env, w->width, &width) == napi_ok &&
        napi_create_int32(env, w->height, &height) == napi_ok &&
        napi_set_named_property(env, result, "pixels", buffer) == napi_ok &&
        napi_set_named_property(env, result, "width", width) == napi_ok &&
        napi_set_named_property(env, result, "height", height) == napi_ok;
    if (ok && w->jpeg) {
        napi_value mime;
        ok = napi_create_string_utf8(env, "image/jpeg", NAPI_AUTO_LENGTH, &mime) == napi_ok &&
            napi_set_named_property(env, result, "mime", mime) == napi_ok;
    }
    if (ok) {
        memcpy(bytes, w->rgba.data(), w->rgba.size());
        napi_resolve_deferred(env, w->deferred, result);
    } else {
        napi_value message, error;
        const std::string reason = "ASSET_RAW_DECODE_FAILED";
        napi_create_string_utf8(env, reason.c_str(), reason.size(), &message);
        napi_create_error(env, nullptr, message, &error);
        napi_value code;
        napi_create_int32(env, w->error, &code);
        napi_set_named_property(env, error, "code", code);
        napi_reject_deferred(env, w->deferred, error);
    }
    napi_delete_async_work(env, w->work);
}

static napi_value Decode(napi_env env, napi_callback_info info)
{
    size_t argc = 1;
    napi_value args[1], promise, name;
    int fd = -1;
    struct stat statbuf {};
    if (napi_get_cb_info(env, info, &argc, args, nullptr, nullptr) != napi_ok || argc != 1 ||
        napi_get_value_int32(env, args[0], &fd) != napi_ok || fd < 0 ||
        fstat(fd, &statbuf) || !S_ISREG(statbuf.st_mode) || statbuf.st_size <= 0 ||
        statbuf.st_size > 512LL * 1024 * 1024) {
        napi_throw_error(env, nullptr, "ASSET_RAW_FD_INVALID"); return nullptr;
    }
    auto w = std::make_unique<DecodeWork>();
    w->fd = dup(fd);
    if (w->fd < 0 || napi_create_promise(env, &w->deferred, &promise) != napi_ok ||
        napi_create_string_utf8(env, "raw-image", NAPI_AUTO_LENGTH, &name) != napi_ok ||
        napi_create_async_work(env, nullptr, name, Execute, Complete, w.get(), &w->work) != napi_ok) {
        napi_throw_error(env, nullptr, "ASSET_RAW_WORK_FAILED"); return nullptr;
    }
    if (napi_queue_async_work(env, w->work) != napi_ok) {
        napi_delete_async_work(env, w->work);
        napi_throw_error(env, nullptr, "ASSET_RAW_WORK_FAILED"); return nullptr;
    }
    w.release(); return promise;
}

// Read-only metadata of a borrowed native-owned FD. Never opens paths, writes,
// unlinks, or closes the caller's descriptor; SDK transfer ownership is unchanged.
static napi_value Identity(napi_env env, napi_callback_info info)
{
    size_t argc = 1;
    napi_value arg = nullptr;
    double value = -1;
    if (napi_get_cb_info(env, info, &argc, &arg, nullptr, nullptr) != napi_ok || argc != 1 ||
        napi_get_value_double(env, arg, &value) != napi_ok || !std::isfinite(value) ||
        value < 0 || value > INT_MAX || std::floor(value) != value) {
        napi_throw_error(env, "CACHE_IDENTITY_FD", "Expected an owned integer file descriptor");
        return nullptr;
    }
    struct stat st {};
    if (fstat(static_cast<int>(value), &st) != 0) {
        const auto message = "fstat errno=" + std::to_string(errno);
        napi_throw_error(env, "CACHE_IDENTITY_FSTAT", message.c_str());
        return nullptr;
    }
    const std::string json = "{\"dev\":\"" + std::to_string(st.st_dev) +
        "\",\"ino\":\"" + std::to_string(st.st_ino) + "\",\"links\":" +
        std::to_string(st.st_nlink) + ",\"size\":" + std::to_string(st.st_size) +
        ",\"regular\":" + (S_ISREG(st.st_mode) ? "true" : "false") + "}";
    napi_value result = nullptr;
    if (napi_create_string_utf8(env, json.data(), json.size(), &result) != napi_ok) {
        napi_throw_error(env, "CACHE_IDENTITY_RESULT", "Cannot return file identity");
        return nullptr;
    }
    return result;
}
napi_value FolderUriPath(napi_env env, napi_callback_info info);
napi_value FolderPublish(napi_env env, napi_callback_info info);
napi_value FolderPublishFrom(napi_env env, napi_callback_info info);
napi_value FolderOpen(napi_env env, napi_callback_info info);
#include "media_lease.h"
static napi_value MediaLeaseLock(napi_env env, napi_callback_info info)
{
    size_t argc = 3;
    napi_value args[3] {};
    double fd = -1;
    bool acquire = false, exclusive = false;
    if (napi_get_cb_info(env, info, &argc, args, nullptr, nullptr) != napi_ok || argc != 3 ||
        napi_get_value_double(env, args[0], &fd) != napi_ok || !std::isfinite(fd) || fd < 0 || fd > INT_MAX || std::floor(fd) != fd ||
        napi_get_value_bool(env, args[1], &acquire) != napi_ok || napi_get_value_bool(env, args[2], &exclusive) != napi_ok) {
        napi_throw_error(env, nullptr, "MEDIA_LEASE_ARGUMENT"); return nullptr;
    }
    const int error = MediaCacheLeaseLock(static_cast<int>(fd), acquire, exclusive);
    if (error && error != EWOULDBLOCK && error != EAGAIN) {
        const auto message = "MEDIA_LEASE_LOCK errno=" + std::to_string(error);
        napi_throw_error(env, nullptr, message.c_str()); return nullptr;
    }
    napi_value result = nullptr;
    napi_get_boolean(env, error == 0, &result); return result;
}
static napi_value AuthorityLock(napi_env env, napi_callback_info info)
{
    size_t argc = 2;
    napi_value args[2] = {nullptr, nullptr};
    double fd = -1;
    bool acquire = false;
    if (napi_get_cb_info(env, info, &argc, args, nullptr, nullptr) != napi_ok || argc != 2 ||
        napi_get_value_double(env, args[0], &fd) != napi_ok || !std::isfinite(fd) ||
        fd < 0 || fd > INT_MAX || std::floor(fd) != fd ||
        napi_get_value_bool(env, args[1], &acquire) != napi_ok) {
        napi_throw_error(env, nullptr, "ASSET_AUTHORITY_LOCK_ARGUMENT"); return nullptr;
    }
    const int error = AssetAuthorityLock(static_cast<int>(fd), acquire);
    if (error) {
        const auto message = "ASSET_AUTHORITY_LOCK errno=" + std::to_string(error);
        napi_throw_error(env, nullptr, message.c_str()); return nullptr;
    }
    napi_value result = nullptr;
    napi_get_undefined(env, &result); return result;
}
static napi_value Init(napi_env env, napi_value exports)
{
    napi_property_descriptor properties[] = {
        {"decode", nullptr, Decode, nullptr, nullptr, nullptr, napi_default, nullptr},
        {"identity", nullptr, Identity, nullptr, nullptr, nullptr, napi_default, nullptr},
        {"folderUriPath", nullptr, FolderUriPath, nullptr, nullptr, nullptr, napi_default, nullptr},
        {"folderPublish", nullptr, FolderPublish, nullptr, nullptr, nullptr, napi_default, nullptr},
        {"folderPublishFrom", nullptr, FolderPublishFrom, nullptr, nullptr, nullptr, napi_default, nullptr},
        {"folderOpen", nullptr, FolderOpen, nullptr, nullptr, nullptr, napi_default, nullptr},
        {"mediaLeaseLock", nullptr, MediaLeaseLock, nullptr, nullptr, nullptr, napi_default, nullptr},
        {"authorityLock", nullptr, AuthorityLock, nullptr, nullptr, nullptr, napi_default, nullptr}};
    napi_define_properties(env, exports, sizeof(properties) / sizeof(properties[0]), properties); return exports;
}
static napi_module module = {1, 0, nullptr, Init, "begoniaraw", nullptr, {0}};
extern "C" __attribute__((constructor)) void RegisterRawImage() { napi_module_register(&module); }

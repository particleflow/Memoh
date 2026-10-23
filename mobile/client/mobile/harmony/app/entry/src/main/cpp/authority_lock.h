#pragma once
#include <cerrno>
#include <sys/file.h>
#include <sys/stat.h>

// Existing private asset directory inode, unrelated to upload/media lock files.
// No marker/file allocation is needed for revocation, including a full disk.
// Only synchronous
// local check/commit or credential replacement may hold it; never HTTP or SDK awaits.
inline int AssetAuthorityLock(int fd, bool acquire)
{
    struct stat st {};
    if (fstat(fd, &st) != 0) { return errno; }
    if (!S_ISDIR(st.st_mode)) { return EINVAL; }
    int result;
    do { result = flock(fd, acquire ? LOCK_EX : LOCK_UN); } while (result != 0 && errno == EINTR);
    return result == 0 ? 0 : errno;
}

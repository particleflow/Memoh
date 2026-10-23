#pragma once
#include <cerrno>
#include <sys/file.h>
#include <sys/stat.h>

// Existing private media-directory inode only. Shared borrowers protect caches;
// deletion takes a nonblocking exclusive lease. No files, deadlines or queue.
inline int MediaCacheLeaseLock(int fd, bool acquire, bool exclusive)
{
    struct stat value {};
    if (fstat(fd, &value)) { return errno; }
    if (!S_ISDIR(value.st_mode)) { return EINVAL; }
    int result;
    do { result = flock(fd, acquire ? (exclusive ? LOCK_EX : LOCK_SH) | LOCK_NB : LOCK_UN); }
    while (result && errno == EINTR);
    return result ? errno : 0;
}

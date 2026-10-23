#pragma once

#include <cerrno>
#include <cstring>
#include <fcntl.h>
#include <stdio.h>
#include <sys/stat.h>
#include <unistd.h>

inline int OpenFolderFile(int directory, const char *name, int mode)
{
    if (!name || !name[0] || std::strlen(name) > 255 || !std::strcmp(name, ".") || !std::strcmp(name, "..") ||
        std::strchr(name, '/') || std::strchr(name, '\\') || mode < 0 || mode > 2) { errno = EINVAL; return -1; }
    struct stat root {};
    if (fstat(directory, &root)) { return -1; }
    if (!S_ISDIR(root.st_mode)) { errno = ENOTDIR; return -1; }
    const int flags = O_CLOEXEC | O_NOFOLLOW | (mode == 0 ? O_RDONLY : O_RDWR) | (mode == 2 ? O_CREAT | O_EXCL : 0);
    const int fd = openat(directory, name, flags, 0600);
    if (fd < 0) { return -1; }
    struct stat file {};
    if (fstat(fd, &file)) { const int error = errno; close(fd); errno = error; return -1; }
    if (!S_ISREG(file.st_mode) || file.st_nlink != 1) { close(fd); errno = ESTALE; return -1; }
    return fd;
}

// A short final commit for an already-written staging file. No copy, unlink,
// retry or overwrite fallback. The caller owns both descriptors and the
// durable export receipt, and checks authority around this synchronous call.
// Directory FD anchoring prevents a renamed parent from redirecting the move.
// The caller must verify the published inode before acknowledging its receipt;
// this precheck is not an immutable snapshot against external source renames.
inline int PublishFolderFileFrom(int stagingDirectory, int directory, int source, const char *stage, const char *destination)
{
    const auto valid = [](const char *name) {
        return name && name[0] && std::strlen(name) <= 255 &&
            std::strcmp(name, ".") && std::strcmp(name, "..") &&
            !std::strchr(name, '/') && !std::strchr(name, '\\');
    };
    if (!valid(stage) || !valid(destination) || (stagingDirectory == directory && !std::strcmp(stage, destination))) { return EINVAL; }
    struct stat stagingRoot {}, root {}, opened {}, named {};
    if (fstat(stagingDirectory, &stagingRoot) || fstat(directory, &root) || fstat(source, &opened) ||
        fstatat(stagingDirectory, stage, &named, AT_SYMLINK_NOFOLLOW)) { return errno; }
    if (!S_ISDIR(stagingRoot.st_mode) || !S_ISDIR(root.st_mode)) { return ENOTDIR; }
    if (stagingRoot.st_dev != root.st_dev || opened.st_dev != root.st_dev) { return EXDEV; }
    if (!S_ISREG(opened.st_mode) || !S_ISREG(named.st_mode) ||
        opened.st_nlink != 1 || named.st_nlink != 1 || opened.st_dev != named.st_dev ||
        opened.st_ino != named.st_ino || opened.st_size != named.st_size) { return ESTALE; }
    // Unsupported filesystems/kernels fail closed. Plain rename and the SDK
    // access-then-rename move mode cannot provide this no-overwrite guarantee.
    if (renameat2(stagingDirectory, stage, directory, destination, RENAME_NOREPLACE)) { return errno; }
    return 0;
}

inline int PublishFolderFile(int directory, int source, const char *stage, const char *destination)
{
    return PublishFolderFileFrom(directory, directory, source, stage, destination);
}

// Exact new directory-open/publication/cache-lease primitives, own temp root only.
#include "../app/entry/src/main/cpp/folder_publish.h"
#include "../app/entry/src/main/cpp/media_lease.h"
#include <cassert>
#include <sys/wait.h>

int main(int argc, char **argv)
{
    assert(argc == 2);
    const int root = open(argv[1], O_RDONLY | O_DIRECTORY | O_NOFOLLOW); assert(root >= 0);
    int ready[2], finish[2]; assert(pipe(ready) == 0 && pipe(finish) == 0);
    const pid_t viewer = fork(); assert(viewer >= 0);
    if (!viewer) {
        close(root); const int own = open(argv[1], O_RDONLY | O_DIRECTORY | O_NOFOLLOW); assert(own >= 0);
        assert(MediaCacheLeaseLock(own, true, false) == 0);
        assert(write(ready[1], "x", 1) == 1); char message; assert(read(finish[0], &message, 1) == 1);
        _exit(0); // Process close releases its real shared directory lease.
    }
    char message; assert(read(ready[0], &message, 1) == 1);
    const int busy = MediaCacheLeaseLock(root, true, true); assert(busy == EWOULDBLOCK || busy == EAGAIN);
    assert(write(finish[1], "x", 1) == 1); int status; assert(waitpid(viewer, &status, 0) == viewer && WIFEXITED(status) && WEXITSTATUS(status) == 0);
    assert(MediaCacheLeaseLock(root, true, true) == 0 && MediaCacheLeaseLock(root, false, true) == 0);
    const int original = OpenFolderFile(root, "original.bin", 2); assert(original >= 0 && write(original, "keep", 4) == 4);
    assert(OpenFolderFile(root, "original.bin", 2) == -1 && errno == EEXIST);
    assert(symlinkat("original.bin", root, "link.bin") == 0);
    assert(OpenFolderFile(root, "link.bin", 0) == -1 && errno == ELOOP);
    assert(OpenFolderFile(root, "../outside", 2) == -1 && errno == EINVAL);
    const int stage = OpenFolderFile(root, "own.partial", 2); assert(stage >= 0 && write(stage, "data", 4) == 4 && fsync(stage) == 0);
    assert(PublishFolderFile(root, stage, "own.partial", "original.bin") == EEXIST);
    assert(PublishFolderFile(root, stage, "own.partial", "received.bin") == 0 && fsync(root) == 0);
    const int received = OpenFolderFile(root, "received.bin", 0); assert(received >= 0);
    struct stat a {}, b {}; assert(fstat(stage, &a) == 0 && fstat(received, &b) == 0 && a.st_ino == b.st_ino && a.st_dev == b.st_dev);
    char bytes[4]; assert(pread(original, bytes, 4, 0) == 4 && memcmp(bytes, "keep", 4) == 0);
    assert(pread(received, bytes, 4, 0) == 4 && memcmp(bytes, "data", 4) == 0);
    // Retire only these exact successful own-engineering outputs; any failed
    // assertion above leaves inputs and failure state untouched.
    assert(unlinkat(root, "original.bin", 0) == 0 && unlinkat(root, "received.bin", 0) == 0 && unlinkat(root, "link.bin", 0) == 0);
    assert(close(original) == 0 && close(stage) == 0 && close(received) == 0 && close(root) == 0);
    puts("{\"passed\":true,\"liveViewerBlocksCleanup\":true,\"processCloseReleasesLease\":true,\"exclusiveCreatePreservesOriginal\":true,\"symlinkAndEscapeRejected\":true,\"publishedSameInode\":true,\"ownOutputsRetired\":true}");
}

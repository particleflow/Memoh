// Exact new native primitive on own files only; no App/provider qualification.
#include "../app/entry/src/main/cpp/folder_publish.h"
#include <cassert>
#include <string>

static int create(int root, const char *name, const char *bytes)
{
    int fd = openat(root, name, O_RDWR | O_CREAT | O_EXCL | O_NOFOLLOW | O_CLOEXEC, 0600);
    assert(fd >= 0 && write(fd, bytes, strlen(bytes)) == static_cast<ssize_t>(strlen(bytes)) && fsync(fd) == 0);
    return fd;
}
static std::string read(int root, const char *name)
{
    int fd = openat(root, name, O_RDONLY | O_NOFOLLOW); assert(fd >= 0);
    char bytes[32] {}; const auto n = ::read(fd, bytes, sizeof(bytes)); assert(n >= 0 && close(fd) == 0);
    return std::string(bytes, n);
}
int main(int argc, char **argv)
{
    assert(argc == 2);
    const int root = open(argv[1], O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC); assert(root >= 0);
    const int source = create(root, "own.stage", "engineering-original");
    const int prior = create(root, "existing.bin", "preserve-existing");
    assert(PublishFolderFile(root, source, "own.stage", "existing.bin") == EEXIST);
    assert(read(root, "existing.bin") == "preserve-existing" && read(root, "own.stage") == "engineering-original");
    assert(PublishFolderFile(root, source, "own.stage", "../outside") == EINVAL);
    assert(PublishFolderFile(root, prior, "own.stage", "wrong.bin") == ESTALE);
    assert(PublishFolderFile(root, source, "own.stage", "published.bin") == 0);
    struct stat opened {}, named {};
    assert(fstat(source, &opened) == 0 && fstatat(root, "published.bin", &named, AT_SYMLINK_NOFOLLOW) == 0);
    assert(opened.st_dev == named.st_dev && opened.st_ino == named.st_ino && read(root, "published.bin") == "engineering-original");
    assert(faccessat(root, "own.stage", F_OK, 0) != 0 && errno == ENOENT);
    assert(mkdirat(root, "private-stage", 0700) == 0 && mkdirat(root, "visible-exchange", 0700) == 0);
    const int privateRoot = openat(root, "private-stage", O_RDONLY | O_DIRECTORY | O_NOFOLLOW);
    const int publicRoot = openat(root, "visible-exchange", O_RDONLY | O_DIRECTORY | O_NOFOLLOW);
    assert(privateRoot >= 0 && publicRoot >= 0);
    const int privateSource = create(privateRoot, "private.partial", "private-to-public");
    const int keep = create(publicRoot, "keep.bin", "keep-public-original");
    assert(PublishFolderFileFrom(privateRoot, publicRoot, privateSource, "private.partial", "keep.bin") == EEXIST);
    assert(PublishFolderFileFrom(privateRoot, publicRoot, keep, "private.partial", "wrong.bin") == ESTALE);
    assert(PublishFolderFileFrom(privateRoot, publicRoot, privateSource, "private.partial", "../escape") == EINVAL);
    assert(read(publicRoot, "keep.bin") == "keep-public-original");
    assert(faccessat(publicRoot, "private.partial", F_OK, 0) != 0 && errno == ENOENT);
    assert(PublishFolderFileFrom(privateRoot, publicRoot, privateSource, "private.partial", "complete.bin") == 0);
    assert(fsync(privateRoot) == 0 && fsync(publicRoot) == 0);
    assert(fstat(privateSource, &opened) == 0 && fstatat(publicRoot, "complete.bin", &named, AT_SYMLINK_NOFOLLOW) == 0);
    assert(opened.st_dev == named.st_dev && opened.st_ino == named.st_ino && read(publicRoot, "complete.bin") == "private-to-public");
    assert(faccessat(privateRoot, "private.partial", F_OK, 0) != 0 && errno == ENOENT);
    assert(close(privateSource) == 0 && close(keep) == 0 && close(privateRoot) == 0 && close(publicRoot) == 0);
    assert(close(source) == 0 && close(prior) == 0 && close(root) == 0);
    puts("{\"passed\":true,\"existingDestinationPreserved\":true,\"wrongSourceDescriptorRejected\":true,\"publishedInodePreserved\":true,\"outsideNameRejected\":true}");
}

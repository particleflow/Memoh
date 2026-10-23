// Isolated engineering-only executable. Includes the exact production lock primitive.
// No application credentials, real assets, network, or user directories are used.
#include "../app/entry/src/main/cpp/authority_lock.h"
#include <fcntl.h>
#include <poll.h>
#include <sys/wait.h>
#include <unistd.h>
#include <cassert>
#include <cstdio>
#include <cstring>
#include <string>

static void signal(int fd) { assert(write(fd, "x", 1) == 1); }
static bool ready(int fd, int ms) { pollfd p {fd, POLLIN, 0}; return poll(&p, 1, ms) == 1 && (p.revents & POLLIN); }
static void waitSignal(int fd) { assert(ready(fd, 4000)); char c; assert(read(fd, &c, 1) == 1); }
static int owned(const char *path) { int fd = open(path, O_CREAT | O_RDWR | O_NOFOLLOW | O_CLOEXEC, 0600); assert(fd >= 0); return fd; }
static int authority() { int fd = open(".", O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC); assert(fd >= 0); return fd; }
static void put(const char *path, const char *text) { int fd = open(path, O_CREAT | O_TRUNC | O_WRONLY | O_NOFOLLOW, 0600); assert(fd >= 0); assert(write(fd, text, strlen(text)) == static_cast<ssize_t>(strlen(text))); assert(fsync(fd) == 0); close(fd); }
static std::string get(const char *path) { char buf[32] {}; int fd = open(path, O_RDONLY | O_NOFOLLOW); if (fd < 0) { assert(errno == ENOENT); return ""; } auto n = read(fd, buf, sizeof(buf)); assert(n >= 0); close(fd); return std::string(buf, n); }
static void joined(pid_t pid) { int status; assert(waitpid(pid, &status, 0) == pid && WIFEXITED(status) && WEXITSTATUS(status) == 0); }

int main(int argc, char **argv)
{
    assert(argc == 2 && chdir(argv[1]) == 0);
    // Fresh controller-owned directory only; refuse accidental reuse.
    assert(access("epoch", F_OK) != 0 && errno == ENOENT);
    put("epoch", "old"); put("session", "fixed-engineering-session"); put("state", "unchanged"); put("original", "preserve");
    int guard = authority(); struct stat before {}; assert(fstat(guard, &before) == 0);
    assert(AssetAuthorityLock(guard, true) == 0); assert(get("epoch") == "old");
    int attempted[2], completed[2]; assert(pipe(attempted) == 0 && pipe(completed) == 0);
    const pid_t revoke = fork(); assert(revoke >= 0);
    if (revoke == 0) {
        close(guard); // Never reuse the parent's locked open-file description.
        const int own = authority(); signal(attempted[1]);
        assert(AssetAuthorityLock(own, true) == 0);
        assert(get("state") == "committed-before-revoke");
        assert(unlink("epoch") == 0 && unlink("session") == 0);
        put("session", "fixed-engineering-session"); put("epoch", "new");
        assert(AssetAuthorityLock(own, false) == 0); close(own); signal(completed[1]); _exit(0);
    }
    waitSignal(attempted[0]); assert(!ready(completed[0], 100));
    // The entire check/write section precedes revocation; no check/write gap.
    put("state", "committed-before-revoke"); assert(AssetAuthorityLock(guard, false) == 0);
    waitSignal(completed[0]); joined(revoke);
    assert(AssetAuthorityLock(guard, true) == 0);
    const bool staleAccepted = get("epoch") == "old";
    if (staleAccepted) { put("state", "late-write"); unlink("original"); }
    assert(!staleAccepted && get("state") == "committed-before-revoke" && get("original") == "preserve");
    assert(AssetAuthorityLock(guard, false) == 0);

    // Hold the long upload lock as if an HTTP request were still unresolved.
    const int upload = owned("journal.lock"); assert(flock(upload, LOCK_EX) == 0);
    const pid_t logout = fork(); assert(logout >= 0);
    if (logout == 0) {
        close(upload); close(guard); const int own = authority();
        assert(AssetAuthorityLock(own, true) == 0);
        assert(unlink("epoch") == 0 && unlink("session") == 0);
        assert(AssetAuthorityLock(own, false) == 0); close(own); signal(completed[1]); _exit(0);
    }
    waitSignal(completed[0]); joined(logout); // Upload lock is STILL held here.
    assert(get("epoch").empty() && get("session").empty());
    assert(AssetAuthorityLock(guard, true) == 0);
    assert(get("epoch") != "new" && get("original") == "preserve");
    assert(AssetAuthorityLock(guard, false) == 0);
    struct stat after {}; assert(fstat(guard, &after) == 0 && before.st_ino == after.st_ino && S_ISDIR(after.st_mode));
    assert(flock(upload, LOCK_UN) == 0); close(upload); close(guard);
    printf("{\"passed\":true,\"workerPid\":%d,\"replacementPid\":%d,\"logoutPid\":%d,\"blockedUntilLocalCommit\":true,\"sameSessionNewEpochRejectsOldCommit\":true,\"logoutWhileUploadLockHeld\":true,\"staleCleanupRejected\":true,\"stableLockInode\":true}\n", getpid(), revoke, logout);
    return 0;
}

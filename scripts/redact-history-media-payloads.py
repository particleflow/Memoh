#!/usr/bin/env python3
"""One-time cleanup: replace persisted read_media image carriers with reference rows.

Scope (C6/B3, FILE_TRANSFER_INTENT.md Q5): bot_history_messages rows whose
content parts array still carries an inline image payload
(``{"type":"image","image":"<data URL or bare base64>"}``) — the form the
pre-fix read_media persistence path wrote. Every such part is replaced in
place by an identity-only reference row:

    <attachment content_hash="<sha256 hex>" mime="<mime>" size="<bytes>"/>

The content_hash is recomputed from the stored payload itself (sha256 of the
decoded bytes), never hand-copied. A row is rewritten only when every image
part in it has a decodable string payload; rows with any unusable image part
are left untouched and reported as skipped. The mime comes from the part's
mediaType or the data URL header; when neither is present the attribute is
omitted rather than guessed.

This is a ONE-TIME backfill for rows written before the persistence fix. It
is idempotent (a second run finds no image parts and changes nothing), it is
not wired into any server startup path, schedule, or flag, and it must not be
re-added as one. New rounds persist reference rows directly.

Procedure (run in this order, stop on any failure):

  1. Backup   : python3 redact-history-media-payloads.py backup /path/to/backup.sql
                (pg_dump of bot_history_messages; the file must be non-empty)
  2. Dry-run  : python3 redact-history-media-payloads.py dry-run
                (read-only counts: rows to rewrite, rows to skip, payload bytes)
  3. Execute  : python3 redact-history-media-payloads.py execute
                (one transactional UPDATE with the same predicate as the dry run)
  4. Verify   : python3 redact-history-media-payloads.py verify
                (read-only: rows still carrying image parts must equal the
                skip count from step 2; a re-run of dry-run must report 0/0)

What this deliberately does NOT touch:
  * Tool-result rows whose TEXT output merely contains long base64 runs
    (e.g. a `read` of an encoded file) are the model's own replayed tool
    output, not media carriers; they stay as stored.
  * The reference row format matches the server's persistence fix
    (internal/agent/application/service.go, persistedUserImageReference).

Connection defaults match the NAS deployment (container "postgres", user/db
"memoh"); override with DOCKER/PGCONTAINER/PGUSER/PGDB environment variables.
Requires Python 3.8+ (standard library only) and a docker CLI that can exec
into the postgres container.
"""

import os
import subprocess
import sys

DOCKER = os.environ.get("DOCKER", "docker")
PGCONTAINER = os.environ.get("PGCONTAINER", "postgres")
PGUSER = os.environ.get("PGUSER", "memoh")
PGDB = os.environ.get("PGDB", "memoh")

# A decodable carrier part: string payload, optional data:URL prefix, standard
# padded base64 body, length multiple of 4 (PostgreSQL decode() requirement).
# body = the base64 after the optional "data:<mime>;base64," prefix; a bare
# payload (no comma) is its own body.
BODY = (
    "CASE WHEN position(',' IN p->>'image') > 0 "
    "THEN split_part(p->>'image', ',', 2) ELSE p->>'image' END"
)
DECODABLE = (
    "p->>'type' = 'image' AND jsonb_typeof(p->'image') = 'string' "
    "AND (p->>'image') ~ '^(data:[^,]+,)?[A-Za-z0-9+/]+=*$' "
    "AND (length(" + BODY + ") % 4) = 0 "
    "AND length(" + BODY + ") > 0"
)
IMAGE = "p->>'type' = 'image'"
# mime = declared mediaType, else data URL header, else omitted
MIME = (
    "coalesce(nullif(p->>'mediaType',''), "
    "CASE WHEN (p->>'image') ~ '^data:[^,;]+[,;]' "
    "THEN substr(p->>'image', 6, position(';' IN p->>'image') - 6) END)"
)

STATISTICS_SQL = (
    "\\pset pager off\n\\set ON_ERROR_STOP on\n"
    "WITH row_stats AS (\n"
    "  SELECT m.id,\n"
    "         count(*) FILTER (WHERE " + IMAGE + ") AS image_parts,\n"
    "         count(*) FILTER (WHERE " + IMAGE + " AND NOT (" + DECODABLE + ")) AS undecodable,\n"
    "         coalesce(sum(octet_length(p->>'image')) FILTER (WHERE " + DECODABLE + "), 0) AS payload_bytes\n"
    "    FROM bot_history_messages m\n"
    "    CROSS JOIN LATERAL jsonb_array_elements(m.content->'content') p\n"
    "   WHERE jsonb_typeof(m.content->'content') = 'array'\n"
    "   GROUP BY m.id\n"
    ")\n"
    "SELECT count(*) FILTER (WHERE image_parts > 0 AND undecodable = 0) AS rows_to_rewrite,\n"
    "       count(*) FILTER (WHERE image_parts > 0 AND undecodable > 0) AS rows_to_skip,\n"
    "       coalesce(sum(payload_bytes) FILTER (WHERE image_parts > 0 AND undecodable = 0), 0) AS bytes_to_free\n"
    "  FROM row_stats;\n"
)

UPDATE_SQL = (
    "\\set ON_ERROR_STOP on\n"
    "BEGIN;\n"
    "UPDATE bot_history_messages m\n"
    "   SET content = jsonb_set(m.content, '{content}', (\n"
    "         SELECT coalesce(jsonb_agg(\n"
    "           CASE WHEN " + DECODABLE + " THEN jsonb_build_object(\n"
    "                  'type', 'text',\n"
    "                  'text', '<attachment content_hash=\"'\n"
    "                          || encode(sha256(decode(" + BODY + ", 'base64')), 'hex')\n"
    "                          || CASE WHEN " + MIME + " IS NULL THEN ''\n"
    "                                 ELSE '\" mime=\"' || " + MIME + " END\n"
    "                          || '\" size=\"'\n"
    "                          || octet_length(decode(" + BODY + ", 'base64'))\n"
    "                          || '\"/>'\n"
    "                )\n"
    "                ELSE p END\n"
    "           ORDER BY ord), '[]'::jsonb)\n"
    "           FROM jsonb_array_elements(m.content->'content') WITH ORDINALITY AS t(p, ord)\n"
    "       ), false)\n"
    " WHERE jsonb_typeof(m.content->'content') = 'array'\n"
    "   AND EXISTS (SELECT 1 FROM jsonb_array_elements(m.content->'content') p WHERE " + DECODABLE + ")\n"
    "   AND NOT EXISTS (SELECT 1 FROM jsonb_array_elements(m.content->'content') p\n"
    "                    WHERE " + IMAGE + " AND NOT (" + DECODABLE + "));\n"
    "COMMIT;\n"
)

VERIFY_SQL = (
    "\\pset pager off\n\\set ON_ERROR_STOP on\n"
    "SELECT role, count(*) AS rows_still_carrying_image_parts\n"
    "  FROM bot_history_messages m\n"
    " WHERE jsonb_typeof(m.content->'content') = 'array'\n"
    "   AND EXISTS (SELECT 1 FROM jsonb_array_elements(m.content->'content') p WHERE " + IMAGE + ")\n"
    " GROUP BY role;\n"
)


def run_psql(sql):
    # type: (str) -> bytes
    cmd = [DOCKER, "exec", "-i", PGCONTAINER, "psql", "-U", PGUSER, "-d", PGDB]
    proc = subprocess.run(cmd, input=sql.encode("utf-8"), stdout=subprocess.PIPE,
                          stderr=subprocess.PIPE, timeout=600)
    if proc.returncode != 0:
        sys.stderr.write(proc.stderr.decode("utf-8", "replace"))
        raise SystemExit("psql failed with exit code %d" % proc.returncode)
    return proc.stdout.decode("utf-8", "replace")


def cmd_backup(dest):
    # type: (str) -> None
    if os.path.exists(dest):
        raise SystemExit("refusing to overwrite existing backup: %s" % dest)
    with open(dest, "wb") as fh:
        proc = subprocess.run(
            [DOCKER, "exec", PGCONTAINER, "pg_dump", "-U", PGUSER,
             "--table=bot_history_messages", PGDB],
            stdout=fh, stderr=subprocess.PIPE, timeout=1800)
    if proc.returncode != 0 or os.path.getsize(dest) == 0:
        raise SystemExit("pg_dump failed: %s" % proc.stderr.decode("utf-8", "replace"))
    print("backup written: %s (%d bytes)" % (dest, os.path.getsize(dest)))


def parse_counts(out):
    # type: (str) -> tuple
    for line in out.splitlines():
        fields = [f.strip().replace(",", "") for f in line.split("|")]
        if len(fields) == 3 and all(f.isdigit() for f in fields):
            return int(fields[0]), int(fields[1]), int(fields[2])
    sys.stderr.write(out)
    raise SystemExit("could not parse counts from psql output")


def cmd_dry_run():
    # type: () -> None
    rewrite, skip, freed = parse_counts(run_psql(STATISTICS_SQL))
    print("rows to rewrite: %d" % rewrite)
    print("rows to skip (undecodable image part, left untouched): %d" % skip)
    print("bytes of payload to remove: %d" % freed)


def cmd_execute():
    # type: () -> None
    rewrite, skip, _ = parse_counts(run_psql(STATISTICS_SQL))
    print("planned: rewrite=%d skip=%d" % (rewrite, skip))
    if rewrite == 0:
        print("nothing to do (already clean)")
        return
    out = run_psql(UPDATE_SQL)
    for line in out.strip().splitlines():
        if line.startswith("UPDATE"):
            print(line)
    verify = run_psql(VERIFY_SQL)
    print("rows still carrying image parts after execute (expected = skip count %d):" % skip)
    for line in verify.splitlines():
        if "|" in line and "rows_still" not in line and "---" not in line and "role" not in line:
            print("  " + line.strip())


def cmd_verify():
    # type: () -> None
    print(run_psql(VERIFY_SQL).strip())


def main():
    commands = {
        "backup": lambda: cmd_backup(sys.argv[2]),
        "dry-run": cmd_dry_run,
        "execute": cmd_execute,
        "verify": cmd_verify,
    }
    if len(sys.argv) < 2 or sys.argv[1] not in commands or (sys.argv[1] == "backup" and len(sys.argv) < 3):
        sys.exit(__doc__)
    commands[sys.argv[1]]()


if __name__ == "__main__":
    main()

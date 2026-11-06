// Host-side continuity guard for Pi 1.2.0 session v3, not a Pi replacement.
// Source contract: earendil-works/pi@06a466935536f8c9bcacb2d5efc0fc021be1bdff.
// Never writes session files. All writes stay in this exclusive operation attempt.
'use strict';
(async () => {
  const [{ default: fs }, { default: path }, { default: crypto }, { TextDecoder }] = await Promise.all([
    import('node:fs'), import('node:path'), import('node:crypto'), import('node:util'),
  ]);
  const base = __dirname;
  const context = JSON.parse(fs.readFileSync(path.join(base, 'history-context.json'), 'utf8'));
  const [stage, dir, started] = process.argv.slice(2);
  const max = 64 * 1024 * 1024;
  const fail = status => { throw Object.assign(new Error(status), { status }); };
  const sha = b => crypto.createHash('sha256').update(b).digest('hex');
  const filename = s => typeof s === 'string' && /^[A-Za-z0-9_-]{1,180}\.jsonl$/.test(s);
  const types = new Set(['message', 'thinking_level_change', 'model_change', 'usage', 'compaction', 'branch_summary', 'custom', 'label', 'session_info', 'custom_message', 'context_edit']);
  function inspect(file, expected) {
    if (!filename(file)) fail('unconfirmed');
    const full = path.join(dir, file);
    const stat = fs.lstatSync(full);
    if (!stat.isFile() || stat.isSymbolicLink()) fail('unconfirmed');
    if (!stat.size) fail('corrupt');
    if (stat.size > max) fail('unconfirmed');
    const raw = fs.readFileSync(full);
    if (raw.length !== stat.size) fail('changed');
    if (expected && (raw.length < expected.size || sha(raw.subarray(0, expected.size)) !== expected.sha256)) fail('changed');
    // Pi skips malformed JSON lines; this host must not silently accept them.
    let entries;
    try {
      const text = new TextDecoder('utf-8', { fatal: true }).decode(raw);
      if (!text.endsWith('\n')) fail('corrupt');
      entries = text.slice(0, -1).split('\n').map(line => JSON.parse(line));
    } catch { fail('corrupt'); }
    const header = entries.shift();
    if (!header || header.type !== 'session' || header.id !== context.id || typeof header.cwd !== 'string' || !header.cwd || !Number.isFinite(Date.parse(header.timestamp))) fail('corrupt');
    // Unknown/older formats require explicit review, never an implicit migration.
    if (header.version !== 3) fail('unconfirmed');
    const ids = new Map();
    let conversation = false;
    for (const e of entries) {
      if (!e || typeof e.id !== 'string' || !e.id || ids.has(e.id) || !Number.isFinite(Date.parse(e.timestamp)) || (e.parentId !== null && !ids.has(e.parentId))) fail('corrupt');
      if (!types.has(e.type)) fail('unconfirmed');
      if (e.type === 'message') {
        if (!e.message || typeof e.message.role !== 'string' || !e.message.role) fail('corrupt');
        conversation ||= ['user', 'assistant'].includes(e.message.role);
      }
      // appendCompaction(null) uses its own ID when no earlier entries are kept.
      if (e.type === 'compaction') {
        if (typeof e.summary !== 'string' || typeof e.firstKeptEntryId !== 'string' || !Number.isFinite(e.tokensBefore)) fail('corrupt');
        if (e.firstKeptEntryId !== e.id) {
          let kept = ids.get(e.parentId);
          while (kept && kept.id !== e.firstKeptEntryId) kept = ids.get(kept.parentId);
          if (!kept) fail('unconfirmed'); // Not a retained point on this branch.
        }
      }
      if (e.type === 'branch_summary') {
        if (typeof e.summary !== 'string' || typeof e.fromId !== 'string' || !e.fromId) fail('corrupt');
        // branchWithSummary() uses "root" after resetLeaf(). A fork may refer to
        // an omitted source branch: preserve it, but do not confirm that history.
        if (e.fromId !== 'root' && !ids.has(e.fromId)) fail('unconfirmed');
      }
      if (e.type === 'context_edit' && (!ids.has(e.targetId) || !(e.replacement === null || e.replacement && 'content' in e.replacement))) fail('corrupt');
      ids.set(e.id, e);
    }
    // Pi flushes on the first user OR assistant message, including an unfinished
    // first turn. A valid setup-only file is unconfirmed, not corrupt.
    if (!conversation) fail('unconfirmed');
    // On reload Pi selects the last entry, then follows parentId. Conversation
    // entries on an abandoned branch alone cannot validate the selected branch.
    let active = entries.at(-1), supportedBranch = false;
    while (active) {
      supportedBranch ||= active.type === 'message' && ['user', 'assistant'].includes(active.message.role)
        || ['branch_summary', 'compaction'].includes(active.type) && !!active.summary;
      active = ids.get(active.parentId);
    }
    if (!supportedBranch) fail('unconfirmed');
    // This verifies file bytes and supported branch structure, not Pi's complete
    // model-context projection: compaction, branching and context edits can omit
    // old messages without removing their bytes from the session file.
    return { file, size: raw.length, sha256: sha(raw) };
  }
  function names() {
    const stat = fs.lstatSync(dir);
    if (!stat.isDirectory() || stat.isSymbolicLink()) fail('unconfirmed');
    return fs.readdirSync(dir).filter(n => n !== 'lock');
  }
  function statusOf(error) {
    if (error.status) return error.status;
    if (error.code === 'ENOENT') return 'missing';
    if (['EACCES', 'EPERM', 'EIO'].includes(error.code)) return 'unreadable';
    return 'unconfirmed';
  }
  const reportFile = path.join(base, 'history-report.json');
  function save(report) { fs.writeFileSync(reportFile, JSON.stringify(report), { flag: 'w', mode: 0o600 }); }
  if (stage === 'before') {
    let report;
    try {
      const h = context.history;
      if (h.version !== 1) fail('unconfirmed');
      const files = names();
      if (h.mode === 'first') {
        if (files.length) fail('unconfirmed');
        report = { before: 'new', after: 'not_started' };
      } else if (h.mode === 'resume') {
        if (!h.evidence || files.length !== 1 || files[0] !== h.evidence.file) {
          if (!files.includes(h.evidence?.file)) fail('missing');
          fail('unconfirmed');
        }
        const checkpoint = inspect(h.evidence.file, h.evidence);
        // A second snapshot anchors all bytes observed immediately before Pi.
        fs.writeFileSync(path.join(base, 'history-before.json'), JSON.stringify(checkpoint), { flag: 'wx', mode: 0o600 });
        fs.writeFileSync(path.join(base, 'history-file.txt'), checkpoint.file, { flag: 'wx', mode: 0o600 });
        report = { before: 'verified', after: 'not_started' };
      } else fail('unconfirmed');
    } catch (error) { report = { before: statusOf(error), after: 'not_started' }; }
    save(report);
    process.exitCode = ['new', 'verified'].includes(report.before) ? 0 : 1;
  } else if (stage === 'after') {
    const report = JSON.parse(fs.readFileSync(reportFile, 'utf8'));
    if (started === 'true') {
      try {
        const files = names();
        const expected = report.before === 'verified' ? JSON.parse(fs.readFileSync(path.join(base, 'history-before.json'), 'utf8')) : null;
        if (!files.length || expected && !files.includes(expected.file)) fail('missing');
        if (files.length !== 1) fail('unconfirmed');
        report.evidence = inspect(files[0], expected);
        report.after = expected ? 'verified' : 'created';
      } catch (error) { report.after = statusOf(error); delete report.evidence; }
    }
    save(report);
  } else process.exitCode = 1;
})();

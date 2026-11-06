"""Compare only literal Harmony overlay anchor counts against the server baseline.
This is a read-only source compatibility check, not a native build or device test.
"""
import ast
import json
from pathlib import Path
import subprocess

BASELINE = '565c0b4b3386d87d057234efe6828479d35fcee0'
HARMONY = 'd213ee7842fc76527de712cd5b2d825a5697a613'
BUILDER = 'mobile/client/mobile/build_memoh_web.py'
FILES = [
    'apps/web/src/pages/home/components/chat-pane.vue',
    'apps/web/src/pages/home/components/message-item.vue',
    'apps/web/src/pages/home/components/message-actions.vue',
    'apps/web/src/style.css',
    *[f'apps/web/src/i18n/locales/{locale}.json' for locale in ('en', 'zh', 'ja')],
]

def show(ref, path):
    return subprocess.check_output(['git', 'show', f'{ref}:{path}'], text=True)

baseline = {path: show(BASELINE, path) for path in FILES}
candidate = {path: Path(path).read_text() for path in FILES}
checks = []
for node in ast.walk(ast.parse(show(HARMONY, BUILDER))):
    if not isinstance(node, ast.Dict):
        continue
    for key, values in zip(node.keys, node.values):
        if not isinstance(key, ast.Constant) or key.value not in FILES or not isinstance(values, ast.List):
            continue
        for entry in values.elts:
            if not isinstance(entry, (ast.Tuple, ast.List)) or not entry.elts:
                continue
            first = entry.elts[0]
            if not isinstance(first, ast.Constant) or not isinstance(first.value, str):
                continue
            checks.append(dict(file=key.value, line=entry.lineno, anchor=first.value[:140],
                               baseline_count=baseline[key.value].count(first.value),
                               candidate_count=candidate[key.value].count(first.value)))
result = dict(native_source=HARMONY, baseline=BASELINE, anchors=len(checks),
              changed_counts=[item for item in checks if item['baseline_count'] != item['candidate_count']],
              checks=checks)
print(json.dumps(result, ensure_ascii=False, indent=2))
if result['changed_counts']:
    raise SystemExit(1)

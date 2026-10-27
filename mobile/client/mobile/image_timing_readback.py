"""Filter the app's transient onDump output; never save or echo a raw device dump."""
import json
import math
import sys

FIELDS = ('id', 'card', 'request', 'headers', 'responseStart', 'responseEnd', 'encodedBodySize', 'transferSize', 'resourceMatch', 'body', 'bytes',
          'hashStart', 'hashEnd', 'blob', 'load', 'decodeStart', 'decodeEnd', 'render', 'error')


def sanitize(text):
    # The official service wrapper can print metadata before the app's onDump line.
    # Keep only our bounded numeric payload, not the surrounding device metadata.
    for line in text.splitlines():
        marker = 'GarnetImageTiming:'
        if marker not in line:
            continue
        source = json.loads(line.split(marker, 1)[1])
        if not isinstance(source, dict) or source.get('version') != 1:
            raise ValueError('IMAGE_TIMING_SCHEMA')
        rows = source.get('rows')
        if not isinstance(rows, list) or len(rows) > 5:
            raise ValueError('IMAGE_TIMING_SCHEMA')
        safe = []
        for item in rows:
            if not isinstance(item, dict) or type(item.get('id')) is not int or not 1 <= item['id'] <= 5:
                raise ValueError('IMAGE_TIMING_SCHEMA')
            safe.append({key: item[key] for key in FIELDS if key in item
                         and type(item[key]) in (int, float) and math.isfinite(item[key]) and item[key] >= 0
                         and (key not in ('encodedBodySize', 'transferSize', 'responseStart', 'responseEnd') or item[key] > 0)
                         and (key != 'resourceMatch' or item[key] in (1, 2))})
        return {'version': 1, 'active': source.get('active') is True,
                'paintSupported': source.get('paintSupported') is True, 'rows': safe}
    raise ValueError('IMAGE_TIMING_NOT_AVAILABLE')


if __name__ == '__main__':
    try:
        # Bounded input; no path, URL, credentials or device control arguments.
        text = sys.stdin.read(65537)
        if len(text) > 65536:
            raise ValueError('IMAGE_TIMING_INPUT_LIMIT')
        print(json.dumps(sanitize(text), separators=(',', ':'), allow_nan=False))
    except (ValueError, TypeError, KeyError):
        print('IMAGE_TIMING_UNAVAILABLE_OR_INVALID', file=sys.stderr)
        sys.exit(1)

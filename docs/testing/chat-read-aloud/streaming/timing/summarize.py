"""Offline metadata-only report. No requests, synthesis or audio replay."""
import json
from pathlib import Path
root = Path(__file__).resolve().parent
current = json.loads((root / 'evidence/native-timing.json').read_text())
old = json.loads((root / '../evidence/native-result.json').read_text())
rows = []
for item in current['results']:
    request = item['requestStartMS']
    relative = {key: round(item[key] - request, 3) if item[key] is not None else None for key in ['clientFirstBytesMS', 'firstAppendStartMS', 'firstAppendDoneMS', 'nativePlayingMS', 'firstNonzeroGraphMS', 'eofMS']}
    rows.append({'profile': item['profile'], 'browserClockRelativeToRequestMS': relative,
                 'operationStartToGraphMS': round(item['firstNonzeroGraphMS'], 3),
                 'buttonToOperationStartMS': round(item['triggerToOperationMS'], 3),
                 'serverOwnClockToAdapterFirstAudioMS': item['serverAdapterFirstAudioMS'],
                 'firstTextObservedMS': round(item['firstTextObservedMS'], 3), 'firstTextChunkMS': round(item['firstTextChunkMS'], 3),
                 'firstTextToChunkMS': round(item['firstTextChunkMS'] - item['firstTextObservedMS'], 3),
                 'firstBytesToPlayingMS': round(item['nativePlayingMS'] - item['clientFirstBytesMS'], 3),
                 'firstBytesToEOFMS': round(item['eofMS'] - item['clientFirstBytesMS'], 3),
                 'graphDeltaFrom500MS': round(item['requestToNonzeroGraphMS'] - 500, 3),
                 'softwareGraphComparedTo500': 'OVER_500' if item['requestToNonzeroGraphMS'] > 500 else 'UNDER_500',
                 'physicalAcoustic500': 'UNMEASURED', 'branch': item['branch']})
legacy = []
for mode in ['manual', 'automatic']:
    call = next(item['ms'] for item in old['calls'] if item['mode'] == mode)
    byte = next(item['ms'] for item in old['metrics'] if item['mode'] == mode and item['kind'] == 'firstFixtureBytes')
    graph = next(item['ms'] for item in old['metrics'] if item['mode'] == mode and item['kind'] == 'firstNonzeroDecodedSamples')
    playing = next(item['ms'] for item in old['states'] if item.get('mode') == mode and item['status'] == 'playing')
    legacy.append({'mode': mode, 'oldFixtureTriggerToSynthesizeMS': round(call, 3), 'oldFixtureTriggerToBytesMS': round(byte, 3), 'oldFixtureTriggerToPlayingMS': round(playing, 3), 'oldFixtureTriggerToGraphMS': round(graph, 3), 'synthesizeToGraphMS': round(graph-call, 3), 'firstAppendMS': None, 'eofMS': None, 'physicalAcoustic500': 'UNMEASURED'})
report = {'legacy': legacy, 'current': rows, 'f523': {'beforeEOFBytes': 8403, 'timingsMS': None, 'reason': 'Ordering/byte/state evidence only; no millisecond samples captured'}, 'historicalField200': {'totalServerRequestMS': 8460, 'firstByteMS': None, 'acousticMS': None, 'provenance': 'Parent-provided existing field log; total duration cannot be substituted for onset'}, 'realPhoneRequestToAcoustic': {'status': 'UNMEASURED', 'deltaFrom500MS': None}, 'singleRunNoP95OrGuarantee': True}
(root / 'evidence/measurement-summary.json').write_text(json.dumps(report, indent=2)+'\n')
lines = ['| Controlled profile | Server first chunk* | Client first bytes | Append done | playing | Nonzero graph | EOF | Graph − 500ms |', '|---|---:|---:|---:|---:|---:|---:|---:|']
for row in rows:
    r = row['browserClockRelativeToRequestMS']
    value = lambda x: 'n/a' if x is None else f'{x:.1f}'
    lines.append('| '+row['profile']+' | '+value(row['serverOwnClockToAdapterFirstAudioMS'])+' | '+ ' | '.join(value(r[k]) for k in ['clientFirstBytesMS','firstAppendDoneMS','nativePlayingMS','firstNonzeroGraphMS','eofMS'])+' | '+f"{row['graphDeltaFrom500MS']:+.1f}"+' |')
lines += ['', '*Server own handler clock. Every other numeric event column is relative to the browser logical synthesis request start. Not acoustic onset.']
(root / 'evidence/measurement-table.md').write_text('\n'.join(lines)+'\n')

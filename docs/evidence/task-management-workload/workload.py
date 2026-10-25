"""Finite test workload, not Herdr/Pi. Writes only its own synthetic log."""
import hashlib
import sys
import time
from pathlib import Path

label = sys.argv[1]
assert label in ('accepted', 'unknown')
root = Path(__file__).parent
print('WORKLOAD ' + label + ' START', flush=True)
# Accepted input returns before the first workload ends. The second process
# remains alive across the owned Memoh Server restart, bounded to 60 seconds.
deadline = time.monotonic() + (3 if label == 'accepted' else 60)
while time.monotonic() < deadline and not (root / 'release-unknown').exists():
    time.sleep(.1)
print('WORKLOAD ' + label + ' SHA256 ' + hashlib.sha256(('synthetic-' + label).encode()).hexdigest(), flush=True)
print('WORKLOAD ' + label + ' EXIT 0', flush=True)

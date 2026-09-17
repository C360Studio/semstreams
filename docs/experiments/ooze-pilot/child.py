#!/usr/bin/env python3
"""Capture one actual Ooze candidate, then run the explicitly selected command."""
import difflib
import json
import os
from pathlib import Path
import subprocess
import sys
import time

def write_meta(path, value):
    temporary = path.with_suffix('.tmp')
    temporary.write_text(json.dumps(value, indent=2)+'\n')
    temporary.replace(path)


out = Path(os.environ['PILOT_RUN'])
index = len(list(out.glob('candidate-*'))) + 1
candidate = out / ('candidate-%02d' % index)
candidate.mkdir()
target = Path(os.environ['PILOT_TARGET'])
original = (Path(os.environ['PILOT_SUBJECT']) / target).read_text()
mutant = target.read_text()
(candidate / 'mutant.go.txt').write_text(mutant)
(candidate / 'mutation.patch').write_text(''.join(difflib.unified_diff(
    original.splitlines(True), mutant.splitlines(True), fromfile=str(target), tofile=str(target))))
meta = {'pid': os.getpid(), 'pgid': os.getpgrp(), 'cwd': os.getcwd(), 'target': str(target)}
write_meta(candidate / 'process.json', meta)
mode = os.environ.get('PILOT_CHILD_MODE', 'test')
if mode == 'exit':
    print('CALIBRATION: non-test command exits 23', flush=True)
    sys.exit(23)
if mode == 'sentinel':
    Path('sentinel.txt').write_text('changed through unmutated symlink\n')
    print('CALIBRATION: wrote sentinel; target is symlink=%s sentinel is symlink=%s' %
          (target.is_symlink(), Path('sentinel.txt').is_symlink()), flush=True)
if mode in ('block', 'orphan'):
    child = subprocess.Popen([sys.executable, '-c', 'import time; time.sleep(120)'])
    meta['descendant_pid'] = child.pid
    write_meta(candidate / 'process.json', meta)
    print('CALIBRATION: blocking owned descendant %d' % child.pid, flush=True)
    if mode == 'orphan':
        sys.exit(0)
    time.sleep(120)
    sys.exit(0)
command = json.loads(os.environ['PILOT_TEST_COMMAND'])
meta['command'] = command
start = time.monotonic()
result = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=45)
meta.update(returncode=result.returncode, seconds=round(time.monotonic()-start, 3))
write_meta(candidate / 'process.json', meta)
(candidate / 'test.log').write_bytes(result.stdout)
sys.stdout.buffer.write(result.stdout)
sys.exit(result.returncode)

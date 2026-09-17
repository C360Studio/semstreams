#!/usr/bin/env python3
"""Bounded, serial one-off pilot. Never mutates the source checkout.

Run with Python 3 from any directory. Optional output directory is positional.
All source inputs come from BASE, not the current worktree. Runtime copies are
retained at the recorded /tmp path for inspection; only owned processes are killed.
"""
import hashlib
import json
import os
import re
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import tempfile
import time

HERE = Path(__file__).resolve().parent
REPO = HERE.parents[2]
BASE = '84fc01e46c104d890bffe32b0046e72f006df454'
OUT = Path(sys.argv[1]).resolve() if len(sys.argv) > 1 else HERE / 'evidence'
OUT.mkdir(exist_ok=False)
RUNTIME = Path(tempfile.mkdtemp(prefix='semstreams-ooze-pilot-'))
ENV = dict(os.environ, GOMAXPROCS='2', GOFLAGS='-p=2', GOTOOLCHAIN='local')
SUMMARY = []


def write(path, value):
    path.write_text(json.dumps(value, indent=2, sort_keys=True)+'\n')


def hashes(root):
    return {str(p.relative_to(root)): hashlib.sha256(p.read_bytes()).hexdigest()
            for p in sorted(root.rglob('*')) if p.is_file()}


def group_members(groups):
    rows = subprocess.check_output(['ps','-axo','pid=,pgid=,stat=,comm=']).decode().splitlines()
    members = []
    for row in rows:
        fields = row.split(None,3)
        if len(fields)<4: continue
        pid, pgid, state, executable = fields
        if int(pgid) in groups and not state.startswith('Z'):
            members.append({'pid':int(pid),'pgid':int(pgid),'state':state,'executable':executable})
    return members


def clean_groups(groups):
    before = group_members(groups)
    for pgid in sorted({item['pgid'] for item in before}):
        try: os.killpg(pgid,signal.SIGKILL)
        except ProcessLookupError: pass
    stop = time.monotonic()+3
    after = group_members(groups)
    while after and time.monotonic()<stop:
        time.sleep(0.1)
        after = group_members(groups)
    return {'groups':sorted(groups),'before_owned_cleanup':before,'after_owned_cleanup':after}


def command(name, args, cwd, env=ENV, timeout=60):
    start = time.monotonic()
    expired = False
    with (OUT/(name+'.log')).open('wb') as log:
        process = subprocess.Popen(args,cwd=cwd,env=env,stdout=log,
                                   stderr=subprocess.STDOUT,start_new_session=True)
        try:
            process.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            expired = True
        finally:
            cleanup = clean_groups({process.pid})
            process.wait(timeout=5)
    record = {'name':name,'command':args,'cwd':str(cwd),'returncode':process.returncode,
              'seconds':round(time.monotonic()-start,3),'deadline_seconds':timeout,
              'deadline_expired':expired,'process_group_cleanup':cleanup}
    SUMMARY.append(record)
    write(OUT/'commands.json',SUMMARY)
    print(name,process.returncode,record['seconds'],flush=True)
    if cleanup['after_owned_cleanup']: raise RuntimeError('owned command group survived cleanup')
    if expired: raise RuntimeError('bounded command deadline exceeded; stop pilot')
    return process


def alive(pid):
    result = subprocess.run(['ps', '-o', 'stat=', '-p', str(pid)], stdout=subprocess.PIPE)
    return result.returncode == 0 and result.stdout.strip() and not result.stdout.strip().startswith(b'Z')


def processes(run):
    result = []
    for path in sorted(run.glob('candidate-*/process.json')):
        record = json.loads(path.read_text())
        for field in ('pid', 'descendant_pid'):
            if field in record:
                result.append({'pid': record[field], 'field': field, 'alive': bool(alive(record[field])),
                               'pgid': record['pgid']})
    return result


def run_ooze(name, subject, target, operator='comparison', selector='.', mode='test', timeout='10s',
             cancel=False, missing=False, deadline=90, interrupt_driver=False):
    run = OUT / name
    run.mkdir()
    temp = RUNTIME / (name+'-tmp')
    temp.mkdir()
    test_command = ['go', 'test', '-json', '-count=1', '-parallel=1', '-timeout='+timeout,
                    '-run='+selector, './...']
    if target.startswith('pkg/'):
        test_command = test_command[:-1] + ['./pkg/types', '-rapid.seed=1318', '-rapid.nofailfile']
    # RE2 has no lookahead. Explicit filenames are compiled from the source set.
    ignore = '^(' + '|'.join(re.escape(str(p.relative_to(subject)))
                           for p in sorted(subject.rglob('*.go'))
                           if str(p.relative_to(subject)) != target and not p.name.endswith('_test.go')) + ')$'
    if ignore == '^()$': ignore = '^never-matches$'
    env = dict(ENV, PILOT_SUBJECT=str(subject), PILOT_TARGET=target, PILOT_OPERATOR=operator,
               PILOT_COMMAND='missing-ooze-pilot-executable' if missing else sys.executable+' '+str(HERE/'child.py'),
               PILOT_IGNORE=ignore, PILOT_RUN=str(run), PILOT_CHILD_MODE=mode,
               PILOT_TEST_COMMAND=json.dumps(test_command), TMPDIR=str(temp))
    if mode == 'test' and not missing:
        command(name+'-before', test_command, subject, env=env)
    before = hashes(subject)
    args = [str(RUNTIME/'pilot.test'), '-test.v', '-test.run=^TestOoze$', '-test.timeout=18m']
    write(run/'manifest.json', {'command': args, 'test_command': test_command, 'operator': operator,
          'ignore_regex': ignore, 'subject': str(subject), 'target': target, 'source_before': before,
          'mode': mode, 'deadline_seconds': deadline, 'cancel': cancel, 'missing_command': missing, 'interrupt_driver':interrupt_driver,
          'environment': {k:env[k] for k in ('GOMAXPROCS','GOFLAGS','GOTOOLCHAIN','TMPDIR','PILOT_COMMAND')}})
    start = time.monotonic()
    intervention = None
    process = None
    failure = None
    try:
        with (run/'ooze.log').open('wb') as log:
            process = subprocess.Popen(args, cwd=HERE/'harness', env=env, stdout=log,
                                       stderr=subprocess.STDOUT, start_new_session=True)
            while process.poll() is None:
                elapsed = time.monotonic()-start
                ready = any('descendant_pid' in json.loads(p.read_text())
                            for p in run.glob('candidate-*/process.json'))
                if interrupt_driver and ready:
                    intervention = 'SIGINT to Python pilot driver (finally-path calibration)'
                    write(run/'driver-signal.json',{'pid':os.getpid(),'signal':'SIGINT'})
                    os.kill(os.getpid(),signal.SIGINT)
                if cancel and ready and intervention is None:
                    process.send_signal(signal.SIGINT)
                    intervention = 'SIGINT to pilot test PID (ordinary cancellation probe)'
                if elapsed > deadline:
                    os.killpg(process.pid, signal.SIGKILL)
                    intervention = 'outer deadline SIGKILL pilot process group'
                    break
                time.sleep(0.1)
            process.wait(timeout=5)
    except BaseException as error:
        failure = repr(error)
        raise
    finally:
        before_cleanup = processes(run)
        groups = {item['pgid'] for item in before_cleanup}
        if process is not None: groups.add(process.pid)
        group_cleanup = clean_groups(groups)
        if process is not None: process.wait(timeout=5)
        after_cleanup = processes(run)
        leftovers = sorted(str(p) for p in temp.glob('ooze-*'))
        after = hashes(subject)
        changed = sorted(key for key in set(before)|set(after) if before.get(key)!=after.get(key))
        expected_sentinel_write = (mode=='sentinel' and changed==['sentinel.txt'] and
            (subject/'sentinel.txt').read_text()=='changed through unmutated symlink\n')
        result = {'returncode':process.returncode if process else None,
                  'seconds':round(time.monotonic()-start,3),'exception':failure,
                  'intervention':intervention,'process_group_cleanup':group_cleanup,
                  'processes_before_owned_cleanup':before_cleanup,
                  'processes_after_owned_cleanup':after_cleanup,
                  'temporary_dirs_before_owned_cleanup':leftovers,
                  'source_after':after,'source_unchanged':before==after,
                  'changed_files':changed,'expected_sentinel_write':expected_sentinel_write}
        # Never remove a candidate's temporary files while any owned group remains live.
        if group_cleanup['after_owned_cleanup']:
            write(run/'cleanup-failed.json',result)
            raise RuntimeError('owned candidate group survived cleanup; retain temporary files')
        for path in leftovers: shutil.rmtree(path)
        result['temporary_dirs_after_owned_cleanup'] = sorted(str(p) for p in temp.glob('ooze-*'))
        write(run/'result.json',result)
        if not result['source_unchanged'] and not expected_sentinel_write:
            raise RuntimeError('unexpected disposable source drift; stop pilot')
    if mode == 'test' and not missing:
        command(name+'-after', test_command, subject, env=env)
    print(name, process.returncode, result['seconds'], 'source_unchanged', result['source_unchanged'], flush=True)
    return run


runner_source = OUT/'runner-source'
runner_source.mkdir()
runner_hashes = {}
for name in ('run.py','child.py','harness/pilot_test.go','harness/go.mod','harness/go.sum'):
    destination = runner_source/name
    destination.parent.mkdir(exist_ok=True)
    destination.write_bytes((HERE/name).read_bytes())
    runner_hashes[name] = hashlib.sha256(destination.read_bytes()).hexdigest()
write(runner_source/'sha256.json',runner_hashes)

files = subprocess.check_output(['git', 'ls-tree', '-r', '--name-only', BASE, '--',
             'go.mod', 'go.sum', 'pkg/types', 'pkg/errs', 'pkg/retry'], cwd=REPO).decode().splitlines()
subject = RUNTIME/'subject'
for file in files:
    dest = subject/file
    dest.parent.mkdir(parents=True, exist_ok=True)
    dest.write_bytes(subprocess.check_output(['git', 'show', BASE+':'+file], cwd=REPO))
write(OUT/'source-manifest.json', {'base': BASE, 'files': hashes(subject), 'runtime': str(RUNTIME),
      'environment': {k: ENV[k] for k in ('GOMAXPROCS','GOFLAGS','GOTOOLCHAIN')},
      'go_version': subprocess.check_output(['go','version'], env=ENV).decode().strip(),
      'copied_files_match_git': True})
for name in ('go.mod','go.sum'):
    shutil.copyfile(subject/name, OUT/('subject-'+name+'.txt'))
if command('harness-dependencies', ['go','mod','tidy'], HERE/'harness', timeout=180).returncode:
    sys.exit('harness dependency failure')
if command('harness-build', ['go','test','-c','-o',str(RUNTIME/'pilot.test')], HERE/'harness', timeout=180).returncode:
    sys.exit('harness build failure')
TEST = ['go','test','-json','-count=1','-parallel=1','-timeout=10s','./pkg/types',
        '-rapid.seed=1318','-rapid.nofailfile']
if command('baseline', TEST, subject, timeout=90).returncode: sys.exit('baseline failed')
command('warm-baseline', TEST, subject)
run_ooze('positive', subject, 'pkg/types/entity_id.go', 'boundary', '^TestPropEntityIDByteBound$')
run_ooze('negative', subject, 'pkg/types/entity_id.go', 'boundary', '^TestPropEntityIDRoundTrip$')
run_ooze('restored-coverage', subject, 'pkg/types/entity_id.go', 'boundary', '^TestPropEntityIDByteBound$')

fixture = RUNTIME/'fixture'
fixture.mkdir()
(fixture/'go.mod').write_text('module fixture\n\ngo 1.26.3\n')
(fixture/'fixture.go').write_text('package fixture\n\nfunc Less(x int) bool { return x < 5 }\n')
(fixture/'fixture_test.go').write_text('package fixture\nimport "testing"\nfunc TestLess(t *testing.T) { if !Less(4) || Less(5) { t.Fatal("boundary assertion") } }\n')
write(OUT/'fixture-manifest.json', {'files': {str(p.relative_to(fixture)):p.read_text() for p in fixture.iterdir()}})
command('fixture-baseline', ['go','test','-json','-count=1','-timeout=5s','./...'],fixture)
run_ooze('invalid-build', fixture, 'fixture.go', 'invalid')
run_ooze('command-exit', fixture, 'fixture.go', mode='exit')
run_ooze('missing-command', fixture, 'fixture.go', missing=True)
run_ooze('zero-tests', fixture, 'fixture.go', selector='^NoSuchTest$')
zero = RUNTIME/'zero'; shutil.copytree(fixture,zero)
(zero/'fixture.go').write_text('package fixture\n\nfunc Less(x int) bool { return true }\n')
run_ooze('zero-mutations', zero, 'fixture.go')
slow = RUNTIME/'timeout'; shutil.copytree(fixture,slow)
(slow/'fixture_test.go').write_text('package fixture\nimport("testing";"time")\nfunc TestTimeout(t *testing.T){time.Sleep(time.Second)}\n')
run_ooze('test-timeout', slow, 'fixture.go', timeout='100ms')
equiv = RUNTIME/'equivalent'; shutil.copytree(fixture,equiv)
(equiv/'fixture.go').write_text('package fixture\n\nfunc Less(x int) bool { return x < x }\n')
(equiv/'fixture_test.go').write_text('package fixture\nimport "testing"\nfunc TestEquivalent(t *testing.T){for _,x:=range []int{-1,0,1}{if Less(x){t.Fatal("irreflexive")}}}\n')
command('equivalent-baseline',['go','test','-json','-count=1','-timeout=5s','./...'],equiv)
run_ooze('equivalent',equiv,'fixture.go','equivalent')
(fixture/'sentinel.txt').write_text('original\n')
run_ooze('source-preservation',fixture,'fixture.go',mode='sentinel')
run_ooze('normal-descendant-cleanup',fixture,'fixture.go',mode='orphan',deadline=15)
run_ooze('cancellation',fixture,'fixture.go',mode='block',cancel=True,deadline=15)
run_ooze('outer-deadline',fixture,'fixture.go',mode='block',deadline=3)
try:
    run_ooze('driver-interruption',fixture,'fixture.go',mode='block',deadline=15,interrupt_driver=True)
except KeyboardInterrupt:
    # This one control intentionally signals this driver, after recording its own PID.
    # The runner itself re-raises; only the explicit calibration consumes the exception.
    assert (OUT/'driver-interruption/driver-signal.json').exists()
    write(OUT/'driver-interruption/exception-observed.json',{'exception':'KeyboardInterrupt',
          'propagated_from_run_ooze':True,'continued_only_for_named_calibration':True})
else:
    raise RuntimeError('driver interruption control did not propagate KeyboardInterrupt')
run_ooze('discovery',subject,'pkg/types/entity_id.go',deadline=900)

# Replay every saved candidate with the exact fixed discovery check command.
discovery_command = json.loads((OUT/'discovery/manifest.json').read_text())['test_command']
original_bytes = (subject/'pkg/types/entity_id.go').read_bytes()
manual = []
for candidate in sorted((OUT/'discovery').glob('candidate-*')):
    prefix = 'manual-'+candidate.name
    before = hashes(subject)
    baseline_result = command(prefix+'-original',discovery_command,subject)
    try:
        (subject/'pkg/types/entity_id.go').write_bytes((candidate/'mutant.go.txt').read_bytes())
        mutant_result = command(prefix+'-mutant',discovery_command,subject)
    finally:
        (subject/'pkg/types/entity_id.go').write_bytes(original_bytes)
    restored_result = command(prefix+'-restored',discovery_command,subject)
    manual.append({'candidate': candidate.name, 'baseline': baseline_result.returncode,
                   'mutant': mutant_result.returncode, 'restored': restored_result.returncode,
                   'source_restored': hashes(subject)==before})
write(OUT/'manual-replay.json',manual)

# Promote exact generated inputs for the panic and property-only detections.
# This changes the check set, so each witness starts a new passing baseline.
for index in (4,6,8,10):
    candidate = OUT/'discovery'/('candidate-%02d'%index)
    events = [json.loads(line) for line in (candidate/'test.log').read_text().splitlines() if line.startswith('{')]
    outputs = [event.get('Output','') for event in events if event.get('Test')=='TestPropEntityIDRoundTrip']
    matches = [re.search(r'canonical ID ("[^"]+") rejected',line) for line in outputs]
    identifiers = [json.loads(match.group(1)) for match in matches if match]
    if identifiers:
        witness = identifiers[0]
    else:
        draws = [re.search(r'\[rapid\] draw segment: ("[^"]+")',line) for line in outputs]
        witness = '.'.join(json.loads(match.group(1)) for match in draws if match)
    assert len(witness.split('.'))==6, witness
    test_source = ('package types\nimport "testing"\n'
        '// Experimental promotion of the exact input recorded by the fixed discovery run.\n'
        'func TestPilotRecordedWitness(t *testing.T) {\n'
        '  if _, err := ParseEntityID('+json.dumps(witness)+'); err != nil { t.Fatalf("canonical ID rejected: %v",err) }\n}\n')
    write(OUT/('witness-%02d.json'%index),{'candidate':candidate.name,'input':witness,
          'origin':'discovery TestPropEntityIDRoundTrip output; same bytes on both implementations'})
    (OUT/('witness-%02d.go.txt'%index)).write_text(test_source)
    witness_path = subject/'pkg/types/pilot_recorded_witness_test.go'
    assert not witness_path.exists()
    witness_path.write_text(test_source)
    replay_command = ['go','test','-json','-count=1','-parallel=1','-timeout=10s',
                      '-run=^TestPilotRecordedWitness$','./pkg/types','-rapid.nofailfile']
    prefix = 'witness-%02d'%index
    command(prefix+'-original',replay_command,subject)
    try:
        (subject/'pkg/types/entity_id.go').write_bytes((candidate/'mutant.go.txt').read_bytes())
        command(prefix+'-mutant',replay_command,subject)
    finally:
        (subject/'pkg/types/entity_id.go').write_bytes(original_bytes)
    command(prefix+'-restored',replay_command,subject)
    assert witness_path.read_text()==test_source
    witness_path.unlink()


# Replay the identical curated witness against saved mutant bytes, then restore by bytes/hash.
original = (subject/'pkg/types/entity_id.go').read_bytes()
mutant = (OUT/'positive/candidate-01/mutant.go.txt').read_bytes()
witness = 'testdata/rapid/TestPropEntityIDByteBound/TestPropEntityIDByteBound-20260831140043-41866.fail'
REPLAY = ['go','test','-json','-count=1','-parallel=1','-timeout=10s','-run=^TestPropEntityIDByteBound$',
          './pkg/types','-rapid.seed=1318','-rapid.nofailfile','-rapid.failfile='+witness]
command('paired-original',REPLAY,subject)
try:
    (subject/'pkg/types/entity_id.go').write_bytes(mutant)
    command('paired-mutant',REPLAY,subject)
finally:
    (subject/'pkg/types/entity_id.go').write_bytes(original)
command('paired-restored',REPLAY,subject)
command('final-race-baseline',TEST[:2]+['-race']+TEST[2:],subject,timeout=90)
write(OUT/'final-integrity.json', {'source_files_match_initial': hashes(subject)==json.loads((OUT/'source-manifest.json').read_text())['files'],
      'subject': str(subject), 'runtime_retained_for_inspection': str(RUNTIME)})
current_runner_hashes = {name:hashlib.sha256((HERE/name).read_bytes()).hexdigest() for name in runner_hashes}
write(OUT/'runner-integrity.json',{'source_unchanged_during_run':current_runner_hashes==runner_hashes,
                                 'source_before':runner_hashes,'source_after':current_runner_hashes})
if current_runner_hashes!=runner_hashes: raise RuntimeError('runner changed during measurement')
print('DONE', OUT, flush=True)

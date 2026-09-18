#!/usr/bin/env python3
"""Offline #1317 fixtures. Every Docker/socket/sysctl command is a private PATH stub."""
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import tempfile
import time
import unittest

ROOT = Path(__file__).resolve().parent.parent
STUB = r'''#!PYTHON
import json, os, pathlib, signal, subprocess, sys, time
name = pathlib.Path(sys.argv[0]).name
args = sys.argv[1:]
with open(os.environ['TRACE'], 'a') as trace:
    trace.write(json.dumps([name, *args]) + '\n')
mode = os.environ.get('FIXTURE', 'bind')
with open(os.environ['PROCESS_TRACE'], 'a') as trace:
    trace.write(json.dumps({'pid':os.getpid(), 'pgid':os.getpgrp()}) + '\n')

def hang_with_child():
    child = subprocess.Popen([sys.executable, '-c', 'import signal,time; signal.signal(signal.SIGTERM, signal.SIG_IGN); time.sleep(30)'])
    with open(os.environ['PROCESS_TRACE'], 'a') as trace:
        trace.write(json.dumps({'pid':child.pid, 'pgid':os.getpgid(child.pid)}) + '\n')
    pathlib.Path(os.environ['DESCENDANT_PID']).write_text(str(child.pid))
    pathlib.Path(os.environ['READY']).touch()
    time.sleep(30)

if (mode == 'fixture-compose-hang' and name == 'docker' and 'up' in args) or (mode == 'fixture-probe-hang' and name == 'ss'):
    hang_with_child()
probe = {'ss':'socket', 'sysctl':'sysctl', 'ps':'proxy'}.get(name)
if name == 'docker' and args and args[0] in ('ps', 'inspect'):
    probe = args[0]
if mode == 'resist-' + str(probe):
    pathlib.Path(os.environ['PROBE_PID']).write_text(str(os.getpid()))
    signal.signal(signal.SIGTERM, signal.SIG_IGN)
    time.sleep(30)
if name == 'uname':
    assert args == ['-s'], args
    print('Linux')
    sys.exit(0)
if name == 'sudo':
    if mode == 'blocking-sudo':
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        time.sleep(30)
    raise SystemExit('UNEXPECTED sudo invocation')
if name == 'docker':
    if args == ['compose', '-f', 'docker/compose/tiered.yml', '--profile', 'statistical', 'up', '-d', '--wait', '--build']:
        if mode == 'success':
            print('services healthy')
            sys.exit(0)
        if mode in ('nonbind', 'build-bind-text'):
            print('build failed: missing source' if mode == 'nonbind' else 'RUN test: listen tcp: bind: address already in use', file=sys.stderr)
            sys.exit(23)
        errors = {
            'legacy': 'Bind for 0.0.0.0:34222 failed: port is already allocated',
            'ipv6': 'failed to bind host port for [::]:34222:172.18.0.2:4222/tcp: address already in use',
            'unknown': 'ports are not available: bind: address already in use',
        }
        print(errors.get(mode, 'failed to bind host port for 0.0.0.0:34222:172.18.0.2:4222/tcp: address already in use'), file=sys.stderr)
        sys.exit(42)
    if args == ['compose', '-f', 'docker/compose/tiered.yml', '--profile', 'statistical', 'down', '-v', '--timeout', '15']:
        print('deferred teardown')
        sys.exit(0)
    if args == ['ps', '--all', '--quiet', '--no-trunc']:
        if mode == 'docker-error':
            print('daemon unavailable', file=sys.stderr)
            sys.exit(9)
        print('\n'.join('id' + str(n) for n in range(101)) if mode == 'many-containers' else 'request\nholder\nwrongtarget')
        sys.exit(0)
    if args[:2] == ['inspect', '--format']:
        # Assert the real request projects only the fields it needs, not raw inspect or environment.
        assert '.HostConfig.PortBindings' in args[2] and '.NetworkSettings.Ports' in args[2]
        assert '.Config.Env' not in args[2]
        if mode == 'many-containers':
            assert len(args[3:]) == 100, args
        records = [
            {'id':'request','name':'/failed-start','status':'created','pid':0,'error':'bind rejected',
             'requested_bindings':{'4222/tcp':[{'HostIp':'0.0.0.0','HostPort':'34222'}]},'observed_mappings':{}},
            {'id':'holder','name':'/observed-holder','status':'running','pid':123,'error':'',
             'requested_bindings':{'80/tcp':[{'HostIp':'','HostPort':'0'}]},
             'observed_mappings':{'80/tcp':[{'HostIp':'0.0.0.0','HostPort':'34222'}]}},
            {'id':'wrongtarget','name':'/target-is-not-host','status':'running','pid':456,'error':'',
             'requested_bindings':{'34222/tcp':[{'HostIp':'','HostPort':'4222'}]},
             'observed_mappings':{'34222/tcp':[{'HostIp':'','HostPort':'4222'}]}},
        ]
        if mode == 'no-match':
            records = records[-1:]
        if mode == 'invalid-inspect':
            print('not json')
            sys.exit(0)
        for row in records:
            print(json.dumps(row))
        sys.exit(0)
    raise SystemExit('UNEXPECTED docker call: ' + repr(args))
if name == 'ss':
    if mode == 'probe-error':
        print('permission denied', file=sys.stderr)
        sys.exit(7)
    if mode == 'large-sockets':
        print('x' * 70000)
        sys.exit(0)
    if mode == 'empty':
        sys.exit(0)
    assert '-a' in args and '-p' in args and '-l' not in args, args
    if mode != 'unknown':
        assert args[-1] == 'sport = :34222', args
    print('ESTAB 0 0 127.0.0.1:34222 127.0.0.1:443 users:(("fixture",pid=123,fd=9))')
    print('TIME-WAIT 0 0 127.0.0.1:34222 127.0.0.1:443')
    sys.exit(0)
if name == 'ps':
    assert args == ['-ww', '-C', 'docker-proxy', '-o', 'pid=,ppid=,user=,args='], args
    if mode == 'empty-proxy':
        sys.exit(0)
    if mode == 'error-proxy':
        print('process visibility denied', file=sys.stderr)
        sys.exit(7)
    print('789 1 root /usr/bin/docker-proxy -proto tcp -host-ip 0.0.0.0 -host-port 34222 -container-ip 172.18.0.2 -container-port 4222')
    sys.exit(0)
if name == 'lsof':
    print('fixture 123 user TCP 127.0.0.1:34222->127.0.0.1:443 (ESTABLISHED)')
    sys.exit(0)
if name == 'sysctl':
    assert args == ['net.ipv4.ip_local_reserved_ports', 'net.ipv4.ip_local_port_range'], args
    print('net.ipv4.ip_local_reserved_ports = 34222,34550,36060')
    print('net.ipv4.ip_local_port_range = 32768 60999')
    sys.exit(0)
raise SystemExit('UNEXPECTED executable: ' + name)
'''


class BindDiagnosticsTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='e2e-bind-fixture-')
        self.addCleanup(self.tmp.cleanup)
        self.home = Path(self.tmp.name)
        self.bin = self.home / 'bin'
        self.bin.mkdir()
        # No inherited PATH means an omitted stub cannot fall through to real infrastructure.
        for name in ('bash', 'sed', 'grep', 'tee', 'mktemp', 'rm', 'head', 'cat', 'sort', 'awk', 'jq', 'timeout', 'task'):
            source = shutil.which(name)
            self.assertIsNotNone(source, f'fixture prerequisite missing: {name}')
            (self.bin / name).symlink_to(source)
        for name in ('docker', 'ss', 'lsof', 'sysctl', 'ps', 'sudo', 'uname'):
            path = self.bin / name
            path.write_text(STUB.replace('PYTHON', str(Path(shutil.which('python3')).resolve()), 1))
            path.chmod(0o755)
        self.trace = self.home / 'trace.jsonl'
        self.env = {**os.environ, 'PATH': str(self.bin), 'TRACE': str(self.trace), 'FIXTURE': 'bind',
                    'PROBE_PID': str(self.home / 'probe.pid'),
                    'PROCESS_TRACE': str(self.home / 'processes.jsonl'),
                    'DESCENDANT_PID': str(self.home / 'descendant.pid'), 'READY': str(self.home / 'ready')}
        (self.home / 'scripts').mkdir()
        wrapper = ROOT / 'scripts/e2e-statistical-up.sh'
        if wrapper.exists():
            shutil.copy2(wrapper, self.home / 'scripts' / wrapper.name)
        # Use the real task's commands/defer; replace only its unrelated dependencies and final scenario.
        task = (ROOT / 'taskfiles/e2e/statistical.yml').read_text()
        start = task.index('    deps:\n')
        end = task.index('    cmds:\n', start)
        task = task[:start] + task[end:]
        task = task.replace('cd cmd/e2e && ./e2e --scenario tiered --variant statistical --output-dir ./test/e2e/results',
                            'echo fixture-scenario')
        (self.home / 'Taskfile.yml').write_text(task)

    def run_case(self, mode='bind', task=False, deadline=20, wait_ready=False):
        self.env['FIXTURE'] = mode
        cmd = ['task', '--exit-code'] if task else ['bash', 'scripts/e2e-statistical-up.sh']
        process_trace = Path(self.env['PROCESS_TRACE'])
        process_trace.write_text('')
        Path(self.env['READY']).unlink(missing_ok=True)
        started = time.monotonic()
        process = subprocess.Popen(cmd, cwd=self.home, env=self.env, text=True, start_new_session=True,
                                   stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        try:
            if wait_ready:
                while not Path(self.env['READY']).exists():
                    if process.poll() is not None or time.monotonic() - started > 5:
                        raise RuntimeError('timeout witness did not become ready within 5s')
                    time.sleep(0.01)
            output, _ = process.communicate(timeout=deadline)
            return subprocess.CompletedProcess(cmd, process.returncode, output)
        finally:
            # GNU timeout creates probe groups within this session. Clean only live groups recorded by our stubs,
            # plus the session's original Task/bash group; never assume one killpg covers every descendant.
            groups = {process.pid}
            for row in process_trace.read_text().splitlines():
                entry = json.loads(row)
                try:
                    if os.getsid(entry['pid']) == process.pid and os.getpgid(entry['pid']) == entry['pgid']:
                        groups.add(entry['pgid'])
                except ProcessLookupError:
                    pass
            for group in groups:
                try:
                    os.killpg(group, signal.SIGKILL)
                except ProcessLookupError:
                    pass
            process.communicate(timeout=3)
            self.elapsed = time.monotonic() - started
            self.events = [json.loads(line) for line in self.trace.read_text().splitlines()] if self.trace.exists() else []

    def test_fixture_timeout_removes_descendants_in_original_and_probe_groups(self):
        for mode in ('fixture-compose-hang', 'fixture-probe-hang'):
            with self.subTest(mode=mode):
                with self.assertRaises(subprocess.TimeoutExpired):
                    self.run_case(mode, task=True, deadline=0.1, wait_ready=True)
                pid = int(Path(self.env['DESCENDANT_PID']).read_text())
                # Poll the owned child's disappearance, not an assumed teardown delay.
                until = time.monotonic() + 2
                while True:
                    try:
                        os.kill(pid, 0)
                    except ProcessLookupError:
                        break
                    if time.monotonic() >= until:
                        self.fail(f'fixture descendant {pid} survived timeout cleanup')
                    time.sleep(0.01)

    def test_task_captures_before_deferred_down(self):
        result = self.run_case(task=True)
        self.assertEqual(result.returncode, 42, result.stdout)
        self.assertIn('[BIND-DIAG] failed host port: 34222', result.stdout)
        self.assertIn('observed-holder', result.stdout)
        self.assertLess(result.stdout.index('observed-holder'), result.stdout.index('deferred teardown'))
        self.assertNotIn('fixture-scenario', result.stdout)
        self.assertEqual(sum('up' in event for event in self.events), 1, self.events)
        self.assertEqual(sum('down' in event for event in self.events), 1, self.events)
        self.assertEqual(self.events[-1][-4:], ['down', '-v', '--timeout', '15'])

    def test_host_port_not_container_target_and_all_socket_states(self):
        result = self.run_case()
        self.assertEqual(result.returncode, 42, result.stdout)
        for value in ('failed host port: 34222', 'ESTAB', 'TIME-WAIT', 'pid=123',
                      'requested_bindings', 'observed_mappings', 'failed-start', 'observed-holder',
                      'not proof of the current holder', 'ip_local_reserved_ports = 34222'):
            self.assertIn(value, result.stdout)
        self.assertNotIn('target-is-not-host', result.stdout)
        self.assertFalse(any('publish=' in arg for event in self.events for arg in event))

    def test_success_does_not_probe_and_task_still_runs_scenario(self):
        result = self.run_case('success', task=True)
        self.assertEqual(result.returncode, 0, result.stdout)
        self.assertNotIn('[BIND-DIAG]', result.stdout)
        self.assertIn('fixture-scenario', result.stdout)
        self.assertEqual(len(self.events), 2, self.events)

    def test_non_bind_failure_preserves_status_without_probes(self):
        for mode in ('nonbind', 'build-bind-text'):
            with self.subTest(mode=mode):
                self.trace.unlink(missing_ok=True)
                result = self.run_case(mode)
                self.assertEqual(result.returncode, 23, result.stdout)
                self.assertNotIn('[BIND-DIAG]', result.stdout)
                self.assertEqual(len(self.events), 1, self.events)

    def test_legacy_and_ipv6_host_error_forms(self):
        for mode in ('legacy', 'ipv6'):
            with self.subTest(mode=mode):
                result = self.run_case(mode)
                self.assertEqual(result.returncode, 42, result.stdout)
                self.assertIn('failed host port: 34222', result.stdout)

    def test_unknown_port_is_explicit_and_still_collects(self):
        result = self.run_case('unknown')
        self.assertEqual(result.returncode, 42, result.stdout)
        self.assertIn('failed host port: UNKNOWN', result.stdout)
        self.assertIn('observed-holder', result.stdout)
        self.assertIn('ESTAB', result.stdout)

    def test_empty_or_failed_probe_never_claims_no_holder(self):
        for mode in ('empty', 'probe-error', 'docker-error'):
            with self.subTest(mode=mode):
                result = self.run_case(mode)
                self.assertEqual(result.returncode, 42, result.stdout)
                self.assertIn('UNKNOWN', result.stdout)
                self.assertNotIn('no holder', result.stdout.lower())

    def test_missing_probe_and_timeout_keep_failure(self):
        (self.bin / 'ss').unlink()
        (self.bin / 'lsof').unlink()
        result = self.run_case()
        self.assertEqual(result.returncode, 42, result.stdout)
        self.assertIn('socket probe unavailable', result.stdout)
        (self.bin / 'timeout').unlink()
        result = self.run_case()
        self.assertEqual(result.returncode, 42, result.stdout)
        self.assertIn('timeout unavailable', result.stdout)

    def test_unavailable_log_storage_preserves_compose_status(self):
        (self.bin / 'mktemp').unlink()
        stub = self.bin / 'mktemp'
        stub.write_text('#!/bin/bash\nexit 1\n')
        stub.chmod(0o755)
        result = self.run_case()
        self.assertEqual(result.returncode, 42, result.stdout)
        self.assertIn('UNKNOWN: cannot retain compose output', result.stdout)
        self.assertEqual(len(self.events), 1, self.events)

    def test_blocking_sudo_cannot_delay_diagnostics(self):
        try:
            result = self.run_case('blocking-sudo', deadline=5)
        except subprocess.TimeoutExpired:
            self.fail('blocking sudo delayed the diagnostic path past the 5s fixture bound')
        self.assertEqual(result.returncode, 42, result.stdout)
        self.assertFalse(any(event[0] == 'sudo' for event in self.events), self.events)

    def test_proxy_candidates_show_root_user_and_host_vs_target_port(self):
        result = self.run_case()
        self.assertEqual(result.returncode, 42, result.stdout)
        self.assertIn('789 1 root /usr/bin/docker-proxy', result.stdout)
        self.assertIn('-host-port 34222', result.stdout)
        self.assertIn('-container-port 4222', result.stdout)
        self.assertIn('metadata, not proof of socket ownership', result.stdout)
        self.assertIn('disabled/renamed proxies and non-proxy holders are not covered', result.stdout)
        self.assertFalse(any(event[0] == 'sudo' for event in self.events), self.events)

    def test_proxy_metadata_empty_or_failed_is_unknown(self):
        for mode in ('empty-proxy', 'error-proxy'):
            with self.subTest(mode=mode):
                result = self.run_case(mode)
                self.assertEqual(result.returncode, 42, result.stdout)
                self.assertIn('UNKNOWN: docker-proxy candidate processes', result.stdout)

    def test_missing_ss_uses_lsof(self):
        (self.bin / 'ss').unlink()
        result = self.run_case()
        self.assertEqual(result.returncode, 42, result.stdout)
        self.assertIn('socket endpoints/processes (lsof', result.stdout)
        self.assertIn('ESTABLISHED', result.stdout)

    def test_missing_jq_or_sysctl_is_explicit(self):
        for executable in ('jq', 'sysctl'):
            with self.subTest(executable=executable):
                (self.bin / executable).unlink()
                result = self.run_case()
                self.assertEqual(result.returncode, 42, result.stdout)
                self.assertIn('UNKNOWN', result.stdout)
                self.assertIn(executable, result.stdout)

    def test_truncation_and_unmatched_or_invalid_records_are_explicit(self):
        cases = [('large-sockets', 'output truncated at 64 KiB'),
                 ('many-containers', 'container inventory truncated to first 100 IDs'),
                 ('no-match', 'no matching container port records; holder unresolved'),
                 ('invalid-inspect', 'could not decode container port evidence')]
        for mode, expected in cases:
            with self.subTest(mode=mode):
                result = self.run_case(mode)
                self.assertEqual(result.returncode, 42, result.stdout)
                self.assertIn('UNKNOWN:', result.stdout)
                self.assertIn(expected, result.stdout)

    def test_term_resistant_probes_are_killed_and_preserve_task_failure(self):
        for probe in ('socket', 'proxy', 'sysctl', 'ps', 'inspect'):
            with self.subTest(probe=probe):
                result = self.run_case('resist-' + probe, task=True)
                self.assertEqual(result.returncode, 42, result.stdout)
                self.assertIn('failed or timed out (exit 137)', result.stdout)
                # Each deadline is 2s plus 1s TERM-to-KILL grace; 10s allows loaded CI startup overhead.
                self.assertLess(self.elapsed, 10, result.stdout)
                pid = int((self.home / 'probe.pid').read_text())
                with self.assertRaises(ProcessLookupError, msg=f'probe {pid} survived deadline'):
                    os.kill(pid, 0)
                self.assertEqual(self.events[-1][-4:], ['down', '-v', '--timeout', '15'])
                if probe == 'socket':
                    self.assertIn('observed-holder', result.stdout)


if __name__ == '__main__':
    unittest.main(verbosity=2)

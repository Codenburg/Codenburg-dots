"""Synthetic G3-G5 checks; never collect live evidence."""
import copy
import json
import unittest
from unittest.mock import patch

from isolation_checks import IsolationEvidenceError, validate_kernel_bundle

NS = ('user', 'mnt', 'pid', 'net', 'ipc', 'uts', 'cgroup')


def frame(pid, ids, nspid, base=20):
    fields = ['S'] + ['0'] * 18 + ['99'] + ['0'] * 30
    status = {'Pid': str(pid), 'NSpid': nspid, 'Uid': ids, 'Gid': ids,
              'Groups': '', 'NoNewPrivs': '1', 'Seccomp': '2'}
    status.update({k: '0000000000000000' for k in
                   ('CapInh', 'CapPrm', 'CapEff', 'CapBnd', 'CapAmb')})
    return dict(stat=f'{pid} (name with ) spaces) ' + ' '.join(fields),
                status=''.join(f'{k}:\t{v}\n' for k, v in status.items()),
                uid_map='0 1 65536\n', gid_map='0 1 65536\n',
                mountinfo='synthetic mount evidence retained by parent',
                namespaces={n: f'{n}:[{base+i}]' for i, n in enumerate(NS)})


def bundle():
    controller = frame(50, '1000 1000 1000 1000', '50', 40)
    controller['uid_map'] = controller['gid_map'] = '0 0 4294967295\n'
    init = frame(80, '100000 100000 100000 100000', '80 1')
    init['uid_map'] = init['gid_map'] = '0 100000 65536\n'
    guest_init = frame(1, '0 0 0 0', '1')
    collector = frame(2, '0 0 0 0', '2')
    inspect = dict(Id='a'*64, Name='owned', Image='b'*64, ImageDigest='sha256:'+'c'*64,
                   Created='created', RestartCount=0,
                   State=dict(Running=True, Paused=False, Restarting=False, Dead=False,
                              Error='', Pid=80, StartedAt='started'),
                   Config=dict(Cmd=['fixed'], Env=['HOME=/root']),
                   EffectiveCaps=[], BoundingCaps=[], HostConfig=dict(
                       Privileged=False, Devices=[], GroupAdd=[], CapAdd=[], CapDrop=['ALL'],
                       SecurityOpt=['no-new-privileges'], UsernsMode='private',
                       PidMode='private', IpcMode='private', UTSMode='private',
                       CgroupMode='private', NetworkMode='none',
                       IDMappings={'UidMap': ['0:1:65536'], 'GidMap': ['0:1:65536']}))
    maps = [dict(container_id=0, host_id=1000, size=1),
            dict(container_id=1, host_id=100000, size=65536)]
    return dict(host=dict(controller=controller, init=init, init_stat_after=init['stat']),
                guest=dict(init=guest_init, collector=collector),
                runtime_info={'host': {'idMappings': {'uidmap': maps, 'gidmap': maps}}},
                before_inspect=inspect, after_inspect=copy.deepcopy(inspect),
                uid=1000, gid=1000, groups=[1000])


class KernelTests(unittest.TestCase):
    def call(self, data):
        old = copy.deepcopy(data)
        with patch('subprocess.Popen', side_effect=AssertionError('spawn forbidden')):
            try:
                return validate_kernel_bundle(**data)
            finally:
                self.assertEqual(data, old, 'validator mutated evidence')

    def test_positive_report_is_narrow_and_serializable(self):
        self.assertEqual(self.call(bundle()), {'scope': 'kernel-G3-G5-only',
                                             'checks': ['G3', 'G4', 'G5']})
        json.dumps(self.call(bundle()))

    def test_rejections(self):
        cases = []
        def add(label, path, value):
            data = bundle()
            obj = data
            for key in path[:-1]:
                obj = obj[key]
            obj[path[-1]] = value
            cases.append((label, data))
        for key in ('Id', 'Name', 'Image', 'ImageDigest', 'Created', 'Config', 'HostConfig'):
            add('inspect drift '+key, ['after_inspect', key], None)
        for key, value in [('Running', False), ('Paused', True), ('Restarting', True),
                           ('Dead', True), ('Error', 'error'), ('Pid', 81), ('StartedAt', '')]:
            add('state '+key, ['after_inspect', 'State', key], value)
        add('restart', ['before_inspect', 'RestartCount'], 1)
        add('reuse', ['host', 'init_stat_after'], frame(80, '', '')['stat'].replace('99', '100', 1))
        for side, name in [('host', 'init'), ('guest', 'init'), ('guest', 'collector')]:
            add('missing frame', [side, name], {})
            for ns in NS:
                add('shared '+ns, [side, name, 'namespaces', ns], bundle()['host']['controller']['namespaces'][ns])
            for cap in ('CapInh', 'CapPrm', 'CapEff', 'CapBnd', 'CapAmb'):
                for mask in ('1', '0000000000000001', '0x0', 'g'*16, '0'*17):
                    status = bundle()[side][name]['status'].replace(f'{cap}:\t0000000000000000', f'{cap}:\t{mask}')
                    add(cap+mask, [side, name, 'status'], status)
            for field, value in [('NoNewPrivs', '0'), ('Seccomp', '0'), ('Uid', '999 999 999 999'),
                                 ('Gid', '999 999 999 999'), ('Groups', '999')]:
                status = bundle()[side][name]['status']
                lines = [f'{field}:\t{value}' if line.startswith(field+':') else line for line in status.splitlines()]
                add(field, [side, name, 'status'], '\n'.join(lines)+'\n')
        for text in ('', '0 0 65536', '0 1000 65536', '0 100000 0',
                     '0 100000 65536\n0 200000 1', '0 4294967295 65536', '00 100000 65536'):
            add('map '+text, ['host', 'init', 'uid_map'], text)
        add('guest frame mismatch', ['guest', 'init', 'gid_map'], '0 100000 65536')
        add('supplementary host group', ['groups'], [100001])
        add('missing runtime', ['runtime_info'], {})
        add('parent overlap', ['runtime_info', 'host', 'idMappings', 'uidmap'],
            [dict(container_id=0, host_id=1000, size=65537), dict(container_id=1, host_id=100000, size=65536)])
        for key, value in [('Privileged', True), ('Devices', ['x']), ('GroupAdd', ['1']),
                           ('CapAdd', ['CHOWN']), ('CapDrop', []), ('SecurityOpt', ['seccomp=unconfined']),
                           ('NetworkMode', 'host'), ('UsernsMode', 'host')]:
            data = bundle()
            for inspect in ('before_inspect', 'after_inspect'):
                data[inspect]['HostConfig'][key] = value
            cases.append((key, data))
        for label, data in cases:
            with self.subTest(label=label), self.assertRaises(IsolationEvidenceError):
                self.call(data)

    def test_dictionary_declaration_preserved(self):
        data = bundle()
        for inspect in ('before_inspect', 'after_inspect'):
            data[inspect]['HostConfig']['IDMappings'] = {
                k: [dict(ContainerID=0, HostID=1, Size=65536)] for k in ('UidMap', 'GidMap')}
        self.call(data)

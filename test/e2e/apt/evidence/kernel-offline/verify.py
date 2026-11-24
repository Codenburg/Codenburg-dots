import contextlib
import io
import json
import os
import pathlib
import signal
import sys
import unittest

root = pathlib.Path('test/e2e/apt/evidence/kernel-offline')
os.chmod(root, 0o700)
def deny(event, args):
    if event in ('subprocess.Popen', 'os.system', 'os.posix_spawn', 'os.exec', 'os.fork', 'os.forkpty'):
        raise AssertionError('process spawn denied')
sys.addaudithook(deny)
signal.signal(signal.SIGALRM, lambda *_: sys.exit(124))
signal.alarm(30)
sys.path.insert(0, 'test/e2e/apt')
out, err = io.StringIO(), io.StringIO()
with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
    suite = unittest.defaultTestLoader.discover('test/e2e/apt', pattern='test_isolation_checks.py')
    result = unittest.TextTestRunner(verbosity=2).run(suite)
rc = 0 if result.wasSuccessful() else 1
for suffix, content in [('stdout', out.getvalue()), ('stderr', err.getvalue()), ('exit', str(rc)+'\n')]:
    (root / (sys.argv[1]+'.'+suffix)).write_text(content)
print(json.dumps({'exit': rc, 'stdout': out.getvalue(), 'stderr': err.getvalue()}))
sys.exit(rc)

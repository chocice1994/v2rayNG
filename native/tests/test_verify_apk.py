import importlib.util
from pathlib import Path
import tempfile
import unittest
import zipfile

spec = importlib.util.spec_from_file_location('verify_apk', Path(__file__).resolve().parents[1] / 'verify_apk.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class VerifyApkTest(unittest.TestCase):
    def test_complete_missing_and_empty(self):
        for mode in ('complete', 'missing', 'empty'):
            with self.subTest(mode=mode), tempfile.TemporaryDirectory() as tmp:
                apk = Path(tmp) / 'fixture.apk'
                with zipfile.ZipFile(apk, 'w') as archive:
                    for abi in module.ABIS:
                        for library in module.LIBRARIES:
                            if mode == 'missing' and abi == 'x86' and library == 'libhevsockstun.so':
                                continue
                            data = b'' if mode == 'empty' else b'fixture'
                            archive.writestr(f'lib/{abi}/{library}', data)
                if mode == 'complete':
                    module.verify(apk)
                else:
                    with self.assertRaises(ValueError):
                        module.verify(apk)

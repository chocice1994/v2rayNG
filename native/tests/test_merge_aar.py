import importlib.util
import io
from pathlib import Path
import tempfile
import unittest
import zipfile

spec = importlib.util.spec_from_file_location('merge_aar', Path(__file__).resolve().parents[1] / 'merge_aar.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


def jar(payload=b'java', year=2020):
    data = io.BytesIO()
    with zipfile.ZipFile(data, 'w') as archive:
        info = zipfile.ZipInfo('Binding.class', date_time=(year, 1, 1, 0, 0, 0))
        archive.writestr(info, payload)
    return data.getvalue()


class MergeAarTest(unittest.TestCase):
    def fixtures(self, directory, changed=False):
        result = []
        for i, abi in enumerate(sorted(module.ABIS)):
            path = directory / f'{abi}.aar'
            with zipfile.ZipFile(path, 'w') as archive:
                archive.writestr('classes.jar', jar(b'wrong' if changed and i == 2 else b'java', 2020 + i))
                archive.writestr('AndroidManifest.xml', b'manifest')
                archive.writestr(f'jni/{abi}/libgojni.so', abi.encode())
            result.append(path)
        return result

    def test_timestamp_only_jar_changes_and_all_abis(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            inputs = self.fixtures(root)
            output = root / 'merged.aar'
            module.merge(output, inputs)
            self.assertEqual(6, len(module.read_entries(output.read_bytes())))
            with self.assertRaises(ValueError):
                module.merge(output, inputs)

    def test_different_bindings_fail_closed(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            with self.assertRaisesRegex(ValueError, 'Non-JNI'):
                module.merge(root / 'bad.aar', self.fixtures(root, changed=True))
            self.assertFalse((root / 'bad.aar').exists())

    def test_missing_and_duplicate_abi_fail_closed(self):
        for duplicate in (False, True):
            with tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                inputs = self.fixtures(root)
                inputs = inputs[:-1] + [inputs[0]] if duplicate else inputs[:-1]
                with self.assertRaises(ValueError):
                    module.merge(root / 'bad.aar', inputs)


if __name__ == '__main__':
    unittest.main()

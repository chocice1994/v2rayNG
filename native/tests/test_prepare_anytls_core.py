import importlib.util
from pathlib import Path
import tempfile
import unittest

MODULE_PATH = Path(__file__).resolve().parents[1] / 'prepare_anytls_core.py'
spec = importlib.util.spec_from_file_location('prepare_anytls_core', MODULE_PATH)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class PrepareCoreTest(unittest.TestCase):
    def test_anchor_must_be_unique(self):
        for source in ('', 'anchor anchor'):
            with self.assertRaises(ValueError):
                module.replace_once(source, 'anchor', 'new')
        self.assertEqual('new', module.replace_once('anchor', 'anchor', 'new'))

    def test_pinned_overlay_and_ownership(self):
        with tempfile.TemporaryDirectory() as tmp:
            output = module.prepare(Path(tmp) / 'core')
            main = (output / 'libv2ray_main.go').read_text()
            utils = (output / 'libv2ray_utils.go').read_text()
            helper = (output / 'libv2ray_anytls.go').read_text()
            self.assertIn('anyTLSPrepared  *anytlsbridge.Prepared', main)
            self.assertIn('if retErr != nil { _ = prepared.Close() }', main)
            self.assertIn('_ = x.anyTLSPrepared.Close()', main)
            self.assertIn('x.anyTLSPrepared = nil', main)
            self.assertIn('x.anyTLSPrepared = prepared', main)
            self.assertIn('defer prepared.Close()', utils)
            self.assertLess(utils.index('defer inst.Close()'), utils.index('if err := inst.Start()'))
            self.assertIn('internet.DialSystem(ctx, destination, nil)', helper)
            self.assertTrue((output / 'internal/anytlsbridge/bridge.go').is_file())
            self.assertEqual((module.ROOT / 'native/third_party/sing-anytls/session/stream.go').read_bytes(), (output / 'internal/third_party/sing-anytls/session/stream.go').read_bytes())
            self.assertIn('replace github.com/anytls/sing-anytls => ./internal/third_party/sing-anytls', (output / 'go.mod').read_text())
            self.assertIn('github.com/anytls/sing-anytls v0.0.11', (output / 'go.mod').read_text())
            with self.assertRaises(ValueError):
                module.prepare(output)


if __name__ == '__main__':
    unittest.main()

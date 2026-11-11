from pathlib import Path
import unittest
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[2]
ANDROID = '{http://schemas.android.com/apk/res/android}'


class PreviewIsolationTest(unittest.TestCase):
    def test_identity_is_explicit_opt_in_and_namespace_preserved(self):
        gradle = (ROOT / 'V2rayNG/app/build.gradle.kts').read_text()
        self.assertIn('gradleProperty("ANYTLS_PREVIEW")', gradle)
        self.assertIn('.getOrElse(false)', gradle)
        self.assertIn('if (anytlsPreview) "com.v2ray.ang.anytlspreview" else "com.v2ray.ang"', gradle)
        self.assertIn('namespace = "com.v2ray.ang"', gradle)
        self.assertIn('"$buildApplicationId.fdroid"', gradle)
        self.assertIn('"shortcut_target_package", buildApplicationId', gradle)

    def test_manifest_components_and_links_are_scoped(self):
        root = ET.parse(ROOT / 'V2rayNG/app/src/main/AndroidManifest.xml').getroot()
        self.assertNotIn(ANDROID + 'sharedUserId', root.attrib)
        for provider in root.findall('.//provider'):
            self.assertTrue(provider.get(ANDROID + 'authorities').startswith('${applicationId}.'))
        app = root.find('application')
        self.assertEqual('${applicationLabel}', app.get(ANDROID + 'label'))
        schemes = [node.get(ANDROID + 'scheme') for node in root.findall('.//data') if node.get(ANDROID + 'scheme')]
        self.assertEqual(['${configUrlScheme}'], schemes)
        for action in root.findall('.//receiver/intent-filter/action'):
            name = action.get(ANDROID + 'name')
            if '.action.widget.' in name or '.action.activity' in name:
                self.assertTrue(name.startswith('${applicationId}.'))

    def test_shortcuts_target_final_application_id_not_production(self):
        sources = sorted((ROOT / 'V2rayNG/app/src').glob('*/res/xml/shortcuts.xml'))
        self.assertEqual(2, len(sources))
        for source in sources:
            root = ET.parse(source).getroot()
            intents = root.findall('.//intent')
            self.assertEqual(4, len(intents))
            for node in intents:
                self.assertEqual('@string/shortcut_target_package', node.get(ANDROID + 'targetPackage'))
                self.assertTrue(node.get(ANDROID + 'targetClass').startswith('com.v2ray.ang.ui.shortcut.'))

    def test_scheme_import_does_not_log_credential_bearing_input(self):
        source = (ROOT / 'V2rayNG/app/src/main/java/com/v2ray/ang/ui/UrlSchemeActivity.kt').read_text()
        self.assertNotIn('LogUtil.i(AppConfig.TAG, uriString)', source)
        self.assertNotIn('LogUtil.i(AppConfig.TAG, decodedUrl)', source)

    def test_visible_test_label_exists_in_all_supported_locales(self):
        for locale in ('', 'en', 'zh-rCN', 'zh-rTW', 'vi', 'ru', 'fa', 'ar', 'az', 'bn', 'bqi-rIR'):
            directory = 'values' + ('-' + locale if locale else '')
            root = ET.parse(ROOT / 'V2rayNG/app/src/main/res' / directory / 'anytls_preview.xml').getroot()
            value = root.find("string[@name='app_name_anytls_preview']").text
            self.assertIn('AnyTLS TCP', value)
            self.assertTrue(any(marker in value for marker in ('TEST', '测试版', '測試版')))


if __name__ == '__main__':
    unittest.main()

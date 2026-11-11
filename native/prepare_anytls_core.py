#!/usr/bin/env python3
"""Create a pinned, disposable AndroidLibXrayLite overlay. Never edit the submodule."""
import argparse
import pathlib
import shutil
import subprocess
import tarfile
import io

REVISION = "b5a377087e74eaa34e86718710b04c50f4b767f5"
MODULE = "github.com/2dust/AndroidLibXrayLite/internal/anytlsbridge"
ROOT = pathlib.Path(__file__).resolve().parents[1]


def replace_once(text, old, new):
    if text.count(old) != 1:
        raise ValueError("Pinned native source anchor mismatch; review upstream before proceeding")
    return text.replace(old, new, 1)


def prepare(destination):
    source = ROOT / "AndroidLibXrayLite"
    head = subprocess.check_output(["git", "-C", str(source), "rev-parse", "HEAD"], text=True).strip()
    if head != REVISION:
        raise ValueError("AndroidLibXrayLite must match the reviewed gitlink " + REVISION)
    if destination.exists():
        raise ValueError("Output already exists; use a new empty output path")
    archive = subprocess.check_output(["git", "-C", str(source), "archive", REVISION])
    destination.mkdir(parents=True)
    with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
        tar.extractall(destination, filter="data")
    bridge = destination / "internal" / "anytlsbridge"
    bridge.mkdir(parents=True)
    for path in (ROOT / "native" / "anytlsbridge").glob("*.go"):
        shutil.copy2(path, bridge / path.name)
    if not (bridge / "bridge.go").is_file():
        raise ValueError("Missing AnyTLS bridge source")
    shutil.copytree(ROOT / "native" / "third_party" / "sing-anytls", destination / "internal" / "third_party" / "sing-anytls")
    shutil.copy2(ROOT / "native" / "core_integration_test.go", destination / "libv2ray_anytls_test.go")
    main_path = destination / "libv2ray_main.go"
    main = main_path.read_text()
    main = replace_once(main, '\t"fmt"', '\t"fmt"\n\t"' + MODULE + '"')
    main = replace_once(main, '\tcoreInstance    *core.Instance', '\tcoreInstance    *core.Instance\n\tanyTLSPrepared  *anytlsbridge.Prepared')
    main = replace_once(main, 'core.Version())', 'core.Version()) + ", AnyTLS TCP preview1"')
    main = replace_once(main, '\tx.IsRunning = false\n\tx.statsManager = nil', '\tif x.anyTLSPrepared != nil {\n\t\t_ = x.anyTLSPrepared.Close()\n\t\tx.anyTLSPrepared = nil\n\t}\n\tx.IsRunning = false\n\tx.statsManager = nil')
    main = replace_once(main, 'func (x *CoreController) doStartLoop(configContent string) error {', '''func (x *CoreController) doStartLoop(configContent string) (retErr error) {
    prepared, err := prepareAnyTLS(configContent)
    if err != nil { return fmt.Errorf("AnyTLS preparation failed: %w", err) }
    defer func() { if retErr != nil { _ = prepared.Close() } }()
    configContent = prepared.Config()''')
    main = replace_once(main, '\tx.statsManager = x.coreInstance.GetFeature', '\tx.anyTLSPrepared = prepared\n\tx.statsManager = x.coreInstance.GetFeature')
    main_path.write_text(main)
    utils_path = destination / "libv2ray_utils.go"
    utils = utils_path.read_text()
    utils = replace_once(utils, 'func MeasureOutboundDelay(ConfigureFileContent string, url string) (int64, error) {', '''func MeasureOutboundDelay(ConfigureFileContent string, url string) (int64, error) {
    prepared, err := prepareAnyTLS(ConfigureFileContent)
    if err != nil { return -1, fmt.Errorf("AnyTLS preparation failed: %w", err) }
    defer prepared.Close()
    ConfigureFileContent = prepared.Config()''')
    utils = replace_once(utils, '\tif err := inst.Start(); err != nil {', '\tdefer inst.Close()\n\tif err := inst.Start(); err != nil {')
    utils = replace_once(utils, '\tdefer inst.Close()\n\treturn measureInstDelay', '\treturn measureInstDelay')
    utils_path.write_text(utils)
    (destination / "libv2ray_anytls.go").write_text('''package libv2ray

import (
    "context"
    "net"
    "github.com/2dust/AndroidLibXrayLite/internal/anytlsbridge"
    corenet "github.com/xtls/xray-core/common/net"
    "github.com/xtls/xray-core/transport/internet"
)

// The Xray system dialer retains the existing TUN interface binding/controllers.
// Never replace it with net.Dialer: that could loop the AnyTLS endpoint into VPN.
func prepareAnyTLS(config string) (*anytlsbridge.Prepared, error) {
    return anytlsbridge.Prepare(config, func(ctx context.Context, network, address string) (net.Conn, error) {
        destination, err := corenet.ParseDestination(network + ":" + address)
        if err != nil { return nil, err }
        return internet.DialSystem(ctx, destination, nil)
    })
}
''')
    mod = destination / "go.mod"
    mod.write_text(mod.read_text() + '\nrequire github.com/anytls/sing-anytls v0.0.11\nreplace github.com/anytls/sing-anytls => ./internal/third_party/sing-anytls\n')
    return destination


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("destination", type=pathlib.Path)
    args = parser.parse_args()
    print(prepare(args.destination.resolve()))

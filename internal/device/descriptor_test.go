// SPDX-License-Identifier: GPL-3.0-only

package device

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

const minimal = `
id = "demo-remote"
name = "Demo"
[match]
names = ["Demo RC"]
[voice]
protocol = "atvv"
`

func TestBuiltinRC003(t *testing.T) {
	ds := Builtin()
	if len(ds) != 1 {
		t.Fatalf("Builtin() returned %d descriptors, want 1", len(ds))
	}
	d := ds[0]
	if d.ID != "xiaomi-rc003" || d.Match.Vendor != 0x2717 || d.Match.Product != 0x32B8 {
		t.Fatalf("unexpected RC003 descriptor: %+v", d)
	}
	for _, name := range []string{"Mi RC", " mi rc ", "XIAOMI BLUETOOTH REMOTE 2 PRO", "小米蓝牙语音遥控器"} {
		if !d.MatchesName(name) {
			t.Errorf("MatchesName(%q) = false", name)
		}
	}
	for _, name := range []string{"", "Mi RC 2", "Magic Keyboard"} {
		if d.MatchesName(name) {
			t.Errorf("MatchesName(%q) = true", name)
		}
	}
	if !d.MatchesModel("RC003") || d.MatchesModel("rc003") || d.MatchesModel("") {
		t.Error("MatchesModel: model numbers must match exactly")
	}
	// The voice key is F5, the usage macinput.UsageF5 blocks.
	if u, ok := d.VoiceUsage(); !ok || u != 0x7_0000_003E {
		t.Errorf("VoiceUsage() = %#x, %v", u, ok)
	}
	if len(d.Keys) != 13 {
		t.Errorf("RC003 has 13 keys, descriptor lists %d", len(d.Keys))
	}
}

func TestParseRejects(t *testing.T) {
	cases := map[string]string{
		"syntax":         `id = `,
		"unknown field":  minimal + "\nbattery = true\n",
		"typo in table":  strings.Replace(minimal, "names", "name", 1),
		"bad id":         strings.Replace(minimal, "demo-remote", "Demo Remote", 1),
		"empty name":     strings.Replace(minimal, `"Demo"`, `" "`, 1),
		"no match":       strings.Replace(minimal, `names = ["Demo RC"]`, "", 1),
		"protocol":       strings.Replace(minimal, `"atvv"`, `"hfp"`, 1),
		"voice key":      minimal + "key = \"mic\"\n[keys]\nok = 0x7_0000_0028\n",
		"bad key name":   minimal + "[keys]\nVolumeUp = 0x7_0000_0080\n",
		"zero usage":     minimal + "[keys]\nok = 0\n",
		"no page":        minimal + "[keys]\nok = 0x28\n",
		"usage overflow": minimal + "[keys]\nok = -1\n",
	}
	for name, src := range cases {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("%s: Parse accepted\n%s", name, src)
		}
	}
	if _, err := Parse([]byte(minimal)); err != nil {
		t.Fatalf("minimal descriptor: %v", err)
	}
}

func TestLoadFSSkipsBrokenFiles(t *testing.T) {
	other := strings.Replace(minimal, "demo-remote", "other-remote", 1)
	fsys := fstest.MapFS{
		"a.toml":      {Data: []byte(minimal)},
		"b.toml":      {Data: []byte("id = ")},
		"c.toml":      {Data: []byte(minimal)}, // duplicate id
		"d.toml":      {Data: []byte(other)},
		"notes.txt":   {Data: []byte("ignored")},
		"sub/e.toml":  {Data: []byte(minimal)}, // not top level
		"readme.toml": {Data: []byte(`# empty`)},
	}
	ds, err := LoadFS(fsys)
	if len(ds) != 2 || ds[0].ID != "demo-remote" || ds[1].ID != "other-remote" {
		t.Fatalf("loaded %+v", ds)
	}
	msg := err.Error()
	for _, want := range []string{"b.toml", "c.toml", "readme.toml"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error does not mention %s:\n%s", want, msg)
		}
	}
}

func TestCatalogOverridesBuiltin(t *testing.T) {
	ds, err := Catalog(filepath.Join(t.TempDir(), "missing"))
	if err != nil || len(ds) != 1 {
		t.Fatalf("missing user dir: %d descriptors, err %v", len(ds), err)
	}

	dir := t.TempDir()
	override := strings.Replace(strings.Replace(minimal, "demo-remote", "xiaomi-rc003", 1), `"Demo"`, `"My RC003"`, 1)
	for name, src := range map[string]string{"rc003.toml": override, "demo.toml": minimal, "bad.toml": "id ="} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ds, err = Catalog(dir)
	if err == nil || !strings.Contains(err.Error(), "bad.toml") {
		t.Errorf("err = %v, want it to mention bad.toml", err)
	}
	if len(ds) != 2 || ds[0].Name != "My RC003" || ds[1].ID != "demo-remote" {
		t.Fatalf("catalog = %+v", ds)
	}
	// The override must not leak into later Builtin calls.
	if Builtin()[0].Name != "小米蓝牙遥控器 2 Pro" {
		t.Fatal("Builtin was modified by Catalog")
	}
}

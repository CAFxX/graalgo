package bundle

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRoundTripThroughExecutable(t *testing.T) {
	want := &Bundle{
		Runner:  "/opt/graalgo/libexec/run",
		Main:    "main",
		X:       []string{"main.version=v1", "runtime.godebugDefault=panicnil=1"},
		ModInfo: "path\tmain\n",
		Resolution: Resolution{
			RegistryVersion: 7, RegistryHash: "00ff",
			Funcs: []FuncBinding{
				{Pkg: "main", Name: "now", Intrinsic: "runtime.nanotime"},
				{Pkg: "sync", Name: "runtime_Semacquire", TargetPkg: "runtime", TargetName: "sync_runtime_Semacquire"},
			},
			Vars: []VarAlias{{Pkg: "math/bits", Name: "overflowError", TargetPkg: "runtime", TargetName: "overflowError"}},
		},
		Packages: []Package{
			{ImportPath: "example.com/m/lib", IR: []byte{1, 2, 3}},
			{ImportPath: "main", IR: []byte{}},
		},
	}
	stub := []byte("\x7fELF pretend stub")
	enc := Encode(want)
	file := append(append(append([]byte{}, stub...), enc...), Trailer(int64(len(stub)), int64(len(enc)))...)
	p := filepath.Join(t.TempDir(), "exe")
	if err := os.WriteFile(p, file, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	// Decode yields empty-but-non-nil byte slices; compare field by field.
	if !reflect.DeepEqual(got.Resolution, want.Resolution) {
		t.Fatalf("resolution: got %+v, want %+v", got.Resolution, want.Resolution)
	}
	if got.Runner != want.Runner || got.Main != want.Main || len(got.Packages) != 2 ||
		got.Packages[0].ImportPath != "example.com/m/lib" || !reflect.DeepEqual(got.Packages[0].IR, []byte{1, 2, 3}) ||
		got.Packages[1].ImportPath != "main" || len(got.Packages[1].IR) != 0 {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}

func TestLocateRejectsForeignFiles(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain")
	if err := os.WriteFile(plain, []byte("an ordinary file that is long enough to hold a trailer"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Locate(plain); err == nil {
		t.Fatal("Locate accepted a file without a trailer")
	}
	// A trailer whose offset/length do not add up to the file size.
	bad := filepath.Join(dir, "bad")
	if err := os.WriteFile(bad, append([]byte("xxxxxxxx"), Trailer(100, 5)...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Locate(bad); err == nil {
		t.Fatal("Locate accepted an inconsistent trailer")
	}
}

func TestDecodeRejectsCorruption(t *testing.T) {
	enc := Encode(&Bundle{Runner: "r", Main: "main", Packages: []Package{{ImportPath: "main", IR: []byte("ir")}}})
	for name, data := range map[string][]byte{
		"truncated":     enc[:len(enc)-1],
		"trailing":      append(append([]byte{}, enc...), 0),
		"bad magic":     append([]byte("XXXX"), enc[4:]...),
		"wrong version": append(append([]byte("GGBN"), 99), enc[5:]...),
	} {
		if _, err := Decode(data); err == nil {
			t.Errorf("%s: Decode succeeded", name)
		}
	}
}

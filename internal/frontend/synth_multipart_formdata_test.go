package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestM7MultipartFormdataDesugarWiring pins the three m7MultiResultFuncs
// entries for the formdata.go multi-result methods (P1-1).
//
// The fix-pkg076 regeneration (f6e5cf92) emitted real __-helpers and pair
// structs for FileHeader.Open, Reader.ReadForm and Reader.readForm, but
// the table rows were written to a side file "for manual insertion" and
// never inserted. Without a row, multiResultHelperFor returns ok=false and
// classifyM7MultiResultCall falls through to the panic-stub method, so any
// user program calling fh.Open() or r.ReadForm() panicked at runtime.
//
// Uses the shared checkM7DesugarWiring: it fails on the pre-fix tree with
// "no m7MultiResultFuncs entry", and fails on any wiring typo (wrong
// helper name, wrong pair type, wrong field name/order, arity mismatch).
// It does NOT merely assert the rows exist: it verifies the helper is a
// real *types.Func in the synthetic package scope, its params accept the
// call args positionally, and the pair struct's fields match the
// multi-result signature's results — i.e. everything the desugar needs to
// lower the call site to the __mp helper instead of the panic stub.
func TestM7MultipartFormdataDesugarWiring(t *testing.T) {
	wants := []m7StrconvJsonExpect{
		{"mime/multipart", "Open", "FileHeader", "__mpFileHeaderOpen", "__mpFileHeaderOpenPair", []string{"f0", "f1"}},
		{"mime/multipart", "ReadForm", "Reader", "__mpReaderReadForm", "__mpReaderReadFormPair", []string{"f0", "f1"}},
		{"mime/multipart", "readForm", "Reader", "__mpReaderreadForm", "__mpReaderreadFormPair", []string{"f0", "err"}},
	}
	if len(wants) != 3 {
		t.Fatalf("wants has %d entries, want 3", len(wants))
	}
	for _, want := range wants {
		name := want.pkgPath + "." + want.recvType + "." + want.name
		t.Run(name, func(t *testing.T) {
			checkM7DesugarWiring(t, want)
		})
	}
}

// TestM7MultipartDesugarWiring pins all 15 mime/multipart m7MultiResultFuncs
// rows: the 3 formdata.go rows above plus the 12 pre-existing ones. Besides
// guarding the P1-1 fix it locks the field-name corrections (P1-1 follow-up):
// several archive/mime table rows carried hand-"normalized" field names
// ("f0", "_") that do not match the generated pair structs; synthPairField
// fails the export loudly on such a mismatch, so the table must carry the
// real field names.
func TestM7MultipartDesugarWiring(t *testing.T) {
	wants := []m7StrconvJsonExpect{
		{"mime/multipart", "Read", "stickyErrorReader", "__mpstickyErrorReaderRead", "__mpstickyErrorReaderReadPair", []string{"n", "f1"}},
		{"mime/multipart", "newPart", "", "__mpnewPart", "__mpnewPartPair", []string{"f0", "f1"}},
		{"mime/multipart", "Read", "Part", "__mpPartRead", "__mpPartReadPair", []string{"n", "err"}},
		{"mime/multipart", "Read", "partReader", "__mppartReaderRead", "__mppartReaderReadPair", []string{"f0", "f1"}},
		{"mime/multipart", "scanUntilBoundary", "", "__mpscanUntilBoundary", "__mpscanUntilBoundaryPair", []string{"f0", "f1"}},
		{"mime/multipart", "NextPart", "Reader", "__mpReaderNextPart", "__mpReaderNextPartPair", []string{"f0", "f1"}},
		{"mime/multipart", "NextRawPart", "Reader", "__mpReaderNextRawPart", "__mpReaderNextRawPartPair", []string{"f0", "f1"}},
		{"mime/multipart", "nextPart", "Reader", "__mpReadernextPart", "__mpReadernextPartPair", []string{"f0", "f1"}},
		{"mime/multipart", "ReadForm", "Reader", "__mpReaderReadForm", "__mpReaderReadFormPair", []string{"f0", "f1"}},
		{"mime/multipart", "readForm", "Reader", "__mpReaderreadForm", "__mpReaderreadFormPair", []string{"f0", "err"}},
		{"mime/multipart", "Open", "FileHeader", "__mpFileHeaderOpen", "__mpFileHeaderOpenPair", []string{"f0", "f1"}},
		{"mime/multipart", "CreatePart", "Writer", "__mpWriterCreatePart", "__mpWriterCreatePartPair", []string{"f0", "f1"}},
		{"mime/multipart", "CreateFormFile", "Writer", "__mpWriterCreateFormFile", "__mpWriterCreateFormFilePair", []string{"f0", "f1"}},
		{"mime/multipart", "CreateFormField", "Writer", "__mpWriterCreateFormField", "__mpWriterCreateFormFieldPair", []string{"f0", "f1"}},
		{"mime/multipart", "Write", "part", "__mppartWrite", "__mppartWritePair", []string{"n", "err"}},
	}
	if len(wants) != 15 {
		t.Fatalf("wants has %d entries, want 15", len(wants))
	}
	for _, want := range wants {
		name := want.pkgPath + "." + want.recvType + "." + want.name
		t.Run(name, func(t *testing.T) {
			checkM7DesugarWiring(t, want)
		})
	}
}

// TestMultipartFormdataDispatchResolves tests the exact lookup at the heart
// of the P1-1 failure chain: classifyMultiResultCall's method-call arm calls
// classifyM7MultiResultCall, which calls multiResultHelperFor. Pre-fix it
// returned ok=false for these three methods and the call site lowered to
// the panic stub; post-fix it must resolve to the real __-helper.
func TestMultipartFormdataDispatchResolves(t *testing.T) {
	wants := []struct {
		name, recv, helper string
	}{
		{"Open", "FileHeader", "__mpFileHeaderOpen"},
		{"ReadForm", "Reader", "__mpReaderReadForm"},
		{"readForm", "Reader", "__mpReaderreadForm"},
	}
	for _, want := range wants {
		t.Run("mime/multipart."+want.recv+"."+want.name, func(t *testing.T) {
			helperPkg, helperName, ok := multiResultHelperFor("mime/multipart", want.name, want.recv)
			if !ok {
				t.Fatalf("multiResultHelperFor(%q, %q, %q) = ok=false, want helper %q (call site would hit the panic stub)",
					"mime/multipart", want.name, want.recv, want.helper)
			}
			if helperPkg != "mime/multipart" || helperName != want.helper {
				t.Fatalf("multiResultHelperFor = (%q, %q), want (%q, %q)",
					helperPkg, helperName, "mime/multipart", want.helper)
			}
		})
	}
}

// TestM7ArchiveZipMissingRowsDesugarWiring pins five more archive/zip
// m7MultiResultFuncs entries that were missing for the same P1-1 reason:
// the generator emitted real __-helpers, pair structs and table-entry
// fragment lines for them, but the rows were never inserted into the table,
// so call sites fell through to the panic stubs. They were found by the
// new TestArchiveMimeTableEntriesInserted (the entries file listed them;
// the table did not). Same shared checker, same failure modes.
func TestM7ArchiveZipMissingRowsDesugarWiring(t *testing.T) {
	wants := []m7StrconvJsonExpect{
		{"archive/zip", "findBodyOffset", "File", "__zipFilefindBodyOffset", "__zipFilefindBodyOffsetPair", []string{"f0", "f1"}},
		{"archive/zip", "Info", "fileListEntry", "__zipfileListEntryInfo", "__zipfileListEntryInfoPair", []string{"f0", "f1"}},
		{"archive/zip", "Stat", "openDir", "__zipopenDirStat", "__zipopenDirStatPair", []string{"f0", "f1"}},
		{"archive/zip", "Read", "openDir", "__zipopenDirRead", "__zipopenDirReadPair", []string{"f0", "f1"}},
		{"archive/zip", "Info", "headerFileInfo", "__zipheaderFileInfoInfo", "__zipheaderFileInfoInfoPair", []string{"f0", "f1"}},
	}
	if len(wants) != 5 {
		t.Fatalf("wants has %d entries, want 5", len(wants))
	}
	for _, want := range wants {
		name := want.pkgPath + "." + want.recvType + "." + want.name
		t.Run(name, func(t *testing.T) {
			checkM7DesugarWiring(t, want)
		})
	}
}

var archiveMimeEntryRe = regexp.MustCompile(
	`^\s*\{"([^"]+)", "([^"]+)", "([^"]*)", "([^"]+)", "([^"]+)", \[\]string\{([^}]*)\}\},?\s*$`)

// TestArchiveMimeTableEntriesInserted closes the P1-1 process trap: the
// generator emits its table-entry fragment to archive_mime_table_entries.txt
// "for manual insertion" into m7MultiResultFuncs. Every entry in that file
// must have exactly one matching row in the table, with identical helper,
// pair type, and field names in order. If a future regeneration adds entries
// that are not pasted into the table, this test fails CI loudly instead of
// shipping runtime panic stubs again.
func TestArchiveMimeTableEntriesInserted(t *testing.T) {
	data, err := os.ReadFile("archive_mime_table_entries.txt")
	if err != nil {
		t.Fatalf("reading archive_mime_table_entries.txt: %v", err)
	}
	lines := strings.Split(string(data), "\n")
	checked := 0
	for ln, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		m := archiveMimeEntryRe.FindStringSubmatch(line)
		if m == nil {
			t.Fatalf("line %d: cannot parse table entry: %q", ln+1, line)
		}
		pkg, name, recv, helper, pair := m[1], m[2], m[3], m[4], m[5]
		var fields []string
		for _, f := range strings.Split(m[6], ",") {
			f = strings.Trim(strings.TrimSpace(f), `"`)
			if f != "" {
				fields = append(fields, f)
			}
		}
		var got *m7MultiResultFunc
		for i := range m7MultiResultFuncs {
			mf := &m7MultiResultFuncs[i]
			if mf.pkgPath == pkg && mf.name == name && mf.recvType == recv {
				if got != nil {
					t.Fatalf("line %d: duplicate m7MultiResultFuncs entry for %s.%s (recv %q)", ln+1, pkg, name, recv)
				}
				got = mf
			}
		}
		if got == nil {
			t.Fatalf("line %d: no m7MultiResultFuncs entry for generated %s.%s (recv %q) — insert it (P1-1)",
				ln+1, pkg, name, recv)
		}
		if got.helper != helper {
			t.Fatalf("line %d: table helper = %q, generated %q", ln+1, got.helper, helper)
		}
		if got.pairType != pair {
			t.Fatalf("line %d: table pairType = %q, generated %q", ln+1, got.pairType, pair)
		}
		if len(got.pairFields) != len(fields) {
			t.Fatalf("line %d: table pairFields = %v, generated %v", ln+1, got.pairFields, fields)
		}
		for i, f := range fields {
			if got.pairFields[i] != f {
				t.Fatalf("line %d: table pairFields = %v, generated %v", ln+1, got.pairFields, fields)
			}
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no entries parsed from archive_mime_table_entries.txt")
	}
	t.Logf("verified %d generated entries against m7MultiResultFuncs", checked)
}

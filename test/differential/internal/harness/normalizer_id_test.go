package harness

import "testing"

func TestNormalizerIDTracksRules(t *testing.T) {
	a := NormalizerID()
	old := NoiseSets["jvm"]
	NoiseSets["jvm"] = append(append([]string{}, old...), "^extra rule$")
	defer func() { NoiseSets["jvm"] = old }()
	if b := NormalizerID(); a == b {
		t.Fatal("NormalizerID must change when a noise rule changes")
	}
	saved := normalizeSource
	normalizeSource += "\n// edited"
	defer func() { normalizeSource = saved }()
	NoiseSets["jvm"] = old
	if c := NormalizerID(); c == a {
		t.Fatal("NormalizerID must change when normalize.go changes")
	}
}

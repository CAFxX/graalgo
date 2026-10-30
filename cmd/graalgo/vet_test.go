package main

import (
	"reflect"
	"testing"
)

func TestGroupVetFiles(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		want  [][]string
	}{
		{
			name:  "single directory stays one group",
			files: []string{"pkg/a.go", "pkg/b.go", "pkg/c_test.go"},
			want:  [][]string{{"pkg/a.go", "pkg/b.go", "pkg/c_test.go"}},
		},
		{
			name:  "two directories become two groups",
			files: []string{"dir1/a.go", "dir2/b.go", "dir1/c.go"},
			want:  [][]string{{"dir1/a.go", "dir1/c.go"}, {"dir2/b.go"}},
		},
		{
			name:  "first-seen directory order is preserved",
			files: []string{"b/x.go", "a/y.go", "b/z.go"},
			want:  [][]string{{"b/x.go", "b/z.go"}, {"a/y.go"}},
		},
		{
			name:  "bare filenames group under dot",
			files: []string{"a.go", "b.go"},
			want:  [][]string{{"a.go", "b.go"}},
		},
		{
			name:  "empty input yields no groups",
			files: nil,
			want:  nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := groupVetFiles(tc.files); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("groupVetFiles(%v) = %v, want %v", tc.files, got, tc.want)
			}
		})
	}
}
